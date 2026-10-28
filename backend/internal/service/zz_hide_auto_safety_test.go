//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 修正早前的说法：删掉字面子模型，auto 是否还能服务它们？
//
//   auto 池 = 全组账号映射键并集 ∩ 白名单
//   但别名本身（copilot / aistudio）会留在池里，因为白名单含 copilot::rr
//   => Allows("copilot") 归一化为基名后放行
//   而 pickModel :441 会把别名【再展开一层】，用的是该别名的【圈定池】，
//   圈定池【不查白名单】=> 子模型仍然可达。
//
// 结论：只要子模型挂在某个「也在白名单里的圈定 sentinel」之下，
//       删掉它们的字面条目【不会】让 auto 失去它们 —— 隐藏是安全的。
//       反之，不属于任何 sentinel 的独立模型，删掉就从 auto 池消失。
// ===========================================================================

// prodGroup14 按生产组 14 的真实形状构造。
func prodGroup14(allowlist []string) (*Group, []Account) {
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

	// 独立模型（不挂任何 sentinel）。
	others := accountWithMapping(
		"glm-5.3", "glm-5.3",
		"deepseek-v4-flash", "deepseek-v4-flash",
		"kimi-k3", "kimi-k3",
	)
	others.ID = 43

	g := &Group{ID: 1400, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: allowlist}}
	return g, []Account{copilot, aistudio, others}
}

// children9 是被窝日记当前可见的 9 个子模型。
var children9 = []string{
	"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
	"ais-lite31", "ais-lite35", "gemini-2.5-flash",
	"gemini-2.5-flash-lite", "gemini-3-flash-preview", "gemini-robotics-er-2-preview",
}

// TestHideChildren_AutoStillServesThemViaAliasNesting 关键验证。
func TestHideChildren_AutoStillServesThemViaAliasNesting(t *testing.T) {
	// 场景 A：现在的白名单（sentinel + 9 个字面子模型 + 独立模型）。
	alNow := append([]string{"auto::rr", "copilot::rr", "aistudio::rr"}, children9...)
	alNow = append(alNow, "glm-5.3", "deepseek-v4-flash", "kimi-k3")

	// 场景 B：隐藏后 —— 只留 sentinel + 独立模型，删掉 9 个字面条目。
	alHidden := []string{"auto::rr", "copilot::rr", "aistudio::rr",
		"glm-5.3", "deepseek-v4-flash", "kimi-k3"}

	reach := func(al []string) map[string]int {
		g, accts := prodGroup14(al)
		r, _, _ := newAutoRouterForTest(t, g, accts, newMiniRedis(t))
		served := map[string]int{}
		for i := 0; i < 400; i++ {
			if got, ok := r.Resolve(context.Background(), 1400, "auto"); ok {
				served[got]++
			}
		}
		return served
	}

	now := reach(alNow)
	hidden := reach(alHidden)
	t.Logf("现在   auto 可达集合(%d): %v", len(now), keysOf(now))
	t.Logf("隐藏后 auto 可达集合(%d): %v", len(hidden), keysOf(hidden))

	// 关键：9 个子模型在隐藏后是否仍被 auto 服务？
	lost := []string{}
	for _, c := range children9 {
		if _, ok := hidden[c]; !ok {
			lost = append(lost, c)
		}
	}
	t.Logf("隐藏后 auto 失去的子模型: %v", lost)

	require.Empty(t, lost,
		"9 个子模型挂在圈定 sentinel 之下 => auto 经别名嵌套仍可达 => 隐藏安全")
	t.Log("=> 修正：删掉字面子模型【不会】让 auto 失去它们（经由 copilot/aistudio 别名展开）")
}

// TestHideChildren_StandaloneModelsAreLostFromAuto 反例：独立模型无别名可绕。
func TestHideChildren_StandaloneModelsAreLostFromAuto(t *testing.T) {
	alNow := []string{"auto::rr", "copilot::rr", "aistudio::rr", "glm-5.3", "deepseek-v4-flash"}
	alHidden := []string{"auto::rr", "copilot::rr", "aistudio::rr"} // 删掉独立模型

	reach := func(al []string) map[string]int {
		g, accts := prodGroup14(al)
		r, _, _ := newAutoRouterForTest(t, g, accts, newMiniRedis(t))
		served := map[string]int{}
		for i := 0; i < 300; i++ {
			if got, ok := r.Resolve(context.Background(), 1400, "auto"); ok {
				served[got]++
			}
		}
		return served
	}

	now := reach(alNow)
	hidden := reach(alHidden)
	t.Logf("现在   : %v", keysOf(now))
	t.Logf("删独立 : %v", keysOf(hidden))

	_, hasGlm := hidden["glm-5.3"]
	require.False(t, hasGlm,
		"独立模型不挂 sentinel => 删掉字面条目后 auto 确实失去它")
	t.Log("=> 反例成立：非 sentinel 旗下的模型，删了就真没了")
}

// TestHideChildren_ScopedAliasStillDirectlyCallable 隐藏后别名本身仍可调用。
func TestHideChildren_ScopedAliasStillDirectlyCallable(t *testing.T) {
	alHidden := []string{"auto::rr", "copilot::rr", "aistudio::rr"}
	g, accts := prodGroup14(alHidden)
	r, _, _ := newAutoRouterForTest(t, g, accts, newMiniRedis(t))

	for _, alias := range []string{"copilot", "aistudio"} {
		require.True(t, g.ModelAllowlist.Allows(alias), "%s 准入放行", alias)
		seen := map[string]int{}
		for i := 0; i < 40; i++ {
			got, ok := r.Resolve(context.Background(), 1400, alias)
			require.True(t, ok, "%s 可调用", alias)
			seen[got]++
		}
		t.Logf("  %s -> %v", alias, keysOf(seen))
		require.NotEmpty(t, seen)
	}

	// 列表只显示 3 个别名。
	listed := g.ModelAllowlist.FilterForListing(append(append([]string{}, children9...),
		"glm-5.3", "deepseek-v4-flash", "kimi-k3"))
	t.Logf("客户端列表=%v", listed)
	require.ElementsMatch(t, []string{"auto", "copilot", "aistudio"}, listed)
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
