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
// 目的：让 sub2api 自己实现 auto —— 客户端请求某个「基名」时，由 sub2 在该基名的
// 候选池里按策略挑一个模型，并把请求模型改写成它。
//
// 开启方式（零迁移）：分组模型白名单 model_allowlist.models 里出现 sentinel 条目：
//
//	auto::rr         轮询（round robin，计数存 Redis，多实例共享）
//	auto::rand       随机
//	copilot::rr      「按账号圈定」的子池，客户端发 copilot
//	aistudio::rand   同上
//
// sentinel 的 `::` 前半段是客户端书写名（基名），后半段是策略。
//
// 两种池子：
//  1. 全组池（默认）：当没有任何可调度账号在 model_mapping 里声明该基名时，
//     池 = (各账号映射键 ∪ [存在无映射账号时] 白名单条目) ∩ 白名单 − sentinel − 基名自身。
//     组级 auto 走这一种。
//  2. 圈定池（子池）：若有账号在 model_mapping 里声明了该基名（例如把 `copilot`
//     写进 Copilot 账号的映射），则池 = 这些账号声明的其余映射键，**不再与白名单取交集**
//     —— 账号的映射本身就是「原模型列表」的能力声明，白名单只需放行基名本身。
//     效果：下游只记一个名字，sub2 按策略轮询该账号的原模型列表。
//     基名 `auto` 永远走全组池（避免某个恰好声明了 auto 的账号把组级 auto 意外收窄）。
//
// 基名可以嵌套一层：全组池里允许包含其它基名（如 copilot），Resolve 会再展开一次，
// 因此 `auto` 仍然覆盖全组，同时 `copilot` / `aistudio` 各自独立轮询、互不干扰。
//
// 设计约束：
//  1. 未出现 sentinel 的分组，Resolve 立即返回 ok=false，网关行为与改动前完全一致。
//  2. 池子基于 ListSchedulableByGroupID —— 该查询只过滤 status/schedulable 这类
//     持久资格，运行时限流/冷却不在这里过滤：冷却账号独有的模型可能仍留在池里，
//     此时请求由 no_account_error 分支返回 503，客户端重试即可跨过去。
//  3. 任何异常（DB 出错、Redis 出错、候选为空）都回退「原样透传」，绝不 5xx。
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

	// autoRRKeyPrefix 轮询计数在 Redis 里的 key 前缀（per group + per 基名）。
	// 多个基名各有独立计数，互不干扰。
	autoRRKeyPrefix = "sub2api:auto:rr:"
	// autoRRKeyTTL 轮询计数 key 的过期时间（长期滚动即可）。
	autoRRKeyTTL = 7 * 24 * time.Hour

	// autoStrategySuffixRoundRobin / autoStrategySuffixRandom sentinel 策略后缀。
	autoStrategySuffixRoundRobin = "rr"
	autoStrategySuffixRandom     = "rand"

	// autoDefaultModelName 未显式指定时使用的客户端书写名；该名字永远走全组池。
	autoDefaultModelName = "auto"

	// autoMaxAliasDepth 基名嵌套展开的最大层数（auto → copilot → 具体模型）。
	autoMaxAliasDepth = 2
)

// AutoStrategy auto 选模型的策略。
type AutoStrategy string

const (
	// AutoStrategyRoundRobin 轮询（跨实例共享计数，严格等权）。
	AutoStrategyRoundRobin AutoStrategy = "round_robin"
	// AutoStrategyRandom 随机。
	AutoStrategyRandom AutoStrategy = "random"
)

// AutoSpec 描述一个分组开启的一个基名（一个分组可同时开启多个基名）。
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
//	copilot::rr     → ("copilot", round_robin)
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

// ParseAutoSpec 在白名单里寻找第一个 sentinel，得到单个 auto 配置。
// 保留给只需要「有没有开 auto」的调用方；需要全部基名时用 ParseAutoSpecs。
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

