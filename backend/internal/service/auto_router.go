package service

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 分组级「原生 auto」解析器
// =============================================================================
// 目的：让 sub2api 自己实现 auto —— 客户端请求 `auto` 时，由 sub2 在「该分组
// 当前可调度账号真正能服务的模型」集合里按策略挑一个，并把请求模型改写成它。
//
// 开启方式（Phase 1，零迁移）：分组模型白名单 model_allowlist.models 里出现
// sentinel 条目即开启：
//
//	auto::rr    轮询（round robin，计数存 Redis，多实例共享）
//	auto::rand  随机
//
// sentinel 的 `::` 前半段是客户端书写名（通常就是 auto），后半段是策略。
// 因此也支持别的名字，例如 `smart::rr`（客户端发 smart）。
//
// 设计约束：
//  1. 未出现 sentinel 的分组，Resolve 立即返回 ok=false，网关行为与改动前完全一致。
//  2. 候选池只取 ListSchedulableByGroupID —— 该查询本身已过滤 status/schedulable/
//     temp_unschedulable_until/rate_limit_reset_at/overload_until/expires_at，
//     所以不会挑到「当前根本没有可用账号」的模型（避免 429/502）。
//  3. 候选池与分组白名单取交集；去掉 sentinel 与 auto 基名自身（防递归）。
//  4. 任何异常（DB 出错、Redis 出错、候选为空）都回退「原样透传」，绝不 5xx。
// =============================================================================

const (
	// AutoSentinelPrefix sentinel 前缀，形如 `<name>::<strategy>`。
	AutoSentinelPrefix = "::"

	// AutoRoundRobinSentinel 轮询 sentinel（默认推荐）。
	AutoRoundRobinSentinel = "auto::rr"
	// AutoRandomSentinel 随机 sentinel。
	AutoRandomSentinel = "auto::rand"

	// autoGroupCacheTTL 分组 auto 配置 + 候选池的进程内缓存时长。
	// 配置改动（加/删 sentinel）最迟这么多秒生效。
	autoGroupCacheTTL = 30 * time.Second

	// autoRRKeyPrefix 轮询计数在 Redis 里的 key 前缀（per group）。
	autoRRKeyPrefix = "sub2api:auto:rr:"
	// autoRRKeyTTL 轮询计数 key 的过期时间（长期滚动即可）。
	autoRRKeyTTL = 7 * 24 * time.Hour

	// autoStrategySuffixRoundRobin / autoStrategySuffixRandom sentinel 策略后缀。
	autoStrategySuffixRoundRobin = "rr"
	autoStrategySuffixRandom     = "rand"

	// autoDefaultModelName 未显式指定时使用的客户端书写名。
	autoDefaultModelName = "auto"
)

// AutoStrategy auto 选模型的策略。
type AutoStrategy string

const (
	// AutoStrategyRoundRobin 轮询（跨实例共享计数，严格等权）。
	AutoStrategyRoundRobin AutoStrategy = "round_robin"
	// AutoStrategyRandom 随机。
	AutoStrategyRandom AutoStrategy = "random"
)

// AutoSpec 描述一个分组开启的 auto。
type AutoSpec struct {
	// Model 客户端书写的模型名（sentinel 的 `::` 前半段），默认 auto。
	Model string
	// Strategy 选模型策略。
	Strategy AutoStrategy
	// Sentinel 命中的白名单原始条目（用于日志/排查）。
	Sentinel string
}

// ParseAutoSentinel 解析白名单条目里的 sentinel。
// 返回 (客户端模型名, 策略, 是否命中)。
//
// 兼容写法：
//
//	auto::rr        → ("auto", round_robin)
//	auto::rand      → ("auto", random)
//	smart::rr       → ("smart", round_robin)
//	qwen3.8-max     → 不命中
func ParseAutoSentinel(entry string) (string, AutoStrategy, bool) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return "", "", false
	}
	idx := strings.LastIndex(trimmed, AutoSentinelPrefix)
	// idx<=0：没有分隔符，或 `::rr` 这种没有名字的写法 → 不认
	if idx <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(trimmed[:idx])
	if name == "" {
		return "", "", false
	}
	switch strings.ToLower(strings.TrimSpace(trimmed[idx+len(AutoSentinelPrefix):])) {
	case autoStrategySuffixRoundRobin, "round_robin":
		return name, AutoStrategyRoundRobin, true
	case autoStrategySuffixRandom, "random":
		return name, AutoStrategyRandom, true
	default:
		return "", "", false
	}
}

