//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 用户的疑问：调用 auto 时，如果轮询到 copilot（别名），会不会最终把
// gpt-4o 这一类 copilot 名下的具体模型也服务出去？
//
// 答案：会。而且比「会」更严重 —— auto 的池里【本来就直接包含】
// copilot / aistudio 这些别名，以及它们名下的所有具体模型。
//
// 依据（auto_router.go）：
//   - buildPool 全组池分支：候选 = 全组账号映射键并集 ∩ 白名单（:385 Allows）
//   - 白名单里有 copilot::rr，Allows("copilot") 因 sentinel 归一化为基名而放行
//     => "copilot" 这个【别名】本身就是一个合法候选模型
//   - pickModel :441 若选中项正好是另一个基名，会【再展开一层】
//     (auto → copilot → gpt-4o-merged)，最多 autoMaxAliasDepth=2 层
// ===========================================================================

// TestAutoNestsIntoCopilotAlias 证明 auto 池包含 copilot 别名，且会被展开。
func TestAutoNestsIntoCopilotAlias(t *testing.T) {
	// 生产组 14 的形状：
	//   acct 45 声明 copilot 别名，其真实上游 gpt-4-merged / gpt-4o-merged
	//   acct 43 有一堆普通模型
	acctCopilot := accountWithMapping(
		"copilot", "copilot",
		"copilot-search-merged", "copilot-search-merged",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	acctCopilot.ID = 45
	acctBig := accountWithMapping("deepseek-v4-flash", "deepseek-v4-flash", "glm-5.3", "glm-5.3")
	acctBig.ID = 43

	// 白名单同时含 auto::rr 和 copilot::rr（生产原样）。
	group := autoGroup(1000,
		"auto::rr", "copilot::rr",
		"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
		"deepseek-v4-flash", "glm-5.3",
	)
	router, _, _ := newAutoRouterForTest(t, group, []Account{acctCopilot, acctBig}, newMiniRedis(t))

	state := router.loadState(context.Background(), 1000)

	// 1) auto 的池（展开前）就包含别名 "copilot" 本身。
	autoPool := state.specs["auto"].pool
	t.Logf("auto pool=%v", autoPool)
	require.Contains(t, autoPool, "copilot",
		"auto 的池里【直接包含】别名 copilot（白名单 sentinel 归一化后放行）")

	// 2) 别名 "copilot" 自身也是一个 spec（会被嵌套展开）。
	_, hasCopilotSpec := state.specs["copilot"]
	require.True(t, hasCopilotSpec, "copilot 是一个独立 spec => 可被嵌套展开")

	// 3) 反复调用 auto，统计最终服务出去的具体模型。
	served := map[string]int{}
	for i := 0; i < 60; i++ {
		got, ok := router.Resolve(context.Background(), 1000, "auto")
		require.True(t, ok)
		served[got]++
	}
	t.Logf("60 次 auto 调用实际服务=%v", served)

	// 4) 关键：copilot 名下的具体模型确实会被 auto 服务出去。
	require.Contains(t, served, "gpt-4-merged",
		"auto 轮询到 copilot 后会展开，最终服务 gpt-4-merged")
	require.Contains(t, served, "gpt-4o-merged",
		"auto 轮询到 copilot 后会展开，最终服务 gpt-4o-merged")
	require.Contains(t, served, "copilot-search-merged")
	t.Log("=> 用户判断正确：auto 会经由 copilot 把 gpt-4o 这类模型服务出去")
}

// TestAutoPoolContainsNestedChildrenWithoutAlias 说明即使 auto 池只直接含别名，
// 展开后的叶子集合仍是那些具体模型。
func TestAutoPoolContainsNestedChildrenWithoutAlias(t *testing.T) {
	acctCopilot := accountWithMapping(
		"copilot", "copilot",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	acctCopilot.ID = 45
	acctBig := accountWithMapping("glm-5.3", "glm-5.3")
	acctBig.ID = 43

	// 白名单【只】给 auto::rr 与 copilot::rr，不给任何字面模型名。
	group := autoGroup(1001, "auto::rr", "copilot::rr")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acctCopilot, acctBig}, newMiniRedis(t))

	state := router.loadState(context.Background(), 1001)
	autoPool := state.specs["auto"].pool
	copilotPool := state.specs["copilot"].pool
	t.Logf("auto pool=%v", autoPool)
	t.Logf("copilot pool=%v", copilotPool)

	// auto 池里是别名，但展开后叶子是具体模型。
	require.Contains(t, autoPool, "copilot")
	require.Contains(t, copilotPool, "gpt-4o-merged")

	served := map[string]int{}
	for i := 0; i < 30; i++ {
		got, _ := router.Resolve(context.Background(), 1001, "auto")
		served[got]++
	}
	t.Logf("30 次 auto 服务=%v", served)
	require.Contains(t, served, "gpt-4o-merged", "展开后仍会服务 copilot 名下的具体模型")
}

// TestAutoAliasDepthIsTwo 固定展开上限：auto → copilot → 叶子，最多 2 层。
func TestAutoAliasDepthIsTwo(t *testing.T) {
	require.Equal(t, 2, autoMaxAliasDepth, "展开深度上限为 2")
	t.Log("auto → 别名 → 叶子 最多两层；三层链（auto→A→B→叶子）不会再展开")
}

// TestRenamingAutoToNonAliasKeepsChildrenOut 对照：若 auto 池里【没有】别名，
// 就不会经由别名展开到 copilot 的模型。
func TestRenamingAutoToNonAliasKeepsChildrenOut(t *testing.T) {
	// 只有普通模型，没有任何嵌套别名。
	acct := accountWithMapping("glm-5.3", "glm-5.3", "deepseek-v4-flash", "deepseek-v4-flash")
	acct.ID = 43
	group := autoGroup(1002, "auto::rr", "glm-5.3", "deepseek-v4-flash")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acct}, newMiniRedis(t))

	served := map[string]int{}
	for i := 0; i < 20; i++ {
		got, ok := router.Resolve(context.Background(), 1002, "auto")
		require.True(t, ok)
		served[got]++
	}
	t.Logf("对照：auto 服务=%v", served)
	require.NotContains(t, served, "copilot", "池里没有别名 => 不会嵌套")
	for m := range served {
		require.Contains(t, []string{"glm-5.3", "deepseek-v4-flash"}, m)
	}
}
