package domain

// GroupModelAllowlist 是分组级模型白名单：开启后既过滤模型列表类接口的返回内容，
// 也约束分组实际可调用的模型（网关准入在合成路由改写与调度之前完成）。
//
// R6 起额外承载分组级「按模型功能过滤」：Modalities / ExcludeThinkingVariants 与
// Models 是 AND 叠加的独立维度。零值 = 不过滤，存量分组行为不变。
type GroupModelAllowlist struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
	// Modalities 是允许的模态集合（空 = 不限制），多个取并集。
	Modalities []string `json:"modalities,omitempty"`
	// ExcludeThinkingVariants 为真时不放行用换 id 表达挡位的变体。
	ExcludeThinkingVariants bool `json:"exclude_thinking_variants,omitempty"`
}