// IsAutoSentinel 报告条目是否是 auto sentinel。
func IsAutoSentinel(entry string) bool {
	_, _, ok := ParseAutoSentinel(entry)
	return ok
}

// ParseAutoSpec 在白名单里寻找第一个 sentinel，得到该分组的 auto 配置。
func ParseAutoSpec(models []string) (AutoSpec, bool) {
	for _, entry := range models {
		name, strategy, ok := ParseAutoSentinel(entry)
		if !ok {
			continue
		}
		return AutoSpec{Model: name, Strategy: strategy, Sentinel: strings.TrimSpace(entry)}, true
	}
	return AutoSpec{}, false
}

// autoGroupState 一个分组的 auto 解析结果（带 30s 缓存）。
type autoGroupState struct {
	spec     AutoSpec
	enabled  bool
	pool     []string // 已排序，跨实例轮询顺序一致
	loadedAt time.Time
}

// AutoAccountReader auto 候选池需要的账号能力（收窄接口，便于测试替换）。
type AutoAccountReader interface {
	ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error)
}

// AutoGroupReader auto 需要的分组能力。
type AutoGroupReader interface {
	GetByIDLite(ctx context.Context, id int64) (*Group, error)
}

// AutoRouter 分组级 auto 解析器。
// 并发安全；由 ChannelService 持有并在网关热路径调用。
type AutoRouter struct {
	accounts AutoAccountReader
	groups   AutoGroupReader
	redis    *redis.Client

	mu    sync.RWMutex
	cache map[int64]autoGroupState
}

// NewAutoRouter 创建 auto 解析器。accounts/groups 必须非 nil 才会生效；
// redis 为 nil 时轮询退化为进程内随机（仍可用，但多实例下不均匀）。
func NewAutoRouter(accounts AutoAccountReader, groups AutoGroupReader, redisClient *redis.Client) *AutoRouter {
	return &AutoRouter{
		accounts: accounts,
		groups:   groups,
		redis:    redisClient,
		cache:    make(map[int64]autoGroupState),
	}
}

// Resolve 尝试把请求模型解析为一个具体模型。
// 返回 (改写后的模型名, true) 表示命中 auto；否则 (_, false) 表示调用方应保持原行为。
func (r *AutoRouter) Resolve(ctx context.Context, groupID int64, requested string) (string, bool) {
	if r == nil || r.accounts == nil || r.groups == nil || groupID <= 0 {
		return "", false
	}

	state := r.loadState(ctx, groupID)
	if !state.enabled || len(state.pool) == 0 {
		return "", false
	}
	// 客户端书写名必须命中（大小写不敏感），避免误伤其它模型。
	if !strings.EqualFold(strings.TrimSpace(requested), state.spec.Model) {
		return "", false
	}
	if len(state.pool) == 1 {
		return state.pool[0], true
	}
	if state.spec.Strategy == AutoStrategyRandom {
		return state.pool[rand.IntN(len(state.pool))], true
	}
	return r.pickRoundRobin(ctx, groupID, state.pool), true
}

// loadState 读取（或刷新）分组 auto 状态。
func (r *AutoRouter) loadState(ctx context.Context, groupID int64) autoGroupState {
	r.mu.RLock()
	cached, ok := r.cache[groupID]
	r.mu.RUnlock()
	if ok && time.Since(cached.loadedAt) < autoGroupCacheTTL {
		return cached
	}

	state, err := r.buildState(ctx, groupID)
	if err != nil {
		// 出错不缓存（下次请求重试），并回退到「不改写」。
		slog.Warn("auto route: build state failed", "group_id", groupID, "error", err)
		return autoGroupState{}
	}
	r.mu.Lock()
	r.cache[groupID] = state
	r.mu.Unlock()
	return state
}

