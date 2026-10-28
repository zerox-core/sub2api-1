package handler

import "testing"

func TestRequiresResponsesForGPT61ToolRequest(t *testing.T) {
	toolsBody := []byte(`{"model":"gpt-6.1-sol","tools":[{"type":"function","name":"shell"}]}`)
	emptyToolsBody := []byte(`{"model":"gpt-6.1-sol","tools":[]}`)
	noToolsBody := []byte(`{"model":"gpt-6.1-sol","messages":[]}`)
	terraToolsBody := []byte(`{"model":"gpt-6.1-terra","tools":[{"type":"function","name":"shell"}]}`)
	terraNoToolsBody := []byte(`{"model":"gpt-6.1-terra","messages":[]}`)

	cases := []struct {
		name  string
		model string
		body  []byte
		want  bool
	}{
		{"gpt61sol with tools", "gpt-6.1-sol", toolsBody, true},
		{"gpt61sol empty tools", "gpt-6.1-sol", emptyToolsBody, false},
		{"gpt61sol without tools", "gpt-6.1-sol", noToolsBody, false},
		{"gpt61terra with tools", "gpt-6.1-terra", terraToolsBody, true},
		{"gpt61terra without tools", "gpt-6.1-terra", terraNoToolsBody, false},
		{"gpt61terra prefixed with tools", "openai/gpt-6.1-terra-max", terraToolsBody, true},
		{"other model with tools", "gpt-5.6", toolsBody, false},
		{"empty body", "gpt-6.1-sol", nil, false},
		{"invalid body", "gpt-6.1-sol", []byte("not-json"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiresResponsesForGPT61ToolRequest(tc.model, tc.body); got != tc.want {
				t.Fatalf("requiresResponsesForGPT61ToolRequest(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestBodyDeclaresTools(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want bool
	}{
		{"one tool", []byte(`{"tools":[{"type":"function"}]}`), true},
		{"empty array", []byte(`{"tools":[]}`), false},
		{"object not array", []byte(`{"tools":{"a":1}}`), false},
		{"no key", []byte(`{"messages":[]}`), false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyDeclaresTools(tc.body); got != tc.want {
				t.Fatalf("bodyDeclaresTools() = %v, want %v", got, tc.want)
			}
		})
	}
}
