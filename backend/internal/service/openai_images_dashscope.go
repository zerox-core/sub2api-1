package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// 百炼（DashScope）原生适配层。
//
// 百炼的 OpenAI 兼容模式**不提供** /v1/images/generations 与 /v1/audio/speech
// （实测两者均返回 404）；其生图/语音模型只能通过原生接口调用：
//
//	POST {site}/api/v1/services/aigc/multimodal-generation/generation
//
// 该接口同步返回 OSS URL（图片或音频），而不是内联 b64。
// 所以当账号 base_url 指向 dashscope.aliyuncs.com 时，网关在这里做协议转换：
// 客户端仍按 OpenAI 协议说话，出站改说百炼原生协议。

const dashScopeNativeMultimodalGenerationPath = "/api/v1/services/aigc/multimodal-generation/generation"

// dashScopeImageModelPrefixes 是百炼原生生图模型的命名族。
// 这些名字在兼容模式下没有可用端点，只能走原生 multimodal-generation。
var dashScopeImageModelPrefixes = []string{
	"qwen-image",
	"wan2.",
	"wanx",
	"wan-image",
	"z-image",
}

// dashScopeNativeBaseURL 判断账号是否为百炼账号，并返回其原生站点根地址。
// 例如 base_url=https://dashscope.aliyuncs.com/compatible-mode/v1
// 归一化为 https://dashscope.aliyuncs.com。
func dashScopeNativeBaseURL(account *Account) (string, bool) {
	if account == nil {
		return "", false
	}
	base := strings.TrimSpace(account.GetOpenAIBaseURL())
	if base == "" {
		return "", false
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return "", false
	}
	if !isDashScopeUpstreamHost(parsed.Hostname()) {
		return "", false
	}
	return parsed.Scheme + "://" + parsed.Host, true
}

func isDashScopeUpstreamHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "dashscope.aliyuncs.com", "dashscope-intl.aliyuncs.com":
		return true
	}
	return strings.HasSuffix(host, ".dashscope.aliyuncs.com") ||
		strings.HasSuffix(host, ".dashscope-intl.aliyuncs.com")
}

func isDashScopeNativeImageModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	for _, prefix := range dashScopeImageModelPrefixes {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}

// forwardOpenAIImagesDashScopeNative 通过百炼原生 multimodal-generation 完成生图，
// 并把原生响应改写成 OpenAI images 响应体，再复用既有收尾逻辑（写回 + usage/图片数提取）。
func (s *OpenAIGatewayService) forwardOpenAIImagesDashScopeNative(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	parsed *OpenAIImagesRequest,
	requestModel string,
	nativeBase string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	upstreamModel, mapped := account.ResolveMappedModel(requestModel)
	if !mapped {
		upstreamModel = requestModel
	}
	// 命中账号级映射时信任管理员配置的上游模型名；否则只放行百炼生图模型族。
	if !mapped && !isDashScopeNativeImageModel(requestModel) && !isDashScopeNativeImageModel(upstreamModel) {
		return nil, fmt.Errorf("images endpoint requires an image model, got %q", requestModel)
	}
	SetOpsUpstreamModel(c, upstreamModel)
	logger.LegacyPrintf(
		"service.openai_gateway",
		"[OpenAI] Images request routing request_model=%s upstream_model=%s endpoint=%s adapter=dashscope-native account_type=%s",
		strings.TrimSpace(parsed.Model),
		upstreamModel,
		parsed.Endpoint,
		account.Type,
	)

	payload, err := buildDashScopeNativeImagesPayload(parsed, upstreamModel)
	if err != nil {
		return nil, err
	}
	// 百炼原生 multimodal-generation 是同步接口，没有 SSE 形态。若客户端点名要流式，
	// 直接拒绝，避免它把 JSON 当 SSE 解析而得到空结果。
	if parsed.Stream {
		return nil, fmt.Errorf("streaming image generation is not supported for model %q", requestModel)
	}

	// 与 OpenAI 兼容路径一致：生图耗时且上游已计费，客户端断开不应取消上游调用。
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()

	upstreamReq, err := s.buildDashScopeNativeRequest(upstreamCtx, account, nativeBase, payload)
	if err != nil {
		return nil, err
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.doOpenAIUpstream(upstreamReq, proxyURL, account)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID:            opsUpstreamProxyID(account),
			ProxyName:          opsUpstreamProxyName(account),
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Kind:               "request_error",
			Message:            safeErr,
		})
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	nativeBody, readErr := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if readErr != nil {
		return nil, readErr
	}

	requestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	// openAIBody 在下面两个分支里都会被赋值，这里只声明不初始化，
	// 避免留下一个必然被覆盖的死赋值（ineffassign）。
	var openAIBody []byte
	if resp.StatusCode >= 400 {
		openAIBody = dashScopeNativeErrorBodyToOpenAI(nativeBody)
	} else if converted, convErr := dashScopeNativeImageBodyToOpenAI(nativeBody); convErr != nil {
		return nil, convErr
	} else {
		openAIBody = converted
	}

	synthetic := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(openAIBody)),
	}
	if requestID != "" {
		synthetic.Header.Set("x-request-id", requestID)
	}

	if resp.StatusCode >= 400 {
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(openAIBody)))
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID:            opsUpstreamProxyID(account),
			ProxyName:          opsUpstreamProxyName(account),
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestID,
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Kind:               "failover",
			Message:            upstreamMsg,
		})
		if s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, openAIBody) {
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           openAIBody,
				RetryableOnSameAccount: false,
			}
		}
		return s.handleOpenAIImagesErrorResponse(upstreamCtx, synthetic, c, account, upstreamModel)
	}

	usage, imageCount, sizes, err := s.handleOpenAIImagesNonStreamingResponse(upstreamCtx, synthetic, c, account, parsed)
	if err != nil {
		return nil, err
	}
	if imageCount <= 0 && parsed.N > 0 {
		imageCount = parsed.N
	}
	return &OpenAIForwardResult{
		RequestID:        requestID,
		UpstreamHeaders:  synthetic.Header,
		Usage:            usage,
		Model:            requestModel,
		UpstreamModel:    upstreamModel,
		Stream:           parsed.Stream,
		ResponseHeaders:  synthetic.Header.Clone(),
		Duration:         time.Since(startTime),
		ImageCount:       imageCount,
		ImageSize:        parsed.SizeTier,
		ImageInputSize:   parsed.Size,
		ImageOutputSizes: sizes,
	}, nil
}

