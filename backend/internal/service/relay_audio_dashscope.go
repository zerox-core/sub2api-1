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

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
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

func (s *OpenAIGatewayService) forwardRelayAudioSpeechDashScopeNative(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	nativeBase string,
	started time.Time,
) (*OpenAIForwardResult, error) {
	model := RelayAudioTTSModelFromBody(body)
	text := RelayAudioTTSTextFromBody(body)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("audio speech request requires text")
	}
	upstreamModel, mapped := account.ResolveMappedModel(model)
	if !mapped || strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = model
	}
	voice := dashScopeTTSVoice(gjson.GetBytes(body, "voice").String())

	payload, err := json.Marshal(map[string]any{
		"model": upstreamModel,
		"input": map[string]any{
			"text":  text,
			"voice": voice,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode dashscope tts payload: %w", err)
	}

	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()

	req, err := s.buildDashScopeNativeRequest(upstreamCtx, account, nativeBase, payload)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	requestID := strings.TrimSpace(resp.Header.Get("x-request-id"))

	if resp.StatusCode >= 400 {
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
		return nil, fmt.Errorf("dashscope tts response contained no audio url")
	}
	contentType, audioBytes, err := s.downloadDashScopeAudio(upstreamCtx, c, account, audioURL)
	if err != nil {
		return nil, err
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
