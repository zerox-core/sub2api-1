package handler

// relay_audio.go — ZeroXCore 自建补丁（additive）。
// OpenAI 平台分组专用：把请求转发到账号 base_url 指向的 audio-proxy，
// 配音按字符、生歌按次（完成轮询一次性计费、失败不收费）统一走 sub2 计费。
// 结构镜像 grok_audio.go / grok_media.go，复用既有计费与异步记账设施，
// 不改动任何既有文件逻辑。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// relayAudioPlatformGate 放行 openai / anthropic 平台分组（claude 分组复用音频账号时走 anthropic）。
func (h *OpenAIGatewayHandler) relayAudioPlatformGate(c *gin.Context) (*service.APIKey, bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	platform := ""
	if ok && apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	if !ok || (platform != service.PlatformOpenAI && platform != service.PlatformAnthropic) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Audio API is not supported for this platform")
		return nil, false
	}
	return apiKey, true
}

// relayAudioCheckBilling 统一的依赖检查 + 计费准入。
func (h *OpenAIGatewayHandler) relayAudioCheckBilling(c *gin.Context, apiKey *service.APIKey) (*service.UserSubscription, bool) {
	if !h.ensureResponsesDependencies(c, nil) {
		return nil, false
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return nil, false
	}
	return subscription, true
}

// relayAudioSelectAndForward 选账号 + 占槽 + 转发（镜像 GrokVoice 的故障切换结构）。
// 成功时返回转发结果与实际使用的账号（记账归因需要账号）。
func (h *OpenAIGatewayHandler) relayAudioSelectAndForward(
	c *gin.Context,
	apiKey *service.APIKey,
	reqLog *zap.Logger,
	forward func(account *service.Account) (*service.OpenAIForwardResult, error),
) (*service.OpenAIForwardResult, *service.Account) {
	failed := map[int64]struct{}{}
	var last *service.UpstreamFailoverError
	for attempts := 0; attempts < 4; attempts++ {
		selection, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			"",
			"",
			failed,
			service.OpenAIUpstreamTransportHTTPSSE,
			"",
			false,
			false,
			false,
			service.PlatformOpenAI,
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if last != nil {
				h.handleFailoverExhausted(c, last, false)
			} else {
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available audio relay accounts")
			}
			return nil, nil
		}
		account := selection.Account
		var started bool
		release, status := h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &started, reqLog)
		if status == openAISlotAcquireProfitVetoed {
			failed[account.ID] = struct{}{}
			continue
		}
		if status != openAISlotAcquireOK {
			if status == openAISlotAcquireFailed && len(failed) == 0 {
				return nil, nil
			}
			failed[account.ID] = struct{}{}
			continue
		}
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer release()
			return forward(account)
		}()
		if forwardErr == nil {
			return result, account
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(forwardErr, &failoverErr) && failoverErr.ShouldRetryNextAccount() {
			failed[account.ID] = struct{}{}
			last = failoverErr
			continue
		}
		// 非 failover 错误：handleGrokMediaErrorResponse / transport 已写响应。
		return nil, nil
	}
	if last != nil {
		h.handleFailoverExhausted(c, last, false)
	}
	return nil, nil
}

// RelayAudioSpeech 处理 POST /v1/audio/speech（配音，按字符计费）。
func (h *OpenAIGatewayHandler) RelayAudioSpeech(c *gin.Context) {
	apiKey, ok := h.relayAudioPlatformGate(c)
	if !ok {
		return
	}
	subscription, ok := h.relayAudioCheckBilling(c, apiKey)
	if !ok {
		return
	}
	body, err := readGrokVoiceGatewayBody(c)
	if err != nil || len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.relay_audio", zap.String("endpoint", "speech"))
	model := service.RelayAudioTTSModelFromBody(body)
	// 余额模式在途预留（与计费同口径估算）。
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{
		Model:      model,
		Kind:       service.InflightEstimateAudio,
		AudioMode:  "tts",
		AudioUnits: service.RelayAudioTTSEstimateUnits(body),
	})
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer inflightDone()

	result, account := h.relayAudioSelectAndForward(c, apiKey, reqLog, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardRelayAudioSpeech(c.Request.Context(), c, account, body)
	})
	if result == nil {
		return
	}
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	h.recordRelayAudioUsage(c, apiKey, subject, account, subscription, "speech", body, result, "")
}

