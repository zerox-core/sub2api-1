package handler

// openai_tool_capability.go — 按请求内容推导 OpenAI 端点能力要求。
//
// gpt-6.1-sol 的 Chat Completions 上游端点不接受工具调用（tools），带工具的请求
// 只能经 Responses 端点转发。部分账号（如 wocnm 低价号）只支持 Chat Completions，
// 一旦被调度选中，请求会在转发层被拒绝并返回
// "gpt-6.1-sol requires Responses for tool calls; this account only supports
// Chat Completions"，而不是 failover 到真正支持 Responses 的账号。
//
// 这里把「带工具的 gpt-6.1-sol 请求需要 Responses 能力」做成选号前的硬性条件，
// 与 Claude Messages 链路已有的 requiresResponsesForAnthropicToolRequest 对齐。

import (
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// bodyDeclaresTools 报告请求体顶层是否声明了非空 tools 数组。
// 使用 gjson 惰性扫描，避免为大请求体做一次完整反序列化。
func bodyDeclaresTools(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	tools := gjson.GetBytes(body, "tools")
	return tools.IsArray() && len(tools.Array()) > 0
}

// requiresResponsesForGPT61ToolRequest 报告该请求是否必须由支持 Responses 的账号承接。
// 覆盖 gpt-6.1-sol 与 gpt-6.1-terra：两者的 Chat Completions 上游端点都不接受工具
// 调用，带工具的请求只能经 Responses 转发。其他模型在 Chat Completions 上支持工具
// 调用，保持原有路由。
func requiresResponsesForGPT61ToolRequest(model string, body []byte) bool {
	if !openai.IsGPT61ModelSpelling(model) {
		return false
	}
	return bodyDeclaresTools(body)
}