// ParseAutoSpecs 收集白名单里全部 sentinel（同一基名只认第一条，后写的策略被忽略）。
// 返回顺序与白名单书写顺序一致。
func ParseAutoSpecs(models []string) []AutoSpec {
	specs := make([]AutoSpec, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, entry := range models {
		name, strategy, ok := ParseAutoSentinel(entry)
		if !ok {
			continue
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		specs = append(specs, AutoSpec{
			Model:    name,
			Strategy: strategy,
			Sentinel: strings.TrimSpace(entry),
		})
	}
	return specs
}

// autoSpecState 单个基名的解析结果。
type autoSpecState struct {
	spec   AutoSpec
	pool   []string // 已排序，跨实例轮询顺序一致
	scoped bool     // true = 池子按「声明了该基名的账号」圈定
}

// autoGroupState 一个分组的 auto 解析结果（带 30s 缓存），可含多个基名。
type autoGroupState struct {
	specs    map[string]autoSpecState // key: lower(基名)
	loadedAt time.Time
}

// enabled 报告该分组是否至少开启了一个基名。
func (s autoGroupState) enabled() bool {
	return len(s.specs) > 0
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
	if !state.enabled() {
		return "", false
	}
	// 客户端书写名必须命中某个基名（大小写不敏感），避免误伤其它模型。
	name := strings.TrimSpace(requested)
	if name == "" {
		return "", false
	}
	nameState, ok := state.specs[strings.ToLower(name)]
	if !ok {
		return "", false
	}
	return r.pickModel(ctx, groupID, nameState.spec, nameState, state, 0)
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

// buildState 从 DB 计算分组 auto 配置与各基名的候选池。
func (r *AutoRouter) buildState(ctx context.Context, groupID int64) (autoGroupState, error) {
	group, err := r.groups.GetByIDLite(ctx, groupID)
	if err != nil {
		return autoGroupState{}, err
	}
	if group == nil || !group.ModelAllowlist.Enabled {
		// 白名单未开启 → 不存在 sentinel 概念，静默不启用。
		return autoGroupState{loadedAt: time.Now()}, nil
	}
	specs := ParseAutoSpecs(group.ModelAllowlist.Models)
	if len(specs) == 0 {
		return autoGroupState{loadedAt: time.Now()}, nil
	}

	// 账号只查一次，多个基名共用（圈定与否都基于同一份可调度账号列表）。
	accounts, err := r.accounts.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return autoGroupState{}, err
	}

	bases := make(map[string]string, len(specs)) // lower(基名) → 原书写
	for _, spec := range specs {
		bases[strings.ToLower(spec.Model)] = spec.Model
	}

	states := make(map[string]autoSpecState, len(specs))
	for _, spec := range specs {
		pool, scoped := buildPool(group, accounts, spec, bases)
		states[strings.ToLower(spec.Model)] = autoSpecState{spec: spec, pool: pool, scoped: scoped}
	}
	return autoGroupState{specs: states, loadedAt: time.Now()}, nil
}

// buildPool 计算某个基名的候选池，返回 (池, 是否圈定)。
//
// 圈定判定：存在可调度账号在 model_mapping 里声明了该基名，且基名不是 `auto`。
//
//	圈定池 = 这些账号声明的其余映射键 − sentinel − 其它基名（不与白名单取交集）
//	全组池 = (各账号映射键 ∪ [存在无映射账号时] 白名单条目) ∩ 白名单 − sentinel − 基名自身
//
// 结果去重后排序，保证多实例轮询顺序一致。
func buildPool(group *Group, accounts []Account, spec AutoSpec, bases map[string]string) ([]string, bool) {
	baseKey := strings.ToLower(strings.TrimSpace(spec.Model))

	// 1) 找「声明了该基名」的账号 —— 它们定义了子池。
	declaring := make([]Account, 0, 2)
	for i := range accounts {
		if accountDeclaresModel(&accounts[i], spec.Model) {
			declaring = append(declaring, accounts[i])
		}
	}
	// `auto` 永远走全组池：避免某个恰好声明了 auto 的账号把组级 auto 收窄。
	scoped := len(declaring) > 0 && baseKey != autoDefaultModelName
	source := accounts
	if scoped {
		source = declaring
	}

	raw := make(map[string]string, len(source)) // lower → 原始书写
	unrestricted := false
	for i := range source {
		mapping := source[i].GetModelMapping()
		if len(mapping) == 0 {
			// 空映射（或 passthrough 账号）表示「白名单内的模型都可用」，
			// 此时白名单本身就是能力声明 —— 与 GetGroupModelsListCandidates 的
			// supplementUnmappedOpenAIModels 思路一致。
			// 圈定池不适用：没有映射就无所谓「原模型列表」。
			if !scoped {
				unrestricted = true
			}
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

	out := make([]string, 0, len(raw))
	for key, name := range raw {
		// 基名自身不进池（防递归）。
		if key == baseKey || IsAutoSentinel(name) {
			continue
		}
		if scoped {
			// 圈定池：账号映射键就是能力声明，且不允许把别的基名当成具体模型。
			if _, isBase := bases[key]; isBase {
				continue
			}
			out = append(out, name)
			continue
		}
		// 全组池：与白名单取交集（Allows 已支持 `*` 通配与大小写不敏感）。
		// 其它基名允许留在池里 —— Resolve 会把它们再展开一层。
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
	return out, scoped
}

// accountDeclaresModel 报告账号的 model_mapping 是否声明了某个模型名（大小写不敏感）。
func accountDeclaresModel(account *Account, model string) bool {
	model = strings.TrimSpace(model)
	if account == nil || model == "" {
		return false
	}
	want := strings.ToLower(model)
	for requested := range account.GetModelMapping() {
		if strings.ToLower(strings.TrimSpace(requested)) == want {
			return true
		}
	}
	return false
}

// pickModel 在 base 的池子里按策略挑一个模型；若挑中的正好是另一个基名，
// 再展开一层（auto → copilot → 具体模型），最多 autoMaxAliasDepth 层。
func (r *AutoRouter) pickModel(
	ctx context.Context,
	groupID int64,
	spec AutoSpec,
	state autoSpecState,
	all autoGroupState,
	depth int,
) (string, bool) {
	if len(state.pool) == 0 {
		return "", false
	}
	var picked string
	switch {
	case len(state.pool) == 1:
		picked = state.pool[0]
	case spec.Strategy == AutoStrategyRandom:
		picked = state.pool[rand.IntN(len(state.pool))]
	default:
		picked = r.pickRoundRobin(ctx, groupID, spec.Model, state.pool)
	}
	if depth+1 >= autoMaxAliasDepth {
		return picked, true
	}
	if next, ok := all.specs[strings.ToLower(picked)]; ok && len(next.pool) > 0 {
		if deeper, ok := r.pickModel(ctx, groupID, next.spec, next, all, depth+1); ok {
			return deeper, true
		}
	}
	return picked, true
}

// pickRoundRobin 用 Redis 计数做跨实例严格轮询；Redis 不可用时退回随机。
// 计数按「分组 + 基名」分开，保证 copilot / aistudio / auto 各自的轮询互相独立。
func (r *AutoRouter) pickRoundRobin(ctx context.Context, groupID int64, base string, pool []string) string {
	if r.redis == nil {
		return pool[rand.IntN(len(pool))]
	}
	key := autoRRKeyPrefix + strconv.FormatInt(groupID, 10) + ":" + strings.ToLower(strings.TrimSpace(base))
	n, err := r.redis.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("auto route: round robin counter failed, fallback to random",
			"group_id", groupID, "base", base, "error", err)
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
