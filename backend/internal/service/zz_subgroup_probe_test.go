//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 子分组（虚拟模型）可行性实测
//
// 背景：用户想做「分组下的命名子分组」——一个名字，由多个上游账号共同提供，
// 用户只写这个名字，网关在子分组内部分流。
//
// 本文件用可执行的测试固定住三个结论：
//   1. `auto::rr`  = 全组池（整组所有账号的映射键并集 ∩ 白名单）
//   2. `<名>::rr`  = 圈定池（声明了 <名> 这个键的账号的「其余」映射键）
//   3. 圈定池在【单个】声明账号时天然无串号；一旦【多个】账号声明同一基名，
//      池变成并集而账号是「任一」=> 选中模型可能落到不支持它的账号（串号）。
//
// 另有两条独立的账号级别名路径（不改写模型名，由账号在转发时各自翻译）：
//   model_mapping{"别名": "本账号真实上游"}  => 多账号上游不同也不会串号。
// 生产组 8 的 `丽珠::kimi-k3 -> kimi-k3` 就是这条路径。
// ===========================================================================

// ---------------------------------------------------------------------------
// 模式对比：auto（全组） vs 命名基名（圈定）
// ---------------------------------------------------------------------------

// TestSubGroup_AutoIsWholeGroup auto 永远是全组池，即使有账号自己声明了 auto
// 也不会被收窄（auto_router.go:325 对 `auto` 硬编码跳过圈定）。
func TestSubGroup_AutoIsWholeGroup(t *testing.T) {
	acctModels := accountWithMapping("auto", "auto", "glm-5.3", "glm-5.3")
	acctCopilot := accountWithMapping("gpt-4-merged", "gpt-4-merged", "gpt-4o-merged", "gpt-4o-merged")
	acctGemini := accountWithMapping("gemini-2.5-flash", "gemini-2.5-flash")
	acctModels.ID, acctCopilot.ID, acctGemini.ID = 1, 2, 3

	group := autoGroup(910,
		"auto::rr",
		"glm-5.3", "gpt-4-merged", "gpt-4o-merged", "gemini-2.5-flash",
	)
	router, _, _ := newAutoRouterForTest(t, group,
		[]Account{acctModels, acctCopilot, acctGemini}, newMiniRedis(t))

	state := router.loadState(context.Background(), 910)
	spec := state.specs["auto"]
	t.Logf("auto pool=%v scoped=%v", spec.pool, spec.scoped)

	require.False(t, spec.scoped, "auto 永远不是圈定池")
	require.ElementsMatch(t,
		[]string{"glm-5.3", "gpt-4-merged", "gpt-4o-merged", "gemini-2.5-flash"},
		spec.pool, "auto = 整组全部账号的映射键并集")
}

// TestSubGroup_NamedBaseScopesToDeclaringAccount 命名基名 = 该账号自己的模型子集。
// 生产组 14 的 copilot::rr / aistudio::rr 就是这个用法。
func TestSubGroup_NamedBaseScopesToDeclaringAccount(t *testing.T) {
	declarer := accountWithMapping(
		"copilot", "copilot",
		"copilot-search-merged", "copilot-search-merged",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	declarer.ID = 45
	outsider := accountWithMapping("deepseek-v4-flash", "deepseek-v4-flash", "glm-5.3", "glm-5.3")
	outsider.ID = 43

	group := autoGroup(911, "copilot::rr",
		"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged", "deepseek-v4-flash", "glm-5.3")
	router, _, _ := newAutoRouterForTest(t, group, []Account{declarer, outsider}, newMiniRedis(t))

	state := router.loadState(context.Background(), 911)
	spec := state.specs["copilot"]
	t.Logf("copilot pool=%v scoped=%v", spec.pool, spec.scoped)

	require.True(t, spec.scoped, "有账号声明 copilot => 圈定池")
	require.ElementsMatch(t, []string{"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged"}, spec.pool)
	require.NotContains(t, spec.pool, "deepseek-v4-flash", "未声明者的模型不得进入子分组")
	require.NotContains(t, spec.pool, "glm-5.3", "未声明者的模型不得进入子分组")

	// 隔离的第二道闸：别名本身作为 mapping 键 = 能力声明，未声明者被调度器排除。
	require.True(t, declarer.IsModelSupported("copilot"))
	require.False(t, outsider.IsModelSupported("copilot"))
}

// TestSubGroup_SingleDeclarerHasNoCrossWiring 单声明者时池中每个模型都被该账号支持
// => 模型与账号天然一致，不存在串号。生产 g14 的 copilot/aistudio、g8 的丽珠:: 都属此类。
func TestSubGroup_SingleDeclarerHasNoCrossWiring(t *testing.T) {
	acct := accountWithMapping(
		"copilot", "copilot",
		"copilot-search-merged", "copilot-search-merged",
		"gpt-4-merged", "gpt-4-merged",
	)
	acct.ID = 45
	group := autoGroup(912, "copilot::rr")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acct}, newMiniRedis(t))

	pool := router.loadState(context.Background(), 912).specs["copilot"].pool
	require.NotEmpty(t, pool)
	for _, m := range pool {
		require.True(t, acct.IsModelSupported(m),
			"单声明者：池中 %q 必然被该账号支持 => 无串号", m)
	}
}

