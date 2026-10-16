package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// R6：分组级「按模型功能过滤」。
// 最重要的不变量是零值无操作 —— 存量分组没有这两个字段，行为必须完全不变。
func TestGroupModelAllowlistCapabilityFilterNoopWhenUnset(t *testing.T) {
	disabled := GroupModelAllowlist{}
	require.True(t, disabled.Allows("gpt-image-2"))
	require.True(t, disabled.Allows("mimo-v2.5-tts"))
	require.Equal(t, []string{"a", "b"}, disabled.FilterForListing([]string{"a", "b"}))

	// 只开白名单、不带功能过滤时行为照旧
	plain := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-image-2"}}
	require.Equal(t, []string{"gpt-image-2"}, plain.FilterForListing([]string{"gpt-image-2", "other"}))

	// 只配排除思考变体时，非变体不受任何影响
	onlyVariants := GroupModelAllowlist{Enabled: true, Models: []string{"*"}, ExcludeThinkingVariants: true}
	require.True(t, onlyVariants.Allows("claude-opus-4-6"))
}

func TestGroupModelAllowlistRejectsModalitiesOutsideFilter(t *testing.T) {
	textVision := GroupModelAllowlist{
		Enabled:    true,
		Models:     []string{"*"},
		Modalities: []string{ModelModalityText, ModelModalityVision},
	}
	require.True(t, textVision.Allows("gemini-3.8-flash"))
	// 即使白名单是 *，图像 / 语音模型也被功能过滤挡掉
	require.False(t, textVision.Allows("gpt-image-2"))
	require.False(t, textVision.Allows("mimo-v2.5-tts"))

	// 列表与准入必须一致：列表里不能出现一个请求必然失败的模型
	require.Equal(t,
		[]string{"gemini-3.8-flash"},
		textVision.FilterForListing([]string{"gemini-3.8-flash", "gpt-image-2", "mimo-v2.5-tts"}),
	)
}

// 功能过滤独立于白名单开关：分组没开白名单，"只要图像模型"的约束同样生效。
func TestGroupModelAllowlistCapabilityFilterAppliesWithoutAllowlistEnabled(t *testing.T) {
	imageOnly := GroupModelAllowlist{Modalities: []string{ModelModalityImage}}

	require.True(t, imageOnly.Allows("gpt-image-2"))
	require.False(t, imageOnly.Allows("gemini-3.8-flash"))
	require.Equal(t,
		[]string{"gpt-image-2"},
		imageOnly.FilterForListing([]string{"gemini-3.8-flash", "gpt-image-2"}),
	)
}

func TestGroupModelAllowlistExcludeThinkingVariants(t *testing.T) {
	cfg := GroupModelAllowlist{
		Enabled:                 true,
		Models:                  []string{"gemini-*"},
		ExcludeThinkingVariants: true,
	}
	require.True(t, cfg.Allows("gemini-3.8-flash"))
	require.False(t, cfg.Allows("gemini-3.8-flash-high"))

	require.Equal(t,
		[]string{"gemini-3.8-flash"},
		cfg.FilterForListing([]string{"gemini-3.8-flash", "gemini-3.8-flash-low", "gemini-3.8-flash-medium"}),
	)
}

func TestNormalizeGroupModelAllowlistCapabilityFilter(t *testing.T) {
	normalized, err := normalizeGroupModelAllowlist(GroupModelAllowlist{
		Modalities:              []string{" Text ", "text", "AUDIO"},
		ExcludeThinkingVariants: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{ModelModalityText, ModelModalityAudio}, normalized.Modalities)
	require.True(t, normalized.ExcludeThinkingVariants)
	// 白名单未开启时也能单独保存功能过滤
	require.False(t, normalized.Enabled)

	// 未知模态必须 400，而不是写进库后静默不生效
	_, err = normalizeGroupModelAllowlist(GroupModelAllowlist{Modalities: []string{"hologram"}})
	require.Error(t, err)

	// 空白模态被丢弃后视为未配置
	empty, err := normalizeGroupModelAllowlist(GroupModelAllowlist{Modalities: []string{"  "}})
	require.NoError(t, err)
	require.Nil(t, empty.Modalities)
}

// domain 往返必须带上能力字段，否则 ent 持久化会静默丢配置。
func TestGroupModelAllowlistDomainRoundTripKeepsCapabilityFilter(t *testing.T) {
	original := GroupModelAllowlist{
		Enabled:                 true,
		Models:                  []string{"gemini-*"},
		Modalities:              []string{ModelModalityText},
		ExcludeThinkingVariants: true,
	}
	require.Equal(t, original, GroupModelAllowlistFromDomain(DomainGroupModelAllowlist(original)))
}
