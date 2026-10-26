package antigravity

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var prefixTestRequestID = regexp.MustCompile(`"requestId":"[^"]*"`)

func prefixTestMask(out []byte) []byte {
	return prefixTestRequestID.ReplaceAll(out, []byte(`"requestId":"X"`))
}

func prefixTestUserContent(ts, question string) json.RawMessage {
	raw, _ := json.Marshal([]map[string]any{
		{
			"type": "text",
			"text": "<system-reminder>\n# currentDate\nToday's date is " + ts + "\n</system-reminder>\n" + question,
		},
	})
	return raw
}

func prefixTestConversation(historyTS, lastTS string) *ClaudeRequest {
	assistantRaw, _ := json.Marshal([]map[string]any{{"type": "text", "text": "first answer"}})
	return &ClaudeRequest{
		Model:     "gemini-3.8-flash",
		MaxTokens: 1024,
		Messages: []ClaudeMessage{
			{Role: "user", Content: prefixTestUserContent(historyTS, "first question")},
			{Role: "assistant", Content: assistantRaw},
			{Role: "user", Content: prefixTestUserContent(lastTS, "second question")},
		},
	}
}

// 历史消息里的时间戳不同，归一化后转换出的上游请求必须一致（前缀稳定）。
func TestNormalizeHistoricalRemindersStabilizesTransformedPrefix(t *testing.T) {
	reqA := prefixTestConversation("2026-10-25T05:18:09Z", "2026-10-25T05:18:22Z")
	reqB := prefixTestConversation("2026-10-25T05:19:01Z", "2026-10-25T05:18:22Z")

	if !NormalizeHistoricalReminders(reqA) {
		t.Fatal("expected normalization to change historical reminder")
	}
	if !NormalizeHistoricalReminders(reqB) {
		t.Fatal("expected normalization to change historical reminder")
	}

	outA, err := TransformClaudeToGeminiWithOptions(reqA, "project-1", "gemini-3.8-flash-high", DefaultTransformOptions())
	if err != nil {
		t.Fatalf("transform A: %v", err)
	}
	outB, err := TransformClaudeToGeminiWithOptions(reqB, "project-1", "gemini-3.8-flash-high", DefaultTransformOptions())
	if err != nil {
		t.Fatalf("transform B: %v", err)
	}
	if !bytes.Equal(prefixTestMask(outA), prefixTestMask(outB)) {
		t.Fatal("transformed bodies differ after normalization; prefix is not stable")
	}
}

// 当前轮（最后一条消息）必须保持原样，模型仍能看到本轮日期时间。
func TestNormalizeHistoricalRemindersKeepsLastMessage(t *testing.T) {
	req := prefixTestConversation("2026-10-25T05:18:09Z", "2026-10-25T05:18:22Z")
	NormalizeHistoricalReminders(req)

	var last []map[string]any
	if err := json.Unmarshal(req.Messages[len(req.Messages)-1].Content, &last); err != nil {
		t.Fatalf("unmarshal last: %v", err)
	}
	text, _ := last[0]["text"].(string)
	if !strings.Contains(text, "2026-10-25T05:18:22Z") {
		t.Fatalf("last message was rewritten: %q", text)
	}

	var history []map[string]any
	if err := json.Unmarshal(req.Messages[0].Content, &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	histText, _ := history[0]["text"].(string)
	if strings.Contains(histText, "2026-10-25T05:18:09Z") {
		t.Fatalf("history timestamp not normalized: %q", histText)
	}
	if !strings.Contains(histText, "<datetime>") {
		t.Fatalf("expected <datetime> placeholder, got %q", histText)
	}
}

// 没有 <system-reminder> 的消息必须逐字节保持原样，且返回值应为 false。
func TestNormalizeHistoricalRemindersLeavesPlainMessagesUntouched(t *testing.T) {
	userRaw, _ := json.Marshal([]map[string]any{{"type": "text", "text": "plain question 2026-10-25"}})
	assistantRaw, _ := json.Marshal([]map[string]any{{"type": "text", "text": "plain answer"}})
	lastRaw, _ := json.Marshal([]map[string]any{{"type": "text", "text": "last"}})
	req := &ClaudeRequest{
		Model:     "gemini-3.8-flash",
		MaxTokens: 1024,
		Messages: []ClaudeMessage{
			{Role: "user", Content: userRaw},
			{Role: "assistant", Content: assistantRaw},
			{Role: "user", Content: lastRaw},
		},
	}
	before := append(json.RawMessage(nil), req.Messages[0].Content...)
	if NormalizeHistoricalReminders(req) {
		t.Fatal("expected no change for plain messages")
	}
	if !bytes.Equal(before, req.Messages[0].Content) {
		t.Fatal("plain message content was modified")
	}
}

// 撇号变体与斜杠日期也要归一化到 ASCII 形式。
func TestNormalizeReminderSpanCanonicalizesApostropheAndDate(t *testing.T) {
	span := "<system-reminder>\nToday’s date is 2026/10/25.\n</system-reminder>"
	got := normalizeReminderSpan(span)
	if !strings.Contains(got, "Today's date is") {
		t.Fatalf("apostrophe not normalized: %q", got)
	}
	if strings.Contains(got, "2026/10/25") || strings.Contains(got, "2026-10-25") {
		t.Fatalf("date not replaced with placeholder: %q", got)
	}
	if !strings.Contains(got, "<date>") {
		t.Fatalf("expected <date> placeholder, got %q", got)
	}
}

// 字符串形式的 content 也要能归一化。
func TestNormalizeHistoricalRemindersStringContent(t *testing.T) {
	historyRaw, _ := json.Marshal("<system-reminder>\nToday's date is 2026-10-25T05:18:09Z\n</system-reminder>\nq1")
	lastRaw, _ := json.Marshal("<system-reminder>\nToday's date is 2026-10-25T05:18:22Z\n</system-reminder>\nq2")
	req := &ClaudeRequest{
		Model:     "gemini-3.8-flash",
		MaxTokens: 1024,
		Messages: []ClaudeMessage{
			{Role: "user", Content: historyRaw},
			{Role: "user", Content: lastRaw},
		},
	}
	if !NormalizeHistoricalReminders(req) {
		t.Fatal("expected string-content history to be normalized")
	}
	var got string
	if err := json.Unmarshal(req.Messages[0].Content, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(got, "2026-10-25T05:18:09Z") || !strings.Contains(got, "<datetime>") {
		t.Fatalf("unexpected normalized content: %q", got)
	}
}