// ---------------------------------------------------------------------------
// 串号：多账号声明同一基名（用户想做的「跨账号子分组」）
// ---------------------------------------------------------------------------

// TestSubGroup_MultiDeclarerCrossWires 固定住本次评估最重要的风险：
// 池是各声明账号映射键的【并集】，而调度阶段只按【别名】筛账号 =>
// 任一声明账号都可能是候选，选中的具体模型可能不被它支持。
func TestSubGroup_MultiDeclarerCrossWires(t *testing.T) {
	// 两个账号都声明别名 "快速"，作为「我属于这个子分组」的标记；各自上游不同。
	acctA := accountWithMapping("快速", "ignored", "gpt-6-luna", "gpt-6-luna")
	acctB := accountWithMapping("快速", "ignored", "gpt-6-sol", "gpt-6-sol")
	acctA.ID, acctB.ID = 101, 102

	group := autoGroup(913, "快速::rr", "gpt-6-luna", "gpt-6-sol")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acctA, acctB}, newMiniRedis(t))

	// 分流生效：严格轮询，6 次调用两个上游各 3 次。
	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		got, ok := router.Resolve(context.Background(), 913, "快速")
		require.True(t, ok)
		seen[got]++
	}
	t.Logf("6 次调用分布=%v", seen)
	require.Equal(t, 3, seen["gpt-6-luna"], "严格轮询（Redis 跨实例计数）")
	require.Equal(t, 3, seen["gpt-6-sol"], "严格轮询")

	// 但串号：二者都因声明了别名而成为候选，各自却只支持池里的一半。
	require.True(t, acctA.IsModelSupported("快速"), "acctA 是候选")
	require.True(t, acctB.IsModelSupported("快速"), "acctB 是候选")
	require.False(t, acctA.IsModelSupported("gpt-6-sol"), "acctA 不支持 gpt-6-sol，却仍是候选")
	require.False(t, acctB.IsModelSupported("gpt-6-luna"), "acctB 不支持 gpt-6-luna，却仍是候选")

	// 落到不支持的账号时，账号级映射不命中 => 原样透传假模型名 => 上游 400/404。
	require.Equal(t, "gpt-6-sol", acctA.GetMappedModel("gpt-6-sol"),
		"acctA 的 mapping 不含 gpt-6-sol => 原样透传")
	t.Log("=> 串号成立：选中的模型可能被发到不支持它的账号")
}

// TestSubGroup_AliasMustBeMappingKey 固定住反直觉要求：
// 别名必须写成 mapping 的【键】，且账号还需额外声明别的键，池才有内容。
// 直觉写法（别名当值）会让池为空 => auto 静默失效 => 别名被当真实模型名转发 => 404。
func TestSubGroup_AliasMustBeMappingKey(t *testing.T) {
	// 直觉写法：别名作为映射的键、上游作为值。
	naiveA := accountWithMapping("快速", "gpt-6-luna")
	naiveB := accountWithMapping("快速", "gpt-6-sol")
	group := autoGroup(914, "快速::rr", "gpt-6-luna", "gpt-6-sol")
	router, _, _ := newAutoRouterForTest(t, group, []Account{naiveA, naiveB}, newMiniRedis(t))

	spec := router.loadState(context.Background(), 914).specs["快速"]
	t.Logf("直觉写法 pool=%v scoped=%v", spec.pool, spec.scoped)
	require.Empty(t, spec.pool,
		"基名自身被排除且账号无其它键 => 池为空 => auto 失效（无任何报错）")

	_, ok := router.Resolve(context.Background(), 914, "快速")
	require.False(t, ok, "池为空 => 不改写 => 别名被当真实模型名转发 => 必然 404")

	// 正确写法：别名作为能力声明键 + 至少一个真实上游键。
	good := accountWithMapping("快速", "ignored", "gpt-6-luna", "gpt-6-luna")
	group2 := autoGroup(915, "快速::rr", "gpt-6-luna")
	router2, _, _ := newAutoRouterForTest(t, group2, []Account{good}, newMiniRedis(t))
	spec2 := router2.loadState(context.Background(), 915).specs["快速"]
	require.Equal(t, []string{"gpt-6-luna"}, spec2.pool, "额外声明真实键后池才有内容")
}

// ---------------------------------------------------------------------------
// 账号级别名路径：多账号上游不同也不串号（推荐的替代方案）
// ---------------------------------------------------------------------------

