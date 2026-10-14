package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// antigravityCountTokensMaxBody 上游 countTokens 响应体读取上限（防异常响应撑爆内存）
const antigravityCountTokensMaxBody = 1 << 20

// ForwardCountTokens 将 Claude 协议 count_tokens 请求映射为 Antigravity
// /v1internal:countTokens 真实请求（移植自 CPA antigravity_executor_tokens.go）。
//
// 上游语义（与 CPA 一致）：
//   - 信封仅含 {"request": {...}}，不含 project/model/requestType/requestId
//   - 删除 request.safetySettings / toolConfig / labels / sessionId
//   - 响应取 totalTokens
//
// 失败时不阻断业务：返回 404 让客户端 fallback 到本地估算（与旧行为兼容），
// 但 429 透传（调用方需要感知限流）。
func (s *AntigravityGatewayService) ForwardCountTokens(ctx context.Context, c *gin.Context, account *Account, parsedModel string, body []byte) error {
	startTime := time.Now()

	// upstream 型账号（走第三方 base_url）不支持 v1internal:countTokens
	if account.Type == AccountTypeUpstream {
		c.JSON(http.StatusNotFound, gin.H{
			"type":  "error",
			"error": gin.H{"type": "not_found_error", "message": "count_tokens endpoint is not supported for this platform"},
		})
		return nil
	}

	mappedModel := s.getMappedModelForThinkingLevel(account, parsedModel, "")
	if mappedModel == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"type":  "error",
			"error": gin.H{"type": "permission_error", "message": fmt.Sprintf("model %s not in whitelist", parsedModel)},
		})
		return nil
	}

	if s.tokenProvider == nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "Antigravity token provider not configured"},
		})
		return fmt.Errorf("antigravity token provider not configured")
	}
	accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "authentication_error", "message": "Failed to get upstream access token"},
		})
		return fmt.Errorf("get access token: %w", err)
	}

	// 解析 Claude 请求为内部 Gemini 格式（复用 messages 路径的转换器；
	// countTokens 不需要身份补丁与 MCP 清洗）
	var claudeReq antigravity.ClaudeRequest
	if err := json.Unmarshal(body, &claudeReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": "Invalid request body"},
		})
		return fmt.Errorf("parse claude request: %w", err)
	}
	innerBody, err := antigravity.TransformClaudeToGeminiWithOptions(&claudeReq, "", mappedModel, antigravity.TransformOptions{})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": "Invalid request"},
		})
		return fmt.Errorf("transform claude request: %w", err)
	}

	// 提取信封中的 request 字段并按 CPA 语义删除不需要的字段
	var envelope struct {
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(innerBody, &envelope); err != nil || len(envelope.Request) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": "Invalid transformed request"},
		})
		return fmt.Errorf("extract request envelope: %w", err)
	}
	inner := gjson.ParseBytes(envelope.Request)
	countReq := map[string]any{"request": map[string]any{
		"contents":          inner.Get("contents").Value(),
		"systemInstruction": inner.Get("systemInstruction").Value(),
		"tools":             inner.Get("tools").Value(),
		"generationConfig":  inner.Get("generationConfig").Value(),
	}}
	// 仅在有值时携带，避免 null 字段
	for k, v := range countReq["request"].(map[string]any) {
		if v == nil {
			delete(countReq["request"].(map[string]any), k)
		}
	}
	wireBody, err := json.Marshal(countReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "Failed to build count_tokens request"},
		})
		return fmt.Errorf("marshal count_tokens request: %w", err)
	}

	// 代理 URL
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	baseURL := resolveAntigravityForwardBaseURL(account)
	if baseURL == "" {
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "No antigravity base URL configured"},
		})
		return fmt.Errorf("no antigravity forward base url")
	}

	upstreamReq, err := antigravity.NewAPIRequestWithURL(ctx, baseURL, "countTokens", accessToken, wireBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "Failed to build upstream request"},
		})
		return fmt.Errorf("build upstream request: %w", err)
	}

	// 数据面出站走 TLS 指纹（与主链路一致）
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.resolveAntigravityTLSProfile(account))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "upstream_error", "message": "Upstream request failed"},
		})
		return fmt.Errorf("upstream countTokens request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody := make([]byte, 0, 512)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		respBody = append(respBody, buf[:n]...)
		if readErr != nil {
			break
		}
		if len(respBody) > antigravityCountTokensMaxBody {
			break
		}
	}

	if resp.StatusCode >= 400 {
		// 429 透传限流语义；其余错误降级为 404 让客户端 fallback 本地估算
		if resp.StatusCode == http.StatusTooManyRequests {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"type":  "error",
				"error": gin.H{"type": "rate_limit_error", "message": "Upstream rate limited"},
			})
			return nil
		}
		logger.LegacyPrintf("service.antigravity_gateway", "countTokens upstream %d: %s", resp.StatusCode, truncateString(string(respBody), 512))
		c.JSON(http.StatusNotFound, gin.H{
			"type":  "error",
			"error": gin.H{"type": "not_found_error", "message": "count_tokens endpoint is not supported for this platform"},
		})
		return nil
	}

	totalTokens := gjson.GetBytes(respBody, "totalTokens").Int()
	c.JSON(http.StatusOK, gin.H{"input_tokens": totalTokens})
	logger.LegacyPrintf("service.antigravity_gateway", "countTokens ok model=%s account=%d totalTokens=%d elapsed=%s",
		mappedModel, account.ID, totalTokens, time.Since(startTime).Truncate(time.Millisecond))
	return nil
}

