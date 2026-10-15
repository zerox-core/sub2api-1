package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// 百炼（DashScope）原生 TTS 适配。
//
// 百炼兼容模式没有 /v1/audio/speech（实测 404），语音合成只能走原生
// POST /api/v1/services/aigc/multimodal-generation/generation，
// 它同步返回音频 URL（OSS），所以网关需要二次下载再回给客户端。
// 计费口径与既有 audio relay 完全一致：字符数 × 模型价格比 -> AudioUsage{Mode:"tts"}。

// dashScopeTTSDefaultVoice 是未指定 voice 时的默认音色。
const dashScopeTTSDefaultVoice = "Cherry"

// dashScopeTTSVoice 把 OpenAI 风格音色名映射到百炼音色；
// 无法识别的值原样透传（允许直接使用百炼音色名，如 Cherry/Ethan）。
func dashScopeTTSVoice(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return dashScopeTTSDefaultVoice
	case "alloy", "shimmer", "nova":
		return "Cherry"
	case "echo", "onyx", "fable":
		return "Ethan"
	}
	return strings.TrimSpace(raw)
}

// dashScopeTTSTextFromBody 提取待合成文本。
// 本网关的音频代理历史上用 `text` 字段（audio-proxy 协议），而 OpenAI 官方
// SDK 用 `input`；两者都接受，`text` 优先。
func dashScopeTTSTextFromBody(body []byte) string {
	if text := strings.TrimSpace(RelayAudioTTSTextFromBody(body)); text != "" {
		return text
	}
	if !gjson.ValidBytes(body) {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, "input").String())
}

// dashScopeNativeClientError 写回 OpenAI 风格错误并返回该错误。
//
// audio relay 的 handler 在 result == nil 时直接返回而不写响应，因此适配器
// 自己产生的错误必须在这里写掉，否则客户端只会看到一个空的 200。
func dashScopeNativeClientError(c *gin.Context, status int, errType, message string) error {
	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.JSON(status, gin.H{"error": gin.H{"message": message, "type": errType}})
	}
	return fmt.Errorf("%s", message)
}

func (s *OpenAIGatewayService) forwardRelayAudioSpeechDashScopeNative(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	nativeBase string,
	started time.Time,
) (*OpenAIForwardResult, error) {
	model := RelayAudioTTSModelFromBody(body)
	text := dashScopeTTSTextFromBody(body)
	if text == "" {
		return nil, dashScopeNativeClientError(c, http.StatusBadRequest, "invalid_request_error",
			"audio speech request requires a non-empty text or input field")
	}
	upstreamModel, mapped := account.ResolveMappedModel(model)
	if !mapped || strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = model
	}
	voice := dashScopeTTSVoice(gjson.GetBytes(body, "voice").String())

	logger.LegacyPrintf(
		"service.openai_gateway",
		"[OpenAI] Audio speech routing request_model=%s upstream_model=%s endpoint=%s adapter=dashscope-native voice=%s chars=%d",
		model,
		upstreamModel,
		dashScopeNativeMultimodalGenerationPath,
		voice,
		RelayAudioEstimateChars(text),
	)

	payload, err := json.Marshal(map[string]any{
		"model": upstreamModel,
		"input": map[string]any{
			"text":  text,
			"voice": voice,
		},
	})
	if err != nil {
		return nil, dashScopeNativeClientError(c, http.StatusInternalServerError, "api_error",
			"failed to encode speech request")
	}

	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()

	req, err := s.buildDashScopeNativeRequest(upstreamCtx, account, nativeBase, payload)
	if err != nil {
		logger.L().Error("relay_audio.dashscope_request_build_failed",
			zap.Int64("account_id", account.ID), zap.Error(err))
		return nil, dashScopeNativeClientError(c, http.StatusBadGateway, "api_error",
			"failed to build upstream speech request")
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.doOpenAIUpstream(req, proxyURL, account)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	nativeBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, dashScopeNativeClientError(c, http.StatusBadGateway, "api_error",
			"failed to read upstream speech response")
	}
	requestID := strings.TrimSpace(resp.Header.Get("x-request-id"))

	if resp.StatusCode >= 400 {
		logger.L().Warn("relay_audio.dashscope_upstream_error",
			zap.Int64("account_id", account.ID),
			zap.Int("status_code", resp.StatusCode),
			zap.String("upstream_model", upstreamModel))
		openAIBody := dashScopeNativeErrorBodyToOpenAI(nativeBody)
		synthetic := &http.Response{
			StatusCode: resp.StatusCode,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(openAIBody)),
		}
		if requestID != "" {
			synthetic.Header.Set("x-request-id", requestID)
		}
		return s.handleGrokMediaErrorResponse(upstreamCtx, synthetic, c, account, "x-request-id", upstreamModel)
	}

	audioURL := strings.TrimSpace(gjson.GetBytes(nativeBody, "output.audio.url").String())
	if audioURL == "" {
		logger.L().Error("relay_audio.dashscope_no_audio_url",
			zap.Int64("account_id", account.ID),
			zap.String("upstream_model", upstreamModel))
		return nil, dashScopeNativeClientError(c, http.StatusBadGateway, "api_error",
			"upstream speech response contained no audio url")
	}
	contentType, audioBytes, err := s.downloadDashScopeAudio(upstreamCtx, c, account, audioURL)
	if err != nil {
		logger.L().Error("relay_audio.dashscope_audio_download_failed",
			zap.Int64("account_id", account.ID), zap.Error(err))
		return nil, dashScopeNativeClientError(c, http.StatusBadGateway, "api_error",
			"failed to fetch synthesized audio")
	}
	c.Data(http.StatusOK, contentType, audioBytes)

	chars := RelayAudioEstimateChars(text)
	var audioUsage *AudioUsage
	if chars > 0 {
		units := float64(chars) / 1e6 * relayAudioTTSPriceRatio(model)
		audioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: units}
	}
	SetOpsUpstreamModel(c, upstreamModel)
	return &OpenAIForwardResult{
		RequestID:        StableGrokAudioBillingRequestID(requestID),
		UpstreamHeaders:  resp.Header,
		Model:            model,
		UpstreamModel:    upstreamModel,
		UpstreamEndpoint: dashScopeNativeMultimodalGenerationPath,
		Duration:         time.Since(started),
		AudioUsage:       audioUsage,
	}, nil
}

// downloadDashScopeAudio 下载百炼返回的音频 URL（公有 OSS，非账号内网地址）。
func (s *OpenAIGatewayService) downloadDashScopeAudio(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	audioURL string,
) (string, []byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(audioURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", nil, fmt.Errorf("invalid dashscope audio url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.doOpenAIUpstream(req, proxyURL, account)
	if err != nil {
		return "", nil, fmt.Errorf("download dashscope audio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("download dashscope audio: upstream status %d", resp.StatusCode)
	}
	data, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return "", nil, err
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" || strings.Contains(contentType, "octet-stream") {
		contentType = dashScopeAudioContentType(parsed.Path)
	}
	return contentType, data, nil
}

func dashScopeAudioContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".opus":
		return "audio/opus"
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".pcm":
		return "audio/L16"
	default:
		return "audio/mpeg"
	}
}