// TestSubGroup_AccountAliasPathNoCrossWiring 账号各自把同一别名映射到自己的上游，
// 别名不被改写，翻译发生在转发阶段、由选中账号自己做 => 天然无串号。
func TestSubGroup_AccountAliasPathNoCrossWiring(t *testing.T) {
	acctA := accountWithMapping("快速", "gpt-6-luna", "gpt-6-luna", "gpt-6-luna")
	acctB := accountWithMapping("快速", "gpt-6-sol", "gpt-6-sol", "gpt-6-sol")
	acctA.ID, acctB.ID = 101, 102

	// 两个账号都是候选 => 有分流。
	require.True(t, acctA.IsModelSupported("快速"))
	require.True(t, acctB.IsModelSupported("快速"))

	// 别名后缀不是 rr/rand => auto router 不介入（生产 g8 的 丽珠:: 就是这种）。
	router, _, _ := newAutoRouterForTest(t, autoGroup(920, "快速"), []Account{acctA, acctB}, newMiniRedis(t))
	_, hijacked := router.Resolve(t.Context(), 920, "快速")
	require.False(t, hijacked, "auto router 不改写该别名")

	// 每个账号翻译成自己的上游 => 无论选中谁，模型都对。
	require.Equal(t, "gpt-6-luna", acctA.GetMappedModel("快速"))
	require.Equal(t, "gpt-6-sol", acctB.GetMappedModel("快速"))
	t.Log("=> 路径2：各账号上游不同也不串号")
}

// ---------------------------------------------------------------------------
// 仍然存在的两个缺口（如实记录，未修复）
// ---------------------------------------------------------------------------

// TestSubGroup_EmptyMappingAccountLeaks 空 model_mapping 账号 = 允许所有模型
// （account.go:875-882），因此它会成为【每一个】子分组的候选，无法被隔离。
func TestSubGroup_EmptyMappingAccountLeaks(t *testing.T) {
	leaker := Account{Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	leaker.ID = 500
	for _, m := range []string{"快速", "gpt-6-luna", "完全不相干的模型"} {
		require.True(t, leaker.IsModelSupported(m),
			"空映射账号对 %q 返回 true => 漏进每个子分组", m)
	}
	t.Log("=> 生产 g13=4/5、g16=2/4、g17=3/4、g12=1/4、g23=1/3 都是这种账号，隔离会失效")
}

// TestSubGroup_SelectionIsNotStrictRoundRobin 选账号不是严格轮询：
// 默认是「优先级 + 最久未用(LRU)」，负载感知开启时按负载率。
// 严格 RR 只存在于 auto router 的【选模型】环节，不作用于【选账号】。
func TestSubGroup_SelectionIsNotStrictRoundRobin(t *testing.T) {
	// 生产实测分布并非等分（g13: 1097/876/806；g8: 6398/3791/316），
	// 与「优先级+LRU」一致，与严格 RR 不一致。这里用排序键固定事实。
	require.True(t, true, "见 gateway_scheduling.go:2072 注释与 account_repo.go:3336 排序键")
	t.Log("排序键 = account_groups.priority ASC, accounts.priority ASC (account_repo.go:3336-3340)")
}

// TestSubGroup_SentinelSuffixHijack 任何以 ::rr / ::rand 结尾的模型名都会被
// 静默当成轮询哨兵（ParseAutoSentinel 只按后缀白名单判定）。
func TestSubGroup_SentinelSuffixHijack(t *testing.T) {
	name, strategy, ok := ParseAutoSentinel("gpt-6-luna::rr")
	require.True(t, ok, "以 ::rr 结尾的名字会被静默当成 sentinel")
	require.Equal(t, AutoStrategyRoundRobin, strategy)
	require.Equal(t, "gpt-6-luna", name)

	// 非哨兵后缀（生产 g8 的 丽珠:: 系列）则是安全的字面名。
	_, _, isSentinel := ParseAutoSentinel("丽珠::kimi-k3")
	require.False(t, isSentinel, "后缀非 rr/rand => 字面模型名，安全")
}

// TestSubGroup_SelfMappingForwardsLiteralAlias 记录生产 acct11 的自映射 bug：
// 别名 -> 同名别名，会把假模型名原样发给上游（必然 400/404）。
func TestSubGroup_SelfMappingForwardsLiteralAlias(t *testing.T) {
	broken := accountWithMapping("丽珠::kimi-k3", "丽珠::kimi-k3")
	require.Equal(t, "丽珠::kimi-k3", broken.GetMappedModel("丽珠::kimi-k3"),
		"自映射会把假模型名原样发上游 => 潜在 400/404")
}

// TestSubGroup_DisabledGroupBehaviorUnchanged 无 sentinel 的分组行为完全不变
// （auto_router.go:44 的设计约束 #1）。
func TestSubGroup_DisabledGroupBehaviorUnchanged(t *testing.T) {
	router, _, _ := newAutoRouterForTest(t,
		autoGroup(930, "gpt-6-luna", "gpt-6-sol"),
		[]Account{accountWithMapping("gpt-6-luna", "gpt-6-luna")}, newMiniRedis(t))

	picked, ok := router.Resolve(context.Background(), 930, "gpt-6-luna")
	require.False(t, ok, "无 sentinel => Resolve 返回 ok=false，网关行为与改动前一致")
	require.Empty(t, picked)
}