// antigravityCountTokensFromGemini 将 Gemini 协议 countTokens 请求映射为真实上游请求。
// body 为 Gemini 格式（含 contents/systemInstruction 等），信封语义与 Claude 路径一致。
func (s *AntigravityGatewayService) antigravityCountTokensFromGemini(ctx context.Context, c *gin.Context, account *Account, originalModel string, body []byte) error {
	// upstream 型账号不支持 v1internal:countTokens
	if account.Type == AccountTypeUpstream {
		c.JSON(http.StatusOK, map[string]any{"totalTokens": 0})
		return nil
	}

	mappedModel := s.getMappedModelForThinkingLevel(account, originalModel, "")
	if mappedModel == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"code":    http.StatusForbidden,
			"message": fmt.Sprintf("model %s not in whitelist", originalModel),
			"status":  "PERMISSION_DENIED",
		}})
		return nil
	}

	if s.tokenProvider == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"code":    http.StatusBadGateway,
			"message": "Antigravity token provider not configured",
			"status":  "UNAVAILABLE",
		}})
		return fmt.Errorf("antigravity token provider not configured")
	}
	accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"code":    http.StatusBadGateway,
			"message": "Failed to get upstream access token",
			"status":  "UNAVAILABLE",
		}})
		return fmt.Errorf("get access token: %w", err)
	}

	// 按 CPA 语义裁剪：去除 model/project/safetySettings/toolConfig/labels/sessionId
	inner := gjson.ParseBytes(body)
	countReq := map[string]any{"request": map[string]any{
		"contents":          inner.Get("contents").Value(),
		"systemInstruction": inner.Get("systemInstruction").Value(),
		"tools":             inner.Get("tools").Value(),
		"generationConfig":  inner.Get("generationConfig").Value(),
	}}
	for k, v := range countReq["request"].(map[string]any) {
		if v == nil {
			delete(countReq["request"].(map[string]any), k)
		}
	}
	wireBody, err := json.Marshal(countReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"code":    http.StatusInternalServerError,
			"message": "Failed to build count_tokens request",
			"status":  "INTERNAL",
		}})
		return fmt.Errorf("marshal count_tokens request: %w", err)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	baseURL := resolveAntigravityForwardBaseURL(account)
	if baseURL == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"code":    http.StatusBadGateway,
			"message": "No antigravity base URL configured",
			"status":  "UNAVAILABLE",
		}})
		return fmt.Errorf("no antigravity forward base url")
	}

	upstreamReq, err := antigravity.NewAPIRequestWithURL(ctx, baseURL, "countTokens", accessToken, wireBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"code":    http.StatusInternalServerError,
			"message": "Failed to build upstream request",
			"status":  "INTERNAL",
		}})
		return fmt.Errorf("build upstream request: %w", err)
	}

	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.resolveAntigravityTLSProfile(account))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"code":    http.StatusBadGateway,
			"message": "Upstream request failed",
			"status":  "UNAVAILABLE",
		}})
		return fmt.Errorf("upstream countTokens request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody := make([]byte, 0, 512)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		respBody = append(respBody, buf[:n]...)
		if readErr != nil {
			break
		}
		if len(respBody) > antigravityCountTokensMaxBody {
			break
		}
	}

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "Upstream rate limited",
				"status":  "RESOURCE_EXHAUSTED",
			}})
			return nil
		}
		logger.LegacyPrintf("service.antigravity_gateway", "gemini countTokens upstream %d: %s", resp.StatusCode, truncateString(string(respBody), 512))
		c.JSON(http.StatusOK, map[string]any{"totalTokens": 0})
		return nil
	}

	totalTokens := gjson.GetBytes(respBody, "totalTokens").Int()
	c.JSON(http.StatusOK, gin.H{"totalTokens": totalTokens})
	return nil
}
