package service

import (
	"sort"
	"strings"
)

// 模型能力目录（R6）：把「模型名 → 模态 / 是否思考 / 是否支持图片输入」收敛到一处判断。
//
// 为什么需要它：分组选模型 UI 与账号层过滤此前都只能靠模型名正则各猜一套，
// 于是"189 个模型"里混着生图、语音、实时类，只能一个个手工剔除
// （分组 14 曾因此被配进 35 个非文本模型）。
//
// 两个消费方：
//   - 分组层（R6）：按模态筛选候选模型、对白名单做"一键按模态过滤"；
//   - 账号层（R5）：与上游实测结果（明确 404/403 的模型）合并成真实支持性判断。
//
// 判断优先级：上游元数据（models.dev，见 UpstreamModelMetadata.InputModalities）>
// 内置规则（本文件）。人工覆盖由调用方合并。
const (
	ModelModalityText      = "text"
	ModelModalityVision    = "vision"
	ModelModalityImage     = "image"
	ModelModalityAudio     = "audio"
	ModelModalityVideo     = "video"
	ModelModalityEmbedding = "embedding"

	ModelCapabilitySourceBuiltin  = "builtin"
	ModelCapabilitySourceRegistry = "registry"
	ModelCapabilitySourceManual   = "manual"
)

// ModelModalities 是全部受支持的模态标识，供管理端校验分组级模态白名单。
var ModelModalities = []string{
	ModelModalityText,
	ModelModalityVision,
	ModelModalityImage,
	ModelModalityAudio,
	ModelModalityVideo,
	ModelModalityEmbedding,
}

// ModelCapability 描述单个模型的可用能力。
//
// BaseModel/ThinkingLevel 把「靠换 id 表达挡位」的写法还原出来：
// 例如 gemini-3.8-flash-high -> base=gemini-3.8-flash, level=high。
// 上游把不同思考挡位做成不同 id（Antigravity 就是如此），
// UI 与路由都需要知道它们是同一个模型的挡位变体，而不是不同模型。
type ModelCapability struct {
	ModelID       string   `json:"model_id"`
	BaseModel     string   `json:"base_model"`
	Modalities    []string `json:"modalities"`
	Reasoning     bool     `json:"reasoning"`
	Vision        bool     `json:"vision"`
	ThinkingLevel string   `json:"thinking_level,omitempty"`
	Source        string   `json:"source"`
}

// 模态规则按优先级排列：先判 embedding/video/image-generation/audio，
// 都不命中才落到文本（可能再叠加 vision）。
var (
	modelEmbeddingMarkers = []string{"embedding", "embed-", "-embed", "bge-", "text-embed"}
	modelVideoMarkers     = []string{"sora", "veo", "kling", "seedance", "-t2v", "-i2v", "text-to-video", "video-", "-video"}
	modelImageGenMarkers  = []string{
		"gpt-image", "dall-e", "dalle", "seedream", "z-image", "-image", "image-",
		"wanx", "wan-image", "qwen-image", "flux", "imagen", "stable-diffusion", "midjourney", "mj-",
	}
	modelAudioMarkers = []string{
		"-tts", "tts-", "speech-", "-speech", "audio", "whisper", "-asr", "asr-",
		"realtime", "-s2s", "s2s-", "livetranslate", "voice-", "-voice",
	}
	modelVisionMarkers = []string{
		"gpt-4o", "gpt-4.1", "gpt-4-turbo", "gpt-5", "gpt-6", "claude-", "gemini-",
		"-vl", "vl-", "vision", "glm-4v", "glm-5v", "-omni", "omni-", "llava", "internvl",
		"step-1v", "yi-vision", "qwen-vl", "qwen2-vl", "qwen3-vl",
	}
	modelReasoningMarkers = []string{
		"thinking", "reasoner", "deepseek-r", "-r1", "qwq", "-reasoning", "reasoning-", "-xhigh",
	}
	// 思考挡位后缀：上游用后缀 id 表达挡位（-tiered 是能力/价格档，也算一种变体）。
	modelThinkingLevelSuffixes = []string{"-minimal", "-low", "-medium", "-high", "-xhigh", "-tiered"}
)

func modelNameContainsAny(modelID string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(modelID, marker) {
			return true
		}
	}
	return false
}

// splitModelThinkingLevelSuffix 拆掉思考挡位后缀，返回基名与挡位。
func splitModelThinkingLevelSuffix(modelID string) (string, string) {
	for _, suffix := range modelThinkingLevelSuffixes {
		if strings.HasSuffix(modelID, suffix) {
			return strings.TrimSuffix(modelID, suffix), strings.TrimPrefix(suffix, "-")
		}
	}
	return modelID, ""
}

