package service

// relay_audio.go — ZeroXCore 自建补丁（additive，不改动任何既有文件逻辑）。
//
// 把自建 audio-proxy（MiniMax 配音 + Suno 生歌）挂进 OpenAI 网关，
// 统一走 sub2api 的 Key / 余额 / 计费链路：
//   - 配音 POST /v1/audio/speech：按字符计费。AudioUsage{Mode:"tts"} 复用
//     分组 AudioTTSPricePerMillionChars（须配置为 455 = hd 标准价）；
//     turbo 按 260/455 比例折算计量单位，保证两个模型都精确计价。
//   - 生歌 POST /v1/music + GET /v1/music/:request_id：按次计费。
//     提交不计费；轮询观察到完成时由 handler 一次性 Claim 并记
//     AudioUsage{Mode:"music", Units:1}，经模型 "suno-music" 的
//     per-request 分层价（tier label "music"）计价；失败不收费。
//
// 上游账号 = openai 平台 API Key 账号，credentials.base_url 指向 audio-proxy
// （如 http://host.docker.internal:8790），api_key 为 audio-proxy 的
// TRUSTED_TOKEN（trusted 模式下 proxy 自身不再重复计费）。

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// RelayAudioMusicModel 是生歌在 sub2 内的计费模型名（管理员按 per-request 定价）。
const RelayAudioMusicModel = "suno-music"

// relayAudioTTSDefaultModel 与 audio-proxy 默认模型保持一致。
const relayAudioTTSDefaultModel = "speech-2.8-hd"

// relayAudioTTSStandardPricePerMChars 是分组 AudioTTSPricePerMillionChars 的标准价
// （= hd 上游成本 3.5 元/万字符 × 1.3 加价）。其他模型按实际代理价对该标准价
// 的比例折算计量单位，使单一分组价即可精确计费。
const relayAudioTTSStandardPricePerMChars = 455.0

// relayAudioTTSPriceRatio 返回模型代理价相对标准价的比例（与 audio-proxy 同口径）。
func relayAudioTTSPriceRatio(model string) float64 {
	switch strings.TrimSpace(model) {
	case "speech-2.8-turbo":
		return 260.0 / relayAudioTTSStandardPricePerMChars
	default:
		return 1.0
	}
}

// RelayAudioTTSModelFromBody 提取配音请求模型（缺省与 proxy 默认一致）。
func RelayAudioTTSModelFromBody(body []byte) string {
	if gjson.ValidBytes(body) {
		if m := strings.TrimSpace(gjson.GetBytes(body, "model").String()); m != "" {
			return m
		}
	}
	return relayAudioTTSDefaultModel
}

