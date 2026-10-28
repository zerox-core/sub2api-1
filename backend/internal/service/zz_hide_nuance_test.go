//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 关键差异：隐藏子模型对「圈定 sentinel」和「全组 auto」影响不同。
//
//   圈定池（copilot::rr 等）：池来自账号 mapping 的键，【不查白名单】
//       (auto_router.go:375-381) => 删掉字面条目【不影响】调用 => 可安全隐藏。
//
//   全组池（auto::rr）      ：池 = 全组账号映射键并集 【∩ 白名单】
//       (auto_router.go:385) => 删掉字面条目会【同时】把它从 auto 的池里删掉。
//
// 所以：想让客户端只看到子分组名，
//   - 子分组是「单/多账号圈定」（copilot::rr、aistudio::rr）：删字面条目即可，安全。
//   - 但同一个模型若也是 auto 的候选，删掉会顺带削弱 auto。
// ===========================================================================

// TestHide_ScopedPoolIgnoresAllowlist 圈定池不查白名单 => 隐藏安全。
func TestHide_ScopedPoolIgnoresAllowlist(t *testing.T) {
	acct := accountWithMapping(
		"copilot", "copilot",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	acct.ID = 45

	// 白名单【只】留 sentinel，子模型字面条目全删。
	group := autoGroup(950, "copilot::rr")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acct}, newMiniRedis(t))

	pool := router.loadState(context.Background(), 950).specs["copilot"].pool
	t.Logf("圈定池=%v（白名单只有 copilot::rr）", pool)
	require.ElementsMatch(t, []string{"gpt-4-merged", "gpt-4o-merged"}, pool,
		"圈定池来自账号 mapping，与白名单无关 => 删字面条目不影响分发")

	// 且能正常调用、轮询覆盖两个子模型。
	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		got, ok := router.Resolve(context.Background(), 950, "copilot")
		require.True(t, ok)
		seen[got]++
	}
	t.Logf("4 次调用=%v", seen)
	require.Len(t, seen, 2, "两个子模型都被轮询到")
	t.Log("=> 圈定子分组：可安全隐藏子模型（列表只显示子分组名）")
}

// TestHide_FullGroupPoolIsIntersectedWithAllowlist 全组 auto 的池会被白名单裁剪。
func TestHide_FullGroupPoolIsIntersectedWithAllowlist(t *testing.T) {
	acct := accountWithMapping(
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
		"glm-5.3", "glm-5.3",
	)
	acct.ID = 43

	// 白名单列出全部三个 => 三个都进 auto 池。
	groupAll := autoGroup(951, "auto::rr", "gpt-4-merged", "gpt-4o-merged", "glm-5.3")
	routerAll, _, _ := newAutoRouterForTest(t, groupAll, []Account{acct}, newMiniRedis(t))
	poolAll := routerAll.loadState(context.Background(), 951).specs["auto"].pool
	t.Logf("白名单列全 3 个 -> auto 池=%v", poolAll)
	require.Len(t, poolAll, 3)

	// 若为「隐藏」而删掉 gpt-4-merged / gpt-4o-merged 字面条目：
	groupHidden := autoGroup(952, "auto::rr", "glm-5.3")
	routerHidden, _, _ := newAutoRouterForTest(t, groupHidden, []Account{acct}, newMiniRedis(t))
	poolHidden := routerHidden.loadState(context.Background(), 952).specs["auto"].pool
	t.Logf("白名单只留 1 个  -> auto 池=%v", poolHidden)
	require.Equal(t, []string{"glm-5.3"}, poolHidden,
		"全组池 ∩ 白名单：删字面条目会连带把该模型从 auto 池移除")

	t.Log("=> 注意：auto::rr 不能靠删字面条目隐藏——会同时削弱 auto 自身")
}

// TestHide_WorksWhenScopedSentinelReplacesAuto 当子分组替代 auto 时可干净隐藏。
func TestHide_WorksWhenScopedSentinelReplacesAuto(t *testing.T) {
	acctA := accountWithMapping(
		"copilot", "copilot",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	acctA.ID = 45
	acctB := accountWithMapping(
		"aistudio", "aistudio",
		"gemini-2.5-flash", "gemini-2.5-flash",
	)
	acctB.ID = 46

	// 白名单只有两个圈定 sentinel，没有任何字面模型名。
	group := autoGroup(953, "copilot::rr", "aistudio::rr")
	router, _, _ := newAutoRouterForTest(t, group, []Account{acctA, acctB}, newMiniRedis(t))

	// 列表：只应看到两个子分组名。
	listed := group.ModelAllowlist.FilterForListing([]string{
		"gpt-4-merged", "gpt-4o-merged", "gemini-2.5-flash",
	})
	t.Logf("客户端列表=%v", listed)
	require.ElementsMatch(t, []string{"copilot", "aistudio"}, listed,
		"客户端只看到子分组名")

	// 调用：两个子分组都能正常工作。
	for _, name := range []string{"copilot", "aistudio"} {
		require.True(t, group.ModelAllowlist.Allows(name), "%s 准入放行", name)
		picked, ok := router.Resolve(context.Background(), 953, name)
		require.True(t, ok, "%s 调用命中", name)
		t.Logf("  %s -> %s", name, picked)
	}
	t.Log("=> 最干净形态：白名单只放子分组名，子模型完全隐藏且可调用")
}
