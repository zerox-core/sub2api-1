package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func modalitiesOf(modelID string) []string {
	return ClassifyModelCapability(modelID).Modalities
}

func TestClassifyModelCapabilityModalities(t *testing.T) {
	cases := []struct {
		model string
		want  []string
	}{
		// 生图
		{"gpt-image-2", []string{ModelModalityImage}},
		{"gpt-image-2.5-flare", []string{ModelModalityImage}},
		{"z-image-turbo", []string{ModelModalityImage}},
		{"qwen-image-2.0", []string{ModelModalityImage}},
		{"doubao-seedream-5-0-260128", []string{ModelModalityImage}},
		{"wan2.7-image", []string{ModelModalityImage}},
		// 视频
		{"seedance-2-0-260128", []string{ModelModalityVideo}},
		{"wan2.1-t2v", []string{ModelModalityVideo}},
		// 语音
		{"mimo-v2.5-tts", []string{ModelModalityAudio}},
		{"speech-2.8-hd", []string{ModelModalityAudio}},
		{"qwen3-tts-flash", []string{ModelModalityAudio}},
		{"whisper-1", []string{ModelModalityAudio}},
		{"livetranslate", []string{ModelModalityAudio}},
		// 语音 + 视觉共存
		{"gpt-4o-realtime-preview", []string{ModelModalityAudio, ModelModalityVision}},
		// 嵌入
		{"text-embedding-3-large", []string{ModelModalityEmbedding}},
		// 文本 / 多模态理解
		{"deepseek-v4-flash", []string{ModelModalityText}},
		{"丽珠::deepseek-v4-flash", []string{ModelModalityText}},
		{"gemini-3.8-flash", []string{ModelModalityText, ModelModalityVision}},
		{"claude-opus-4-6", []string{ModelModalityText, ModelModalityVision}},
		{"Qwen3.5-Omni-Plus", []string{ModelModalityText, ModelModalityVision}},
		{"GLM-5.3-Flash", []string{ModelModalityText}},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, modalitiesOf(tc.model), "model=%s", tc.model)
	}
}

func TestClassifyModelCapabilityThinkingVariant(t *testing.T) {
	high := ClassifyModelCapability("gemini-3.8-flash-high")
	require.Equal(t, "gemini-3.8-flash", high.BaseModel)
	require.Equal(t, "high", high.ThinkingLevel)
	require.True(t, high.Reasoning)

	tiered := ClassifyModelCapability("gemini-3.8-flash-tiered")
	require.Equal(t, "gemini-3.8-flash", tiered.BaseModel)
	require.Equal(t, "tiered", tiered.ThinkingLevel)

	plain := ClassifyModelCapability("gemini-3.8-flash")
	require.Equal(t, "gemini-3.8-flash", plain.BaseModel)
	require.Equal(t, "", plain.ThinkingLevel)
	require.False(t, plain.Reasoning)

	// 带 thinking 字样的原生思考模型
	thinking := ClassifyModelCapability("claude-opus-4-6-thinking")
	require.True(t, thinking.Reasoning)
	require.Equal(t, "claude-opus-4-6-thinking", thinking.BaseModel)

	// o 系列靠短前缀识别
	require.True(t, ClassifyModelCapability("o3-mini").Reasoning)
	require.True(t, ClassifyModelCapability("o1").Reasoning)
	// 不能被名字中间的 o3 误伤
	require.False(t, ClassifyModelCapability("gpt-5.6-terra").Reasoning)
}

func TestClassifyModelCapabilityKeepsOriginalName(t *testing.T) {
	capability := ClassifyModelCapability("  Gemini-3.8-Flash-HIGH  ")
	require.Equal(t, "Gemini-3.8-Flash-HIGH", capability.ModelID)
	require.Equal(t, "Gemini-3.8-Flash", capability.BaseModel)
	require.Equal(t, "high", capability.ThinkingLevel)
	require.Equal(t, ModelCapabilitySourceBuiltin, capability.Source)
}

func TestModelCapabilityFilterByModality(t *testing.T) {
	models := []string{"gemini-3.8-flash", "gpt-image-2", "mimo-v2.5-tts", "speech-2.8-hd", "claude-opus-4-6"}

	textOnly := ModelCapabilityFilter{Modalities: []string{ModelModalityText}}
	require.Equal(t, []string{"gemini-3.8-flash", "claude-opus-4-6"}, textOnly.FilterModelIDs(models))

	imageOnly := ModelCapabilityFilter{Modalities: []string{ModelModalityImage}}
	require.Equal(t, []string{"gpt-image-2"}, imageOnly.FilterModelIDs(models))

	// 多个模态取并集：勾选 text + vision 表示「文本模型和视觉模型都放进来」
	textVision := ModelCapabilityFilter{Modalities: []string{ModelModalityText, ModelModalityVision}}
	require.Equal(t, []string{"gemini-3.8-flash", "claude-opus-4-6"}, textVision.FilterModelIDs(models))

	// 并集语义的可区分用例：取交集会得到空列表
	speechOrImage := ModelCapabilityFilter{Modalities: []string{ModelModalityImage, ModelModalityAudio}}
	require.Equal(t, []string{"gpt-image-2", "mimo-v2.5-tts", "speech-2.8-hd"}, speechOrImage.FilterModelIDs(models))

	// 零值 = 不过滤（存量分组行为不变）
	require.False(t, ModelCapabilityFilter{}.Active())
	require.Equal(t, models, ModelCapabilityFilter{}.FilterModelIDs(models))
}

func TestModelCapabilityFilterExcludesThinkingVariants(t *testing.T) {
	models := []string{"gemini-3.8-flash", "gemini-3.8-flash-low", "gemini-3.8-flash-high", "gemini-3.8-flash-tiered"}
	filter := ModelCapabilityFilter{Modalities: []string{ModelModalityText}, ExcludeThinkingVariants: true}
	require.Equal(t, []string{"gemini-3.8-flash"}, filter.FilterModelIDs(models))

	// 不排除时全部保留
	filter.ExcludeThinkingVariants = false
	require.Len(t, filter.FilterModelIDs(models), 4)
}

func TestModelCapabilityCatalogPrefersRegistryMetadata(t *testing.T) {
	reasoning := true
	metadata := map[string]UpstreamModelMetadata{
		"gpt-image-2": {ID: "gpt-image-2", InputModalities: []string{"text", "image"}, Reasoning: &reasoning},
	}
	catalog := ModelCapabilityCatalog([]string{"gpt-image-2", "deepseek-v4-flash"}, metadata)

	fromRegistry := catalog["gpt-image-2"]
	require.Equal(t, ModelCapabilitySourceRegistry, fromRegistry.Source)
	require.True(t, fromRegistry.Reasoning)

	// 没有元数据的模型保持内置判断
	fromBuiltin := catalog["deepseek-v4-flash"]
	require.Equal(t, ModelCapabilitySourceBuiltin, fromBuiltin.Source)
	require.Equal(t, []string{ModelModalityText}, fromBuiltin.Modalities)
}
