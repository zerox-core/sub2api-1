//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 用【线上实测的真实 26 个模型名】作为 source，算出「一次白名单编辑」后的
// 客户端可见列表 —— 这就是被窝 App 彩蛋里最终会显示的东西。
//
// 为什么一个动作能同时满足三个诉求：
//   - 列表可见    = FilterForListing（白名单条目 ∩ source）
//   - 能否调用    = Allows（中间件准入查的就是白名单）
//   - auto 轮询到 = 全组池 = 账号映射键并集 ∩ 白名单（auto_router.go:385）
// 三者都以白名单为唯一判据 => 改白名单即可，无需改任何代码。
//
// 关键区别（本文件要固定住的）：
//   - 9 个子模型挂在 copilot::rr / aistudio::rr 之下 => 删掉字面条目只是"隐藏"，
//     auto 经别名嵌套【仍能】服务它们（组内轮询照旧）。
//   - 4 个非对话模型不挂任何 sentinel => 删掉就【彻底】从 auto 池消失。
// ===========================================================================

// livePool26 是 2026-10-18 从 /models/pool 实测到的真实列表（顺序即实际返回顺序）。
var livePool26 = []string{
	"qwen3.8-omni-flash", "deepseek-v4-pro-0813", "glm-5.2", "deepseek-v4-flash",
	"sensenova-6.8-flash-lite", "auto",
	"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
	"gemini-2.5-flash", "ais-lite35", "gemini-2.5-flash-lite",
	"gemini-3-flash-preview", "gemini-robotics-er-2-preview", "ais-lite31",
	"qwen-mt-uni", "qwen3.7-text-rerank", "qwen3.7-text-embedding-flash",
	"qwen-image-2.1-pro", "qwen-image-3.0", "qwen-image-3.0-pro",
	"copilot", "aistudio", "kimi-k3", "qwen3.8-max-0902", "glm-5.3",
}

// hideChildren9 要隐藏的子模型（保留 copilot / aistudio 分组名）。
var hideChildren9 = []string{
	"copilot-search-merged", "gpt-4-merged", "gpt-4o-merged",
	"ais-lite31", "ais-lite35", "gemini-2.5-flash",
	"gemini-2.5-flash-lite", "gemini-3-flash-preview", "gemini-robotics-er-2-preview",
}

func TestFinalList_OneAllowlistEditProducesTheTargetEgg(t *testing.T) {
	// 线上现状：sentinel + 9 子模型 + 4 待删 + 独立对话模型（全部字面列出）。
	alNow := append([]string{}, livePool26...)
	alNow = append(alNow, "auto::rr", "copilot::rr", "aistudio::rr")

	// 提议：删 9 个子模型 + 删 4 个非对话模型，其余一字不动。
	drop := append(append([]string{}, hideChildren9...), toRemove...)
	alAfter := make([]string, 0, len(alNow))
	for _, m := range alNow {
		if !containsStr(drop, m) {
			alAfter = append(alAfter, m)
		}
	}

	gAfter := &Group{ID: 1499, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: alAfter}}

	listed := gAfter.ModelAllowlist.FilterForListing(livePool26)
	t.Logf("线上 26 个 -> 白名单删除 %d 个 -> 客户端列表 %d 个:", len(drop), len(listed))
	for i, m := range listed {
		t.Logf("   %2d  %s", i, m)
	}

	// ---- 1) 9 个子模型：列表不可见（只留分组名）----
	for _, m := range hideChildren9 {
		require.NotContains(t, listed, m, "子模型 %s 不应出现在列表", m)
	}
	require.Contains(t, listed, "copilot", "copilot 分组名保留")
	require.Contains(t, listed, "aistudio", "aistudio 分组名保留")

	// ---- 2) 4 个非对话模型：列表不可见 + 准入拒绝 ----
	for _, m := range toRemove {
		require.NotContains(t, listed, m, "%s 不应出现在列表", m)
		require.False(t, gAfter.ModelAllowlist.Allows(m), "%s 准入应被拒(404)", m)
	}

	// ---- 3) 其余对话模型：一个都没少 ----
	expectKeep := []string{
		"deepseek-v4-pro-0813", "glm-5.2", "deepseek-v4-flash",
		"sensenova-6.8-flash-lite", "auto", "qwen-mt-uni",
		"qwen3.7-text-rerank", "qwen3.7-text-embedding-flash",
		"kimi-k3", "qwen3.8-max-0902", "glm-5.3",
	}
	for _, m := range expectKeep {
		require.Contains(t, listed, m, "%s 必须保持不变", m)
		require.True(t, gAfter.ModelAllowlist.Allows(m), "%s 仍可调用", m)
	}

	require.ElementsMatch(t,
		append(append([]string{}, expectKeep...), "copilot", "aistudio"),
		listed, "列表 = 11 个对话模型 + 2 个分组名")

	// ---- 4) 白名单条目数 ----
	t.Logf("白名单条目: %d -> %d", len(alNow), len(alAfter))
	require.Equal(t, len(alNow)-len(drop), len(alAfter))

	t.Log("=> 被窝 App 彩蛋最终显示 13 行（列表 13 个，其中 auto 由 App 自己滤掉）")
	t.Log("=> 生图不受影响：g24「羊毛组生图」/ g5「国模」仍持有 qwen-image-*；")
	t.Log("   被窝出图走 newapi.livzon.cn 的 doubao-seedream-5-0-260128，与本组白名单无关")
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
