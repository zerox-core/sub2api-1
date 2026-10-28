package handler

import "testing"

func TestRequiresResponsesForAnthropicToolRequest(t *testing.T) {
	toolBody := []byte(`{"model":"gpt-6.1-sol","tools":[{"name":"read"}]}`)
	plainBody := []byte(`{"model":"gpt-6.1-sol","tools":[]}`)

	if !requiresResponsesForAnthropicToolRequest("gpt-6.1-sol", toolBody, false) {
		t.Fatal("gpt-6.1-sol tool turns must require Responses")
	}
	if requiresResponsesForAnthropicToolRequest("gpt-6.1-sol", plainBody, false) {
		t.Fatal("plain gpt-6.1-sol turns must keep the default capability")
	}
	if !requiresResponsesForAnthropicToolRequest("gpt-6-sol", toolBody, true) {
		t.Fatal("reasoning gpt-6-sol tool turns must require Responses")
	}
	if requiresResponsesForAnthropicToolRequest("gpt-6-sol", toolBody, false) {
		t.Fatal("non-reasoning gpt-6-sol tool turns may use Chat Completions")
	}
}
