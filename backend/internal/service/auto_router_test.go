//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Stubs：只实现 auto 需要的两个收窄接口
// ---------------------------------------------------------------------------

type autoGroupStub struct {
	group *Group
	err   error
	calls int
}

func (s *autoGroupStub) GetByIDLite(context.Context, int64) (*Group, error) {
	s.calls++
	return s.group, s.err
}

type autoAccountStub struct {
	accounts []Account
	err      error
	calls    int
}

func (s *autoAccountStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	s.calls++
	return s.accounts, s.err
}

// accountWithMapping 构造一个带 model_mapping 的账号（映射键即客户端可书写的模型名）。
func accountWithMapping(pairs ...string) Account {
	mapping := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		mapping[pairs[i]] = pairs[i+1]
	}
	return Account{
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"model_mapping": mapping},
	}
}

func autoGroup(id int64, models ...string) *Group {
	return &Group{
		ID:       id,
		Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{
			Enabled: true,
			Models:  models,
		},
	}
}

func newAutoRouterForTest(t *testing.T, group *Group, accounts []Account, rdb *redis.Client) (*AutoRouter, *autoGroupStub, *autoAccountStub) {
	t.Helper()
	groupStub := &autoGroupStub{group: group}
	accountStub := &autoAccountStub{accounts: accounts}
	return NewAutoRouter(accountStub, groupStub, rdb), groupStub, accountStub
}

func newMiniRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// ---------------------------------------------------------------------------
// sentinel 解析
// ---------------------------------------------------------------------------

func TestParseAutoSentinel(t *testing.T) {
	cases := []struct {
		name         string
		entry        string
		wantModel    string
		wantStrategy AutoStrategy
		wantOK       bool
	}{
		{name: "轮询", entry: "auto::rr", wantModel: "auto", wantStrategy: AutoStrategyRoundRobin, wantOK: true},
		{name: "随机", entry: "auto::rand", wantModel: "auto", wantStrategy: AutoStrategyRandom, wantOK: true},
		{name: "自定义名", entry: "smart::rr", wantModel: "smart", wantStrategy: AutoStrategyRoundRobin, wantOK: true},
		{name: "大小写与空格", entry: "  AUTO::RR  ", wantModel: "AUTO", wantStrategy: AutoStrategyRoundRobin, wantOK: true},
		{name: "长策略名", entry: "auto::round_robin", wantModel: "auto", wantStrategy: AutoStrategyRoundRobin, wantOK: true},
		{name: "普通模型不命中", entry: "glm-5.3", wantOK: false},
		{name: "裸 auto 不命中", entry: "auto", wantOK: false},
		{name: "无名字不命中", entry: "::rr", wantOK: false},
		{name: "未知策略不命中", entry: "auto::fast", wantOK: false},
		{name: "空串不命中", entry: "   ", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, strategy, ok := ParseAutoSentinel(tc.entry)
			require.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			require.Equal(t, tc.wantModel, model)
			require.Equal(t, tc.wantStrategy, strategy)
		})
	}
}

func TestParseAutoSpec_FirstSentinelWins(t *testing.T) {
	spec, ok := ParseAutoSpec([]string{"glm-5.3", "auto::rr", "auto::rand"})
	require.True(t, ok)
	require.Equal(t, "auto", spec.Model)
	require.Equal(t, AutoStrategyRoundRobin, spec.Strategy)

	_, ok = ParseAutoSpec([]string{"glm-5.3", "kimi-k3"})
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// 未开启 auto：行为必须与改动前完全一致
// ---------------------------------------------------------------------------

func TestAutoRouter_DisabledWithoutSentinel(t *testing.T) {
	router, groupStub, accountStub := newAutoRouterForTest(t, autoGroup(14, "glm-5.3", "kimi-k3"), []Account{accountWithMapping("glm-5.3", "glm-5.3")}, newMiniRedis(t))

	picked, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok, "白名单没有 sentinel 时不得改写")
	require.Empty(t, picked)
	require.Equal(t, 1, groupStub.calls)
	require.Zero(t, accountStub.calls, "未开启 auto 时不应查账号")
}

func TestAutoRouter_DisabledWhenAllowlistDisabled(t *testing.T) {
	group := autoGroup(14, AutoRoundRobinSentinel, "glm-5.3")
	group.ModelAllowlist.Enabled = false
	router, _, accountStub := newAutoRouterForTest(t, group, []Account{accountWithMapping("glm-5.3", "glm-5.3")}, newMiniRedis(t))

	_, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok)
	require.Zero(t, accountStub.calls)
}

func TestAutoRouter_RequestedModelMustMatchAutoName(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3", "kimi-k3"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3", "kimi-k3", "kimi-k3")}, newMiniRedis(t))

	// 具体模型请求不受 auto 影响
	picked, ok := router.Resolve(context.Background(), 14, "glm-5.3")
	require.False(t, ok)
	require.Empty(t, picked)

	// 大小写不敏感地命中 auto
	picked, ok = router.Resolve(context.Background(), 14, "AUTO")
	require.True(t, ok)
	require.Contains(t, []string{"glm-5.3", "kimi-k3"}, picked)
}

