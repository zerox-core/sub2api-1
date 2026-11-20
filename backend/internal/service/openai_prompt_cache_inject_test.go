package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSupportsPromptCacheOptions 锁定「哪些模型接受 Responses 显式缓存控制」。
//
// 上游实测补充（重要）：即使是 gpt-6.x，Plus 订阅的 ChatGPT 后端也会对
// prompt_cache_breakpoint 返回 "not supported on this model"。所以这个函数只
// 用于决定「转发时是否保留客户端自带的 prompt_cache_options」，不做主动注入。
func TestSupportsPromptCacheOptions(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		// GPT-5.6 起支持显式断点。
		{"gpt-5.6-sol", true},
		{"gpt-5.6-luna", true},
		{"gpt-5.6-terra", true},
		{"gpt-5.6", true},
		{"gpt-5.7", true},
		// 6 系列。
		{"gpt-6", true},
		{"gpt-6-sol", true},
		{"gpt-6.1-sol", true},
		{"gpt-6.2-sol", true},
		{"gpt-6.2-astra", true},
		// 5.6 之前不支持。
		{"gpt-5.5", false},
		{"gpt-5.4", false},
		{"gpt-5.4-mini", false},
		{"gpt-5.2", false},
		{"gpt-5.2-pro", false},
		{"gpt-5", false},
		{"gpt-5-sol", false},
		// 更早的模型。
		{"gpt-4.1", false},
		// 非 GPT 命名不走这条判定。
		{"claude-sonnet-4-6", false},
		{"", false},
		// 大小写与空白要容错。
		{"  GPT-6.2-SOL  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			require.Equal(t, tc.want, SupportsPromptCacheOptions(tc.model), tc.model)
		})
	}
}
