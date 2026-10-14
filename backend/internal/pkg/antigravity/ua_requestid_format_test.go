package antigravity

import (
	"encoding/json"
	"regexp"
	"testing"
)

// TestBuildUserAgentOfficialFormat 锁定官方 Windows 客户端抓包的 UA 格式
// （2026-10-06 mitmproxy 捕获，与生产 tlsproxy AG_UA_* 规则一致）。
func TestBuildUserAgentOfficialFormat(t *testing.T) {
	got := BuildUserAgent("2.19.1")
	want := "antigravity/hub/2.19.1 (aidev_client; os_type=windows; arch=amd64; cl=991101027)"
	if got != want {
		t.Fatalf("BuildUserAgent = %q, want %q", got, want)
	}

	// 非法版本回退默认值（DefaultUserAgentVersion = 2.19.1）
	got = BuildUserAgent("")
	if got != "antigravity/hub/2.19.1 (aidev_client; os_type=windows; arch=amd64; cl=991101027)" {
		t.Fatalf("BuildUserAgent fallback = %q", got)
	}
}

// TestBuildOnboardUserUserAgentLongFormat 控制面 UA = 短格式 + google-api-nodejs-client
// （与 CPA AntigravityOnboardUserUserAgent 语义一致）。
func TestBuildOnboardUserUserAgentLongFormat(t *testing.T) {
	got := BuildOnboardUserUserAgent("2.19.1")
	want := "antigravity/hub/2.19.1 (aidev_client; os_type=windows; arch=amd64; cl=991101027) google-api-nodejs-client/10.3.0"
	if got != want {
		t.Fatalf("BuildOnboardUserUserAgent = %q, want %q", got, want)
	}
}

// TestTransformClaudeToGeminiRequestIDSlashFormat 锁定 requestId 斜杠格式：
// agent/{conv-uuid}/{epoch-ms}/{uuid4}/1
func TestTransformClaudeToGeminiRequestIDSlashFormat(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model:     "claude-fable-5-1",
		MaxTokens: 1024,
		Messages: []ClaudeMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	body, err := TransformClaudeToGemini(claudeReq, "proj-x", "gemini-3-pro-high")
	if err != nil {
		t.Fatalf("TransformClaudeToGemini: %v", err)
	}

	var v1 V1InternalRequest
	if err := json.Unmarshal(body, &v1); err != nil {
		t.Fatalf("unmarshal v1internal: %v", err)
	}

	pattern := regexp.MustCompile(`^agent/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/\d+/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/1$`)
	if !pattern.MatchString(v1.RequestID) {
		t.Fatalf("requestId = %q, want slash format agent/{uuid}/{ms}/{uuid}/1", v1.RequestID)
	}
}