// ---------------------------------------------------------------------------
// 候选池
// ---------------------------------------------------------------------------

func TestAutoRouter_PoolIsAllowlistIntersectAccountMappings(t *testing.T) {
	accounts := []Account{
		accountWithMapping("glm-5.3", "glm-5.3", "kimi-k3", "kimi-k3", "not-in-allowlist", "not-in-allowlist"),
		accountWithMapping("sensenova-6.8-flash-lite", "sensenova-6.8-flash-lite"),
	}
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3", "kimi-k3", "sensenova-6.8-flash-lite"),
		accounts, newMiniRedis(t))

	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		got = append(got, picked)
	}
	// 候选池按名排序后严格轮询：glm-5.3 → kimi-k3 → sensenova-6.8-flash-lite
	require.Equal(t, []string{"glm-5.3", "kimi-k3", "sensenova-6.8-flash-lite"}, got)
}

func TestAutoRouter_ExcludesSentinelAndAutoName(t *testing.T) {
	// 白名单里同时写了 sentinel 与裸 auto，二者都不能成为选中目标
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "auto", "glm-5.3"),
		[]Account{accountWithMapping("auto", "auto", "auto::rr", "auto::rr", "glm-5.3", "glm-5.3")}, newMiniRedis(t))

	for i := 0; i < 3; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		require.Equal(t, "glm-5.3", picked)
	}
}

func TestAutoRouter_EmptyPoolFallsBack(t *testing.T) {
	// 账号能服务的模型都不在白名单里 → 候选池为空 → 不改写
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3"),
		[]Account{accountWithMapping("kimi-k3", "kimi-k3")}, newMiniRedis(t))

	_, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok)
}

func TestAutoRouter_NoSchedulableAccountsFallsBack(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3"), nil, newMiniRedis(t))
	_, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok)
}

func TestAutoRouter_UnrestrictedAccountUsesAllowlistAsPool(t *testing.T) {
	// 无映射账号（= 白名单内模型都可用）→ 候选池取白名单条目（去掉 sentinel）
	unrestricted := Account{Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3", "kimi-k3"),
		[]Account{unrestricted}, newMiniRedis(t))

	got := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		got = append(got, picked)
	}
	require.Equal(t, []string{"glm-5.3", "kimi-k3"}, got)
}

func TestAutoRouter_WildcardAllowlistEntryKeepsModel(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-*"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3", "kimi-k3", "kimi-k3")}, newMiniRedis(t))

	picked, ok := router.Resolve(context.Background(), 14, "auto")
	require.True(t, ok)
	require.Equal(t, "glm-5.3", picked, "通配白名单条目应保留其覆盖的模型")
}

// ---------------------------------------------------------------------------
// 策略
// ---------------------------------------------------------------------------

func TestAutoRouter_SingleCandidateAlwaysPicked(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3")}, newMiniRedis(t))

	for i := 0; i < 3; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		require.Equal(t, "glm-5.3", picked)
	}
}

func TestAutoRouter_RoundRobinIsStrictAndSharedAcrossInstances(t *testing.T) {
	rdb := newMiniRedis(t)
	group := autoGroup(14, AutoRoundRobinSentinel, "a-model", "b-model")
	accounts := []Account{accountWithMapping("a-model", "a-model", "b-model", "b-model")}

	// 两个实例（模拟多实例部署）共享同一个 Redis 计数
	instA, _, _ := newAutoRouterForTest(t, group, accounts, rdb)
	instB, _, _ := newAutoRouterForTest(t, group, accounts, rdb)

	got := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		router := instA
		if i%2 == 1 {
			router = instB
		}
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		got = append(got, picked)
	}
	require.Equal(t, []string{"a-model", "b-model", "a-model", "b-model"}, got)
}

func TestAutoRouter_RandomStrategyStaysInPool(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRandomSentinel, "a-model", "b-model"),
		[]Account{accountWithMapping("a-model", "a-model", "b-model", "b-model")}, newMiniRedis(t))

	seen := map[string]int{}
	for i := 0; i < 40; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		require.Contains(t, []string{"a-model", "b-model"}, picked)
		seen[picked]++
	}
	require.Len(t, seen, 2, "随机策略应能覆盖候选池全部模型")
}

func TestAutoRouter_RoundRobinFallsBackToRandomWithoutRedis(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "a-model", "b-model"),
		[]Account{accountWithMapping("a-model", "a-model", "b-model", "b-model")}, nil)

	for i := 0; i < 6; i++ {
		picked, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
		require.Contains(t, []string{"a-model", "b-model"}, picked)
	}
}