// normalizeModelCapabilityLookupName 去掉渠道前缀（如 "丽珠::deepseek-v4-flash"）
// 与首尾空白，只用于能力判定，不改写对外展示的模型名。
func normalizeModelCapabilityLookupName(modelID string) string {
	name := strings.TrimSpace(modelID)
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// ClassifyModelCapability 用内置规则判断模型能力（无网络、无数据库依赖，可离线复现）。
func ClassifyModelCapability(modelID string) ModelCapability {
	original := strings.TrimSpace(modelID)
	capability := ModelCapability{
		ModelID:   original,
		BaseModel: original,
		Source:    ModelCapabilitySourceBuiltin,
		Modalities: []string{
			ModelModalityText,
		},
	}
	lookup := normalizeModelCapabilityLookupName(original)
	if lookup == "" {
		return capability
	}
	if _, level := splitModelThinkingLevelSuffix(lookup); level != "" {
		capability.ThinkingLevel = level
		capability.Reasoning = true
		// 用原始大小写还原基名：后缀是按小写匹配的，不能用 TrimSuffix 直接裁，
		// 这里按长度裁掉 "-<level>"（原串大小写可能与挡位名不同）。
		if len(original) > len(level)+1 {
			capability.BaseModel = original[:len(original)-len(level)-1]
		}
	}

	modalities := make(map[string]bool, 3)
	switch {
	case modelNameContainsAny(lookup, modelEmbeddingMarkers):
		modalities[ModelModalityEmbedding] = true
	case modelNameContainsAny(lookup, modelVideoMarkers):
		modalities[ModelModalityVideo] = true
	case modelNameContainsAny(lookup, modelImageGenMarkers):
		modalities[ModelModalityImage] = true
	case modelNameContainsAny(lookup, modelAudioMarkers):
		modalities[ModelModalityAudio] = true
		// gpt-4o-audio-preview 这类既是语音又是视觉模型。
		if modelNameContainsAny(lookup, modelVisionMarkers) {
			modalities[ModelModalityVision] = true
			capability.Vision = true
		}
	default:
		modalities[ModelModalityText] = true
		if modelNameContainsAny(lookup, modelVisionMarkers) {
			modalities[ModelModalityVision] = true
			capability.Vision = true
		}
	}
	// o1 / o3 / o4 这类极短前缀单独判定，避免 "-o1" 之类误伤。
	if !capability.Reasoning {
		if hasReasoningPrefix(lookup) || modelNameContainsAny(lookup, modelReasoningMarkers) {
			capability.Reasoning = true
		}
	}

	keys := make([]string, 0, len(modalities))
	for modality := range modalities {
		keys = append(keys, modality)
	}
	sort.Strings(keys)
	capability.Modalities = keys
	return capability
}

func hasReasoningPrefix(modelID string) bool {
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if modelID == prefix || strings.HasPrefix(modelID, prefix+"-") {
			return true
		}
	}
	return false
}

// ClassifyModelCapabilities 批量分类，保持输入顺序。
func ClassifyModelCapabilities(modelIDs []string) []ModelCapability {
	result := make([]ModelCapability, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		result = append(result, ClassifyModelCapability(modelID))
	}
	return result
}

// ModelCapabilityCatalog 汇总一批模型的能力，并合并上游元数据。
//
// metadata 来自 accounts.extra.upstream_model_metadata（models.dev 同步结果），
// 它的 InputModalities 比内置正则更可信，因此优先采用；
// 但只有当元数据里确实带了模态时才覆盖，避免把内置判断降级成"未知"。
func ModelCapabilityCatalog(modelIDs []string, metadata map[string]UpstreamModelMetadata) map[string]ModelCapability {
	catalog := make(map[string]ModelCapability, len(modelIDs))
	for _, modelID := range modelIDs {
		capability := ClassifyModelCapability(modelID)
		if entry, ok := metadata[strings.TrimSpace(modelID)]; ok {
			if modalities := normalizeInputModalities(entry.InputModalities); len(modalities) > 0 {
				capability.Modalities = modalities
				capability.Vision = containsString(modalities, ModelModalityVision)
				capability.Source = ModelCapabilitySourceRegistry
			}
			if entry.Reasoning != nil {
				capability.Reasoning = *entry.Reasoning
				if capability.Source == ModelCapabilitySourceBuiltin && entry.Reasoning != nil {
					capability.Source = ModelCapabilitySourceRegistry
				}
			}
		}
		catalog[strings.TrimSpace(modelID)] = capability
	}
	return catalog
}

func normalizeInputModalities(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case ModelModalityText, ModelModalityVision, ModelModalityImage, ModelModalityAudio, ModelModalityVideo, ModelModalityEmbedding:
		case "image_input", "images":
			normalized = ModelModalityVision
		default:
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ModelCapabilityFilter 是分组层"按模态过滤白名单"的判据。
// Modalities 为空表示不按模态过滤；ExcludeThinkingVariants 用来把
// "-low/-medium/-high/-tiered" 这类挡位变体从白名单里折叠掉。
type ModelCapabilityFilter struct {
	Modalities              []string `json:"modalities"`
	ExcludeThinkingVariants bool     `json:"exclude_thinking_variants"`
}

// Active 报告是否配置了过滤条件。零值 = 不过滤，保证存量分组行为完全不变。
func (f ModelCapabilityFilter) Active() bool {
	return len(f.Modalities) > 0 || f.ExcludeThinkingVariants
}

// Allows 判断某个模型是否通过过滤（用于分组白名单的按功能过滤与分组级准入）。
//
// 多个模态取**并集**（任一命中即通过）：管理端勾选 text + vision 的意图是
// 「文本模型和视觉模型都放进来」，取交集会把纯文本模型排除掉，也与选模型面板上
// 模态 chips 的筛选语义不一致。ExcludeThinkingVariants 单独生效，两者是 AND。
func (f ModelCapabilityFilter) Allows(modelID string) bool {
	capability := ClassifyModelCapability(modelID)
	if len(f.Modalities) > 0 {
		matched := false
		for _, modality := range f.Modalities {
			if containsString(capability.Modalities, strings.ToLower(strings.TrimSpace(modality))) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if f.ExcludeThinkingVariants && capability.ThinkingLevel != "" {
		return false
	}
	return true
}

// FilterModelIDs 按过滤条件保留模型，保持输入顺序。
func (f ModelCapabilityFilter) FilterModelIDs(modelIDs []string) []string {
	result := make([]string, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if f.Allows(modelID) {
			result = append(result, modelID)
		}
	}
	return result
}