// buildState 从 DB 计算分组 auto 配置与候选池。
func (r *AutoRouter) buildState(ctx context.Context, groupID int64) (autoGroupState, error) {
	group, err := r.groups.GetByIDLite(ctx, groupID)
	if err != nil {
		return autoGroupState{}, err
	}
	if group == nil || !group.ModelAllowlist.Enabled {
		// 白名单未开启 → 不存在 sentinel 概念，静默不启用。
		return autoGroupState{loadedAt: time.Now()}, nil
	}
	spec, ok := ParseAutoSpec(group.ModelAllowlist.Models)
	if !ok {
		return autoGroupState{loadedAt: time.Now()}, nil
	}

	pool, err := r.buildPool(ctx, group, spec)
	if err != nil {
		return autoGroupState{}, err
	}
	return autoGroupState{
		spec:     spec,
		enabled:  true,
		pool:     pool,
		loadedAt: time.Now(),
	}, nil
}

// buildPool 计算候选池：
//
//	(可调度账号 model_mapping 的键 ∪ [存在无映射账号时] 白名单条目)
//	   ∩ 白名单  − sentinel  − auto 基名自身
//
// 结果去重后排序，保证多实例轮询顺序一致。
func (r *AutoRouter) buildPool(ctx context.Context, group *Group, spec AutoSpec) ([]string, error) {
	accounts, err := r.accounts.ListSchedulableByGroupID(ctx, group.ID)
	if err != nil {
		return nil, err
	}

	raw := make(map[string]string, len(accounts)) // lower → 原始书写
	unrestricted := false
	for i := range accounts {
		mapping := accounts[i].GetModelMapping()
		if len(mapping) == 0 {
			// 空映射（或 passthrough 账号）表示「白名单内的模型都可用」，
			// 此时白名单本身就是能力声明 —— 与 GetGroupModelsListCandidates 的
			// supplementUnmappedOpenAIModels 思路一致。
			unrestricted = true
			continue
		}
		for requested := range mapping {
			name := strings.TrimSpace(requested)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, exists := raw[key]; !exists {
				raw[key] = name
			}
		}
	}
	if unrestricted {
		for _, entry := range group.ModelAllowlist.Models {
			name := strings.TrimSpace(entry)
			if name == "" || IsAutoSentinel(name) {
				continue
			}
			key := strings.ToLower(name)
			if _, exists := raw[key]; !exists {
				raw[key] = name
			}
		}
	}

	autoName := strings.ToLower(spec.Model)
	out := make([]string, 0, len(raw))
	for key, name := range raw {
		if key == autoName || IsAutoSentinel(name) {
			continue
		}
		// 与白名单取交集（Allows 已支持 `*` 通配与大小写不敏感）。
		if !group.ModelAllowlist.Allows(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i]), strings.ToLower(out[j])
		if li == lj {
			return out[i] < out[j]
		}
		return li < lj
	})
	return out, nil
}

// pickRoundRobin 用 Redis 计数做跨实例严格轮询；Redis 不可用时退回随机。
func (r *AutoRouter) pickRoundRobin(ctx context.Context, groupID int64, pool []string) string {
	if r.redis == nil {
		return pool[rand.IntN(len(pool))]
	}
	key := autoRRKeyPrefix + strconv.FormatInt(groupID, 10)
	n, err := r.redis.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("auto route: round robin counter failed, fallback to random",
			"group_id", groupID, "error", err)
		return pool[rand.IntN(len(pool))]
	}
	if n == 1 {
		// 首次创建时设置过期，避免 key 永久堆积。
		if err := r.redis.Expire(ctx, key, autoRRKeyTTL).Err(); err != nil {
			slog.Debug("auto route: expire round robin key failed", "group_id", groupID, "error", err)
		}
	}
	// n 从 1 开始，保证第一次落在 pool[0]。
	idx := int((n - 1) % int64(len(pool)))
	if idx < 0 {
		idx = 0
	}
	return pool[idx]
}
