package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestGeminiThinkingLevelFromReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"none":    "low",
		"minimal": "low",
		"low":     "low",
		"LOW":     "low",
		"medium":  "medium",
		"high":    "high",
		"x-high":  "high",
		"xhigh":   "high",
		"max":     "high",
		"banana":  "",
	}
	for effort, want := range cases {
		require.Equal(t, want, geminiThinkingLevelFromReasoningEffort(effort), "effort=%q", effort)
	}
}

// 回归用例：兼容入口此前只读 Claude thinking 配置，OpenAI 风格客户端传
// reasoning_effort=low 时挡位虽被记入 usage_log，上游却始终跑默认的 high 变体
// ——「能记挡位但挡位不起作用、只能靠换模型 id 表达思考强度」的根因。
func TestGeminiThinkingLevelFromAntigravityCompatPrefersReasoningEffort(t *testing.T) {
	low := "low"
	require.Equal(t, "low", geminiThinkingLevelFromAntigravityCompat(&low, nil))

	// 显式 effort 优先于 thinking budget
	high := "high"
	require.Equal(t, "high", geminiThinkingLevelFromAntigravityCompat(&high, &antigravity.ThinkingConfig{
		Type:         "enabled",
		BudgetTokens: 512,
	}))

	// 未给 effort 时回退到 thinking 配置（保持原有行为）
	require.Equal(t, "low", geminiThinkingLevelFromAntigravityCompat(nil, &antigravity.ThinkingConfig{
		Type:         "enabled",
		BudgetTokens: 512,
	}))
	require.Equal(t, "high", geminiThinkingLevelFromAntigravityCompat(nil, nil))

	// 无法识别的 effort 不得覆盖 thinking 信号
	unknown := "banana"
	require.Equal(t, "low", geminiThinkingLevelFromAntigravityCompat(&unknown, &antigravity.ThinkingConfig{
		Type:         "enabled",
		BudgetTokens: 512,
	}))
}
