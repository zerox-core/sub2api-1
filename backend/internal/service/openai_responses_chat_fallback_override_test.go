package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForcesResponsesChatFallbackReadsExtraFlag(t *testing.T) {
	require.False(t, (&Account{Type: AccountTypeAPIKey}).ForcesResponsesChatFallback())

	on := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIResponsesChatFallbackExtraKey: true}}
	require.True(t, on.ForcesResponsesChatFallback())

	off := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIResponsesChatFallbackExtraKey: false}}
	require.False(t, off.ForcesResponsesChatFallback())

	// 类型不正确时按关闭处理，避免脏配置把流量整体改道。
	wrongType := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIResponsesChatFallbackExtraKey: "true"}}
	require.False(t, wrongType.ForcesResponsesChatFallback())
}

func TestShouldForwardResponsesViaRawChatCompletionsHonorsExplicitOverride(t *testing.T) {
	var nilAccount *Account
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(nilAccount))

	// 显式开关对 API Key 账号生效。
	on := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIResponsesChatFallbackExtraKey: true}}
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(on))

	// OAuth 账号不走这条降级（其上游链路不同，改道会破坏 codex 语义）。
	oauth := &Account{Type: AccountTypeOAuth, Extra: map[string]any{OpenAIResponsesChatFallbackExtraKey: true}}
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(oauth))
}

func TestResolveUpstreamProtocolHonorsResponsesChatFallbackOverride(t *testing.T) {
	account := &Account{
		Type:     AccountTypeAPIKey,
		Platform: PlatformOpenAI,
		Extra:    map[string]any{OpenAIResponsesChatFallbackExtraKey: true},
	}
	require.Equal(t, APIProtocolChatCompletions,
		resolveUpstreamProtocol(account, APIProtocolResponses, "deepseek-v4.1-flash", nil))
}