// RelayMusicCreate 处理 POST /v1/music（生歌提交；提交不计费，登记 pending billing）。
func (h *OpenAIGatewayHandler) RelayMusicCreate(c *gin.Context) {
	apiKey, ok := h.relayAudioPlatformGate(c)
	if !ok {
		return
	}
	subscription, ok := h.relayAudioCheckBilling(c, apiKey)
	if !ok {
		return
	}
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	body, err := readGrokVoiceGatewayBody(c)
	if err != nil || len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.relay_audio", zap.String("endpoint", "music_create"))
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{
		Model:      service.RelayAudioMusicModel,
		Kind:       service.InflightEstimateAudio,
		AudioMode:  "music",
		AudioUnits: 1,
	})
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer inflightDone()

	result, _ := h.relayAudioSelectAndForward(c, apiKey, reqLog, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardRelayMusicCreate(c.Request.Context(), c, account, body)
	})
	if result == nil || strings.TrimSpace(result.ResponseID) == "" {
		return
	}
	// 登记 pending billing：完成轮询时据此判定「该任务确由本 Key 经 sub2 提交」并一次性计费。
	pending := service.GrokVideoPendingBilling{
		Model:         service.RelayAudioMusicModel,
		BillingModel:  service.RelayAudioMusicModel,
		OriginalModel: service.RelayAudioMusicModel,
		CreatedAt:     service.GrokVideoPendingCreatedAtNow(),
	}
	if err := h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), result.ResponseID, subject.UserID, apiKey.ID, pending); err != nil {
		reqLog.Warn("relay_audio.store_music_pending_billing_failed_retrying",
			zap.String("request_id", result.ResponseID), zap.Error(err))
		if err2 := h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), result.ResponseID, subject.UserID, apiKey.ID, pending); err2 != nil {
			reqLog.Error("relay_audio.store_music_pending_billing_failed",
				zap.String("request_id", result.ResponseID), zap.Error(err2))
		}
	}
}

// RelayMusicStatus 处理 GET /v1/music/:request_id（状态查询；首次观察到完成时一次性计费）。
func (h *OpenAIGatewayHandler) RelayMusicStatus(c *gin.Context) {
	apiKey, ok := h.relayAudioPlatformGate(c)
	if !ok {
		return
	}
	subscription, ok := h.relayAudioCheckBilling(c, apiKey)
	if !ok {
		return
	}
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	requestID := strings.TrimSpace(c.Param("request_id"))
	if requestID == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request_id is required")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.relay_audio", zap.String("endpoint", "music_status"))
	// 查询不做在途预留：不能因余额预留拦截用户取回已付费结果（与媒体查询同口径）。
	result, account := h.relayAudioSelectAndForward(c, apiKey, reqLog, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardRelayMusicStatus(c.Request.Context(), c, account, requestID)
	})
	if result == nil || result.AudioUsage == nil {
		return
	}
	// 仅当该任务是本 Key 经 sub2 提交（存在 pending）时才计费，避免为直连 proxy 的任务误扣。
	pending, loadErr := h.gatewayService.LoadGrokVideoPendingBilling(c.Request.Context(), requestID, subject.UserID, apiKey.ID)
	if loadErr != nil {
		reqLog.Warn("relay_audio.music_pending_billing_load_failed", zap.String("request_id", requestID), zap.Error(loadErr))
	}
	if pending == nil {
		reqLog.Info("relay_audio.music_billing_skipped_no_pending", zap.String("request_id", requestID))
		return
	}
	claimed, err := h.gatewayService.ClaimGrokVideoBilling(c.Request.Context(), requestID, subject.UserID, apiKey.ID)
	if err != nil {
		reqLog.Warn("relay_audio.music_billing_claim_failed", zap.String("request_id", requestID), zap.Error(err))
		return
	}
	if !claimed {
		reqLog.Debug("relay_audio.music_billing_already_claimed", zap.String("request_id", requestID))
		return
	}
	h.recordRelayAudioUsage(c, apiKey, subject, account, subscription, "music", nil, result, requestID)
}

// recordRelayAudioUsage 记账：配音按字符、生歌按次（AudioUsage 驱动）。
// claimReleaseKey 非空时，记账失败会释放一次性计费闸门以便重试。
func (h *OpenAIGatewayHandler) recordRelayAudioUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	account *service.Account,
	subscription *service.UserSubscription,
	endpoint string,
	body []byte,
	result *service.OpenAIForwardResult,
	claimReleaseKey string,
) {
	if h == nil || c == nil || apiKey == nil || result == nil || result.AudioUsage == nil {
		return
	}
	result.RequestID = service.StableGrokAudioBillingRequestID(result.RequestID)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	if requestPayloadHash == "" {
		requestPayloadHash = service.HashUsageRequestPayload([]byte(endpoint))
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := ""
	if account != nil {
		upstreamEndpoint = GetUpstreamEndpoint(c, account.Platform)
	}
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	model := strings.TrimSpace(result.Model)
	if model == "" {
		model = endpoint
	}
	userID := apiKey.User.ID
	if subject.UserID > 0 {
		userID = subject.UserID
	}

	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: clientRequestedUsageFields(c, service.ChannelMappingResult{}, model, result.UpstreamModel),
		}); err != nil {
			if claimReleaseKey != "" {
				if releaseErr := h.gatewayService.ReleaseGrokVideoBilling(ctx, claimReleaseKey, userID, apiKey.ID); releaseErr != nil {
					logger.L().Warn("relay_audio.billing_claim_release_failed",
						zap.String("request_id", claimReleaseKey), zap.Error(releaseErr))
				}
			}
			logger.L().With(
				zap.String("component", "handler.openai_gateway.relay_audio"),
				zap.Int64("user_id", userID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("endpoint", endpoint),
			).Error("relay_audio.record_usage_failed", zap.Error(err))
		}
	})
}