// ---------------------------------------------------------------------------
// 缓存与异常回退
// ---------------------------------------------------------------------------

func TestAutoRouter_CachesPerGroup(t *testing.T) {
	router, groupStub, accountStub := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3")}, newMiniRedis(t))

	for i := 0; i < 5; i++ {
		_, ok := router.Resolve(context.Background(), 14, "auto")
		require.True(t, ok)
	}
	require.Equal(t, 1, groupStub.calls, "TTL 内应复用分组缓存")
	require.Equal(t, 1, accountStub.calls, "TTL 内应复用候选池缓存")
}

func TestAutoRouter_ErrorFallsBackToPassthrough(t *testing.T) {
	groupStub := &autoGroupStub{err: errors.New("db down")}
	accountStub := &autoAccountStub{}
	router := NewAutoRouter(accountStub, groupStub, newMiniRedis(t))

	_, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok, "查库失败必须回退透传，不能改写请求")

	groupStub.err = nil
	groupStub.group = autoGroup(14, AutoRoundRobinSentinel, "glm-5.3")
	accountStub.err = errors.New("db down")
	picked, ok := router.Resolve(context.Background(), 14, "auto")
	require.False(t, ok)
	require.Empty(t, picked)
}

func TestAutoRouter_IgnoresInvalidGroupIDAndNilDeps(t *testing.T) {
	router, groupStub, _ := newAutoRouterForTest(t, autoGroup(14, AutoRoundRobinSentinel, "glm-5.3"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3")}, newMiniRedis(t))

	_, ok := router.Resolve(context.Background(), 0, "auto")
	require.False(t, ok)
	require.Zero(t, groupStub.calls)

	nilRouter := NewAutoRouter(nil, nil, nil)
	_, ok = nilRouter.Resolve(context.Background(), 14, "auto")
	require.False(t, ok)

	var typedNil *AutoRouter
	_, ok = typedNil.Resolve(context.Background(), 14, "auto")
	require.False(t, ok, "nil AutoRouter 必须安全")
}

// ---------------------------------------------------------------------------
// 与 ChannelService / 白名单准入的集成
// ---------------------------------------------------------------------------

func TestChannelService_AutoRouteRewritesModelForBilling(t *testing.T) {
	gid := int64(14)
	router, _, _ := newAutoRouterForTest(t, autoGroup(gid, AutoRoundRobinSentinel, "glm-5.3", "kimi-k3"),
		[]Account{accountWithMapping("glm-5.3", "glm-5.3", "kimi-k3", "kimi-k3")}, newMiniRedis(t))

	svc := NewChannelService(&mockChannelRepository{}, nil, nil, nil, nil)
	svc.SetAutoRouter(router)

	result, restricted := svc.ResolveChannelMappingAndRestrict(context.Background(), &gid, "auto")
	require.False(t, restricted)
	require.True(t, result.Mapped, "auto 命中后必须标记为已映射（否则计费会按 auto 计）")
	require.Contains(t, []string{"glm-5.3", "kimi-k3"}, result.MappedModel)
	require.Equal(t, BillingModelSourceChannelMapped, result.BillingModelSource)

	fields := result.ToUsageFields("auto", result.MappedModel)
	require.Equal(t, "auto", fields.OriginalModel)
	require.Equal(t, result.MappedModel, fields.ChannelMappedModel)
	require.Equal(t, "auto→"+result.MappedModel, fields.ModelMappingChain)
}

func TestChannelService_WithoutAutoRouterBehaviorUnchanged(t *testing.T) {
	gid := int64(14)
	svc := NewChannelService(&mockChannelRepository{}, nil, nil, nil, nil)

	result, restricted := svc.ResolveChannelMappingAndRestrict(context.Background(), &gid, "auto")
	require.False(t, restricted)
	require.False(t, result.Mapped)
	require.Equal(t, "auto", result.MappedModel)

	result, _ = svc.ResolveChannelMappingAndRestrict(context.Background(), nil, "auto")
	require.Equal(t, "auto", result.MappedModel)
}

func TestGroupModelAllowlist_AllowsSentinelBaseName(t *testing.T) {
	allowlist := GroupModelAllowlist{Enabled: true, Models: []string{AutoRoundRobinSentinel, "glm-5.3"}}

	require.True(t, allowlist.Allows("auto"), "开启 auto 后必须放行 auto 本身")
	require.True(t, allowlist.Allows("AUTO"))
	require.True(t, allowlist.Allows("glm-5.3"))
	require.False(t, allowlist.Allows("gpt-4o-mini"), "其余模型仍应被白名单拦住")
	require.False(t, allowlist.Allows("auto::rand"))

	// 自定义书写名同样放行
	custom := GroupModelAllowlist{Enabled: true, Models: []string{"smart::rand", "glm-5.3"}}
	require.True(t, custom.Allows("smart"))
	require.False(t, custom.Allows("auto"))
}
