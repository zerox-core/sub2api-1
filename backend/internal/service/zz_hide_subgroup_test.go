//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 需求：把「子分组」下的具体模型从客户端列表里隐藏，只暴露子分组名，
//       但仍能通过子分组名正常调用。
//
// 结论：白名单里【只留 sentinel 条目、删掉具体子模型名】即可 —— 纯配置，零代码。
//
// 依据：
//   - FilterForListing 对 sentinel 条目【无条件】输出客户端书写名（基名），
//     且只有同时被字面列出的模型才会一并出现 => 删掉字面条目 = 隐藏。
//   - 圈定池（scoped）的候选模型来自【账号 model_mapping 的键】，不查白名单
//     （auto_router.go:375-381 `out = append(out, name); continue`）
//     => 删掉字面条目不影响调用。
//   - 请求准入校验的是客户端书写名（基名），sentinel 放行基名
//     （group_model_allowlist.go:177-181）=> 调用不被 404。
// ===========================================================================

// TestHide_ListingShowsChildrenWhenLiterallyListed 现状：字面列出 => 子模型可见。
func TestHide_ListingShowsChildrenWhenLiterallyListed(t *testing.T) {
	// 组 14 现状：sentinel + 子模型【都】字面列出。
	al := GroupModelAllowlist{
		Enabled: true,
		Models: []string{
			"copilot::rr",
			"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
		},
	}
	// source = 该组账号能提供的模型（子模型）。
	source := []string{"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged", "其它模型"}

	got := al.FilterForListing(source)
	t.Logf("列表=%v", got)

	require.Contains(t, got, "copilot", "sentinel 输出基名")
	require.Contains(t, got, "copilot-search-merged", "字面列出 => 子模型也可见（现状）")
	require.Contains(t, got, "gpt-4-merged")
	require.Contains(t, got, "gpt-4o-merged")
	t.Log("=> 现状：客户端同时看到 copilot 和它的 3 个子模型")
}

// TestHide_SentinelOnlyHidesChildren 目标：白名单只留 sentinel => 子模型隐藏。
func TestHide_SentinelOnlyHidesChildren(t *testing.T) {
	// 只留 sentinel，删掉子模型字面条目。
	al := GroupModelAllowlist{
		Enabled: true,
		Models:  []string{"copilot::rr"},
	}
	source := []string{"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged"}

	got := al.FilterForListing(source)
	t.Logf("列表=%v", got)

	require.Equal(t, []string{"copilot"}, got,
		"只应看到子分组名 copilot，子模型全部隐藏")
	t.Log("=> 隐藏成功：子模型不在列表里")
}

// TestHide_HiddenChildrenStillCallable 关键：隐藏后仍能通过子分组名调用。
func TestHide_HiddenChildrenStillCallable(t *testing.T) {
	// 白名单只留 sentinel。
	al := GroupModelAllowlist{Enabled: true, Models: []string{"copilot::rr"}}

	// 1) 准入：客户端写 "copilot" 必须被放行（否则调用会被 404）。
	require.True(t, al.Allows("copilot"),
		"sentinel 放行客户端书写名 => 调用不被白名单拦截")

	// 2) 隐藏的子模型名本身应被拒绝（它们已不在白名单里）。
	require.False(t, al.Allows("gpt-4-merged"),
		"子模型已从白名单删除 => 客户端无法直接点名调用（这正是「隐藏」的准入侧）")

	// 3) 圈定池来自账号 mapping，不查白名单 => 删字面条目不影响分发。
	acct := accountWithMapping(
		"copilot", "copilot",
		"copilot-search-merged", "copilot-search-merged",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	acct.ID = 45
	group := autoGroup(940, "copilot::rr") // 白名单只有 sentinel
	router, _, _ := newAutoRouterForTest(t, group, []Account{acct}, newMiniRedis(t))

	pool := router.loadState(context.Background(), 940).specs["copilot"].pool
	t.Logf("圈定池=%v", pool)
	require.ElementsMatch(t,
		[]string{"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged"}, pool,
		"池来自账号 mapping => 子模型虽被隐藏，仍参与分发")

	// 4) 实际调用：Resolve 把 copilot 改写成具体子模型。
	for i := 0; i < 3; i++ {
		picked, ok := router.Resolve(context.Background(), 940, "copilot")
		require.True(t, ok, "通过子分组名调用必须命中 auto")
		require.Contains(t, pool, picked, "改写为池中某个真实子模型")
		t.Logf("第 %d 次调用: copilot -> %s", i+1, picked)
	}
	t.Log("=> 隐藏但可调用：客户端只写 copilot，网关内部展开为具体子模型")
}

// TestHide_MultiChildAcrossAccountsCanHideSimilarly 多账号子分组同样可隐藏。
func TestHide_MultiChildAcrossAccountsCanHideSimilarly(t *testing.T) {
	al := GroupModelAllowlist{Enabled: true, Models: []string{"快速::rr"}}
	got := al.FilterForListing([]string{"gpt-6-luna", "gpt-6-sol"})
	require.Equal(t, []string{"快速"}, got, "只暴露子分组名")

	require.True(t, al.Allows("快速"))
	require.False(t, al.Allows("gpt-6-luna"), "子模型被隐藏（不可直接点名）")
	require.False(t, al.Allows("gpt-6-sol"))

	acctA := accountWithMapping("快速", "ignored", "gpt-6-luna", "gpt-6-luna")
	acctB := accountWithMapping("快速", "ignored", "gpt-6-sol", "gpt-6-sol")
	acctA.ID, acctB.ID = 101, 102
	group := autoGroup(941, "快速::rr")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acctA, acctB}, newMiniRedis(t))

	pool := router.loadState(context.Background(), 941).specs["快速"].pool
	require.ElementsMatch(t, []string{"gpt-6-luna", "gpt-6-sol"}, pool)
	t.Logf("多账号圈定池=%v（可通过子分组名调用，列表只显示 快速）", pool)
}

// TestHide_UnlistedModelStillRefused 确认隐藏有真实效果：白名单外的模型被拒。
func TestHide_UnlistedModelStillRefused(t *testing.T) {
	al := GroupModelAllowlist{Enabled: true, Models: []string{"copilot::rr"}}
	for _, m := range []string{"gpt-4-merged", "gemini-2.5-flash", "随便一个模型"} {
		require.False(t, al.Allows(m), "%q 不在白名单 => 拒绝", m)
	}
	require.True(t, al.Allows("copilot"))
	require.True(t, al.Allows("COPILOT"), "大小写不敏感")
}
