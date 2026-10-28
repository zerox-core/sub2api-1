//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 需求：把 4 个非对话模型从白名单里【字面删除】——
//   qwen3.8-omni-flash（视觉备用，用户要求"完全去除，不要轮询调用到"）
//   qwen-image-2.1-pro / qwen-image-3.0 / qwen-image-3.0-pro（生图用，不当对话模型）
//
// 验证删除后同时满足三件事：
//   1) 列表不再出现（客户端看不到）；
//   2) 直接按名字调用被拒（Allows=false => 中间件 404，即"完全去除"）；
//   3) auto 池不再命中它们（"不要轮询调用到"）。
//
// 这三点都由"白名单字面删除"这一个动作达成，因为：
//   - FilterForListing 只输出白名单条目 ∩ source；
//   - auto 全组池 = 映射键并集 ∩ 白名单（auto_router.go:385）；
//   - 中间件准入查的就是白名单（group_model_allowlist.go:75）。
// ===========================================================================

// prodGroup14WithExtras 在真实 g14 形状上补一个声明这 4 个模型的账号（对应 acct 43）。
func prodGroup14WithExtras(allowlist []string) (*Group, []Account) {
	copilot := accountWithMapping(
		"copilot", "copilot",
		"copilot-search-merged", "copilot-search-merged",
		"gpt-4-merged", "gpt-4-merged",
		"gpt-4o-merged", "gpt-4o-merged",
	)
	copilot.ID = 45

	aistudio := accountWithMapping(
		"aistudio", "aistudio",
		"ais-lite31", "ais-lite31",
		"ais-lite35", "ais-lite35",
		"gemini-2.5-flash", "gemini-2.5-flash",
		"gemini-2.5-flash-lite", "gemini-2.5-flash-lite",
		"gemini-3-flash-preview", "gemini-3-flash-preview",
		"gemini-robotics-er-2-preview", "gemini-robotics-er-2-preview",
	)
	aistudio.ID = 46

	// acct 43（百炼）：同时声明 4 个待删模型 + 若干要保留的对话模型。
	bailian := accountWithMapping(
		"qwen3.8-omni-flash", "qwen3.8-omni-flash",
		"qwen-image-2.1-pro", "qwen-image-2.1-pro",
		"qwen-image-3.0", "qwen-image-3.0",
		"qwen-image-3.0-pro", "qwen-image-3.0-pro",
		"glm-5.2", "glm-5.2",
		"deepseek-v4-flash", "deepseek-v4-flash",
		"sensenova-6.8-flash-lite", "sensenova-6.8-flash-lite",
	)
	bailian.ID = 43

	g := &Group{ID: 1414, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: allowlist}}
	return g, []Account{copilot, aistudio, bailian}
}

// toRemove 是用户要求从列表里去掉的 4 个模型。
var toRemove = []string{
	"qwen3.8-omni-flash",
	"qwen-image-2.1-pro", "qwen-image-3.0", "qwen-image-3.0-pro",
}

// keepThese 是必须保持不变的对话模型。
var keepThese = []string{"glm-5.2", "deepseek-v4-flash", "sensenova-6.8-flash-lite"}

func TestRemoveFour_AllThreeEffectsHoldTogether(t *testing.T) {
	base := []string{"auto::rr", "copilot::rr", "aistudio::rr"}
	base = append(base, keepThese...)
	alNow := append(append([]string{}, base...), toRemove...)
	alAfter := append([]string{}, base...) // 只删这 4 个

	reach := func(al []string) map[string]int {
		g, accts := prodGroup14WithExtras(al)
		r, _, _ := newAutoRouterForTest(t, g, accts, newMiniRedis(t))
		served := map[string]int{}
		for i := 0; i < 600; i++ {
			if got, ok := r.Resolve(context.Background(), 1414, "auto"); ok {
				served[got]++
			}
		}
		return served
	}

	now, after := reach(alNow), reach(alAfter)
	t.Logf("删除前 auto 可达(%d): %v", len(now), keysOf(now))
	t.Logf("删除后 auto 可达(%d): %v", len(after), keysOf(after))

	gBefore, _ := prodGroup14WithExtras(alNow)
	gAfter, _ := prodGroup14WithExtras(alAfter)

	// ---- 1) 列表：删除后不可见 ----
	src := append(append([]string{}, toRemove...), keepThese...)
	src = append(src, "copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
		"ais-lite31", "ais-lite35", "gemini-2.5-flash", "gemini-2.5-flash-lite",
		"gemini-3-flash-preview", "gemini-robotics-er-2-preview")

	listBefore := gBefore.ModelAllowlist.FilterForListing(src)
	listAfter := gAfter.ModelAllowlist.FilterForListing(src)
	t.Logf("列表 删除前(%d)=%v", len(listBefore), listBefore)
	t.Logf("列表 删除后(%d)=%v", len(listAfter), listAfter)

	for _, m := range toRemove {
		require.Contains(t, listBefore, m, "删除前 %s 可见", m)
		require.NotContains(t, listAfter, m, "【1】删除后 %s 不可见", m)
	}
	// 其余保持不变
	for _, m := range keepThese {
		require.Contains(t, listAfter, m, "%s 必须保持不变", m)
	}
	require.Contains(t, listAfter, "copilot", "copilot 分组名保留")
	require.Contains(t, listAfter, "aistudio", "aistudio 分组名保留")
	require.Contains(t, listAfter, "auto", "auto 保留")

	// ---- 2) 准入：删除后按名字直接调用被拒 ----
	for _, m := range toRemove {
		require.True(t, gBefore.ModelAllowlist.Allows(m), "删除前 %s 可调用", m)
		require.False(t, gAfter.ModelAllowlist.Allows(m), "【2】删除后 %s 准入被拒(404)", m)
	}
	require.True(t, gAfter.ModelAllowlist.Allows("copilot"), "copilot 仍可调用")
	require.True(t, gAfter.ModelAllowlist.Allows("auto"), "auto 仍可调用")
	for _, m := range keepThese {
		require.True(t, gAfter.ModelAllowlist.Allows(m), "%s 仍可调用", m)
	}

	// ---- 3) auto 池：删除后不再轮询到这 4 个 ----
	for _, m := range toRemove {
		require.NotZero(t, now[m], "删除前 auto 会轮询到 %s", m)
		require.Zero(t, after[m], "【3】删除后 auto 不再轮询到 %s", m)
	}
	// 保留的仍在 auto 池里
	for _, m := range keepThese {
		require.NotZero(t, after[m], "%s 仍在 auto 池", m)
	}
	// copilot / aistudio 的 9 个子模型经别名嵌套仍在
	for _, c := range children9 {
		require.NotZero(t, after[c], "子模型 %s 仍经别名可达", c)
	}

	// ---- 4) 生图链不受影响：这 4 个名字本就不在 DIARY 出图/视觉链里 ----
	t.Log("=> 出图链 = doubao-seedream-5-0-260128 @ newapi.livzon.cn（与本组白名单无关）")
	t.Log("=> 视觉链 = deepseek-v4-flash-vision-exp @ newapi.livzon.cn（qwen3.8-omni-flash 仅为未生效的第 3 兜底）")
}