// relayAudioEstimateChars 与 audio-proxy 的 estimate_chars 同规则：汉字计 2 字符。
func RelayAudioEstimateChars(text string) int {
	n := 0
	for _, r := range text {
		if r > 0x2E7F {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// RelayAudioTTSTextFromBody 提取配音请求文本。
func RelayAudioTTSTextFromBody(body []byte) string {
	if gjson.ValidBytes(body) {
		return gjson.GetBytes(body, "text").String()
	}
	return ""
}

// RelayAudioTTSEstimateUnits 估算配音请求的计费单位（百万字符，按模型价格比折算），
// 供 handler 做在途预留，与实际计费同口径。
func RelayAudioTTSEstimateUnits(body []byte) float64 {
	chars := RelayAudioEstimateChars(RelayAudioTTSTextFromBody(body))
	if chars <= 0 {
		return 0
	}
	return float64(chars) / 1e6 * relayAudioTTSPriceRatio(RelayAudioTTSModelFromBody(body))
}

// doRelayAudioRequest 统一转发入口：构建上游 URL、携带 trusted 凭据、透传响应。
// 返回上游响应体与响应头（响应已同步透传给下游客户端）。
func (s *OpenAIGatewayService) doRelayAudioRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	method, path string,
	body []byte,
) ([]byte, http.Header, error) {
	if s == nil || account == nil {
		return nil, nil, fmt.Errorf("relay audio service/account is required")
	}
	base := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/")
	if base == "" {
		return nil, nil, fmt.Errorf("relay audio account %d requires credentials.base_url", account.ID)
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, nil, err
	}
	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()
	req, err := http.NewRequestWithContext(upstreamCtx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, audio/*")
	req.Header.Set("Content-Type", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	started := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		result, herr := s.handleGrokMediaErrorResponse(ctx, resp, c, account, resp.Header.Get("x-request-id"), "audio-relay")
		_ = result
		return nil, nil, herr
	}
	data, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, nil, err
	}
	writeGrokMediaResponse(c, resp, data, s.responseHeaderFilter)
	return data, resp.Header, nil
}

// ForwardRelayAudioSpeech 转发配音请求；计费单位来自上游 X-Chars（缺失时按请求文本估算），
// 并按模型价格比折算到标准价单位。
func (s *OpenAIGatewayService) ForwardRelayAudioSpeech(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	started := time.Now()
	data, hdr, err := s.doRelayAudioRequest(ctx, c, account, http.MethodPost, "/v1/audio/speech", body)
	if err != nil {
		return nil, err
	}
	_ = data
	model := RelayAudioTTSModelFromBody(body)
	chars := 0
	if v := strings.TrimSpace(hdr.Get("X-Chars")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil {
			chars = n
		}
	}
	if chars <= 0 {
		chars = RelayAudioEstimateChars(RelayAudioTTSTextFromBody(body))
	}
	var audioUsage *AudioUsage
	if chars > 0 {
		units := float64(chars) / 1e6 * relayAudioTTSPriceRatio(model)
		audioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: units}
	}
	return &OpenAIForwardResult{
		RequestID:        StableGrokAudioBillingRequestID(hdr.Get("x-request-id")),
		UpstreamHeaders:  hdr,
		Model:            model,
		UpstreamModel:    model,
		UpstreamEndpoint: "/v1/audio/speech",
		Duration:         time.Since(started),
		AudioUsage:       audioUsage,
	}, nil
}

// ForwardRelayMusicCreate 转发生歌提交；提交本身不计费（完成轮询时一次性计费）。
// 返回的 ResponseID 为上游 request_id，供 handler 登记 pending billing。
func (s *OpenAIGatewayService) ForwardRelayMusicCreate(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	started := time.Now()
	data, hdr, err := s.doRelayAudioRequest(ctx, c, account, http.MethodPost, "/v1/music", body)
	if err != nil {
		return nil, err
	}
	requestID := ""
	if gjson.ValidBytes(data) {
		requestID = strings.TrimSpace(gjson.GetBytes(data, "request_id").String())
	}
	return &OpenAIForwardResult{
		RequestID:        StableGrokAudioBillingRequestID(""),
		ResponseID:       requestID,
		UpstreamHeaders:  hdr,
		Model:            RelayAudioMusicModel,
		UpstreamModel:    RelayAudioMusicModel,
		UpstreamEndpoint: "/v1/music",
		Duration:         time.Since(started),
	}, nil
}

// ForwardRelayMusicStatus 转发生歌状态查询。仅当上游已确认完成（非失败）时
// 才挂 AudioUsage{Mode:"music", Units:1}；是否真正计费由 handler 的
// ClaimGrokVideoBilling 一次性闸门决定（失败/进行中/重复轮询均不计费）。
func (s *OpenAIGatewayService) ForwardRelayMusicStatus(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	requestID string,
) (*OpenAIForwardResult, error) {
	started := time.Now()
	path := "/v1/music/" + url.PathEscape(strings.TrimSpace(requestID))
	data, hdr, err := s.doRelayAudioRequest(ctx, c, account, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var audioUsage *AudioUsage
	if relayAudioMusicCompleted(data) {
		audioUsage = &AudioUsage{Mode: "music", DurationOrUnits: 1}
	}
	return &OpenAIForwardResult{
		RequestID:        StableGrokAudioBillingRequestID("music:" + strings.TrimSpace(requestID)),
		ResponseID:       strings.TrimSpace(requestID),
		UpstreamHeaders:  hdr,
		Model:            RelayAudioMusicModel,
		UpstreamModel:    RelayAudioMusicModel,
		UpstreamEndpoint: "/v1/music/{request_id}",
		Duration:         time.Since(started),
		AudioUsage:       audioUsage,
	}, nil
}

// relayAudioMusicCompleted 判定生歌任务是否完成（可计费）。
// 失败一律不可计费；状态值命中成功集合、或任一 task 已带音频产物即视为完成。
func relayAudioMusicCompleted(data []byte) bool {
	if !gjson.ValidBytes(data) {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(gjson.GetBytes(data, "status").String()))
	failed := status == "failed" || status == "error" || status == "cancelled"
	taskHasAudio := false
	for _, t := range gjson.GetBytes(data, "tasks").Array() {
		ts := strings.ToLower(strings.TrimSpace(t.Get("status").String()))
		if ts == "failed" || ts == "error" || ts == "cancelled" {
			failed = true
		}
		for _, key := range []string{"audio_url", "url", "audio", "source_url"} {
			if strings.TrimSpace(t.Get(key).String()) != "" {
				taskHasAudio = true
			}
		}
	}
	if failed {
		return false
	}
	switch status {
	case "complete", "completed", "success", "succeeded", "finished", "done":
		return true
	}
	return taskHasAudio
}