func (s *OpenAIGatewayService) buildDashScopeNativeRequest(
	ctx context.Context,
	account *Account,
	nativeBase string,
	payload []byte,
) (*http.Request, error) {
	validated, err := s.validateUpstreamBaseURL(nativeBase)
	if err != nil {
		return nil, err
	}
	targetURL := strings.TrimRight(validated, "/") + dashScopeNativeMultimodalGenerationPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, fmt.Errorf("build dashscope authentication headers: %w", err)
	}
	for key, values := range authHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		req.Header.Set("User-Agent", customUA)
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

// buildDashScopeNativeImagesPayload 把 OpenAI images 请求翻译成百炼原生 multimodal 请求。
// 文生图 -> content=[{text}]; 图生图/编辑 -> content=[{image}..., {text}]。
func buildDashScopeNativeImagesPayload(parsed *OpenAIImagesRequest, upstreamModel string) ([]byte, error) {
	content := make([]map[string]any, 0, len(parsed.InputImageURLs)+len(parsed.Uploads)+1)
	for _, raw := range parsed.InputImageURLs {
		if v := strings.TrimSpace(raw); v != "" {
			content = append(content, map[string]any{"image": v})
		}
	}
	for _, upload := range parsed.Uploads {
		dataURL, err := openAIImagesUploadDataURL(upload)
		if err != nil {
			return nil, err
		}
		content = append(content, map[string]any{"image": dataURL})
	}
	if prompt := strings.TrimSpace(parsed.Prompt); prompt != "" {
		content = append(content, map[string]any{"text": prompt})
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("images endpoint requires a prompt or an input image")
	}

	payload := map[string]any{
		"model": upstreamModel,
		"input": map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": content},
			},
		},
	}
	parameters := map[string]any{}
	if size := dashScopeNativeImageSize(parsed.Size); size != "" {
		parameters["size"] = size
	}
	if parsed.N > 1 {
		parameters["n"] = parsed.N
	}
	if len(parameters) > 0 {
		payload["parameters"] = parameters
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode dashscope images payload: %w", err)
	}
	return body, nil
}

// dashScopeNativeImageSize 把 OpenAI 的 "1024x1024" 写成百炼要求的 "1024*1024"。
func dashScopeNativeImageSize(size string) string {
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "" {
		return ""
	}
	return strings.ReplaceAll(size, "x", "*")
}

func openAIImagesUploadDataURL(upload OpenAIImagesUpload) (string, error) {
	if len(upload.Data) == 0 {
		return "", fmt.Errorf("images upload %q is empty", upload.FieldName)
	}
	contentType := strings.TrimSpace(upload.ContentType)
	if contentType == "" {
		contentType = "image/png"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(upload.Data), nil
}

// dashScopeNativeImageBodyToOpenAI 把原生响应改写成 OpenAI images 响应体。
// 原生：{"output":{"choices":[{"message":{"content":[{"image":"https://..."}]}}]}}
// 目标：{"created":<ts>,"data":[{"url":"https://..."}]}
func dashScopeNativeImageBodyToOpenAI(body []byte) ([]byte, error) {
	data := make([]map[string]any, 0, 1)
	for _, choice := range gjson.GetBytes(body, "output.choices").Array() {
		for _, item := range choice.Get("message.content").Array() {
			imageURL := strings.TrimSpace(item.Get("image").String())
			if imageURL == "" {
				continue
			}
			entry := map[string]any{"url": imageURL}
			if size := dashScopeNativeImageSizeLabel(body); size != "" {
				entry["size"] = size
			}
			data = append(data, entry)
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("dashscope multimodal response contained no image")
	}
	out, err := json.Marshal(map[string]any{
		"created": time.Now().Unix(),
		"data":    data,
	})
	if err != nil {
		return nil, fmt.Errorf("encode openai images response: %w", err)
	}
	return out, nil
}

func dashScopeNativeImageSizeLabel(body []byte) string {
	width := gjson.GetBytes(body, "usage.width").Int()
	height := gjson.GetBytes(body, "usage.height").Int()
	if width <= 0 || height <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", width, height)
}

// dashScopeNativeErrorBodyToOpenAI 把百炼错误体包装成 OpenAI 错误体，
// 让既有错误处理/降级逻辑（识别 error.message）继续工作。
func dashScopeNativeErrorBodyToOpenAI(body []byte) []byte {
	msg := strings.TrimSpace(gjson.GetBytes(body, "message").String())
	if msg == "" {
		msg = strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	}
	if msg == "" {
		msg = strings.TrimSpace(extractUpstreamErrorMessage(body))
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	errObj := map[string]any{
		"message": msg,
		"type":    "upstream_error",
	}
	if code := strings.TrimSpace(gjson.GetBytes(body, "code").String()); code != "" {
		errObj["code"] = code
	}
	out, err := json.Marshal(map[string]any{"error": errObj})
	if err != nil {
		return body
	}
	return out
}
