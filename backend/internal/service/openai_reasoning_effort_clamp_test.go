package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSupportedReasoningEffortsForGPT62 锁定 GPT-6.2 一代的真实档位集合。
//
// 依据上游 400 原文："Unsupported value: 'none' is not supported with the
// 'gpt-6.2-sol' model. Supported values are: 'low', 'medium', 'high', 'xhigh',
// and 'max'." 注意这与"未知档位"的 400 不同：后者列的是通用枚举（含 none /
// minimal），照抄会导致 none/minimal 被误放行后继续 400。
func TestSupportedReasoningEffortsForGPT62(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra", "gpt-6.1-sol", "gpt-6.2-sol-high"} {
		levels := supportedReasoningEffortsForModel(model)
		require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, levels, model)
		require.NotContains(t, levels, "none", "%s 不支持 none", model)
		require.NotContains(t, levels, "minimal", "%s 不支持 minimal", model)
		require.NotContains(t, levels, "ultra", "%s 不支持 ultra", model)
	}
	// 非 GPT 命名不参与裁剪。
	require.Nil(t, supportedReasoningEffortsForModel("claude-sonnet-4-6"))
	require.Nil(t, supportedReasoningEffortsForModel(""))
}

func TestNormalizeReasoningEffortClampsToSupported(t *testing.T) {
	cases := []struct {
		model      string
		effort     string
		want       string
		wantChange bool
	}{
		// DSH 选 "默认" 会发 none，必须降级而不是透传。
		{"gpt-6.2-sol", "none", "low", true},
		{"gpt-6.2-sol", "minimal", "low", true},
		{"gpt-6.2-sol", "ultra", "max", true},
		// 已支持的档位原样保留。
		{"gpt-6.2-sol", "low", "low", false},
		{"gpt-6.2-sol", "medium", "medium", false},
		{"gpt-6.2-sol", "high", "high", false},
		{"gpt-6.2-sol", "xhigh", "xhigh", false},
		{"gpt-6.2-sol", "max", "max", false},
	}
	for _, tc := range cases {
		t.Run(tc.model+"_"+tc.effort, func(t *testing.T) {
			got, changed := normalizeOpenAIResponsesReasoningEffortForModel(tc.model, tc.effort)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantChange, changed)
		})
	}
}

func TestModelAcceptsReasoningEffort(t *testing.T) {
	require.False(t, modelAcceptsReasoningEffort("gpt-6.2-sol", "none"),
		"none 不被 GPT-6.2 接受，minimal->none 的兼容转换不能对它生效")
	require.True(t, modelAcceptsReasoningEffort("gpt-6.2-sol", "low"))
	require.True(t, modelAcceptsReasoningEffort("gpt-6.2-sol", "max"))
}
