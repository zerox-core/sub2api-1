package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// buildGrokCompactionTestBody assembles a Responses-shaped Grok body with enough
// history to cross the compaction trigger. Each turn is a user message, an
// assistant function_call, and the matching function_call_output, so a naive
// tail slice would split a pair and be rejected upstream.
func buildGrokCompactionTestBody(turns int, filler string) []byte {
	items := make([]map[string]any, 0, turns*3+1)
	items = append(items, map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{
			{"type": "input_text", "text": "Original task: keep the parser tests green."},
		},
	})
	for i := 0; i < turns; i++ {
		items = append(items, map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": fmt.Sprintf("step %d: %s", i, filler)},
			},
		})
		callID := fmt.Sprintf("call_%04d", i)
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   callID,
			"name":      "read_file",
			"arguments": fmt.Sprintf(`{"path":"pkg/file_%04d.go"}`, i),
		})
		items = append(items, map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  fmt.Sprintf("result %d: %s", i, filler),
		})
	}
	body := map[string]any{
		"model":        "grok-4.6",
		"instructions": "You are a coding agent.",
		"input":        items,
		"tools": []map[string]any{
			{"type": "function", "name": "read_file", "description": "Read a file from disk."},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestCompactGrokResponsesInput_DisabledLeavesBodyAlone(t *testing.T) {
	t.Parallel()

	body := buildGrokCompactionTestBody(400, strings.Repeat("alpha beta gamma delta ", 8))
	got, result, err := compactGrokResponsesInput(body, grokContextCompactionOptions{
		Enabled: false,
	})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, string(body), string(got))
}

func TestCompactGrokResponsesInput_BelowTriggerLeavesBodyAlone(t *testing.T) {
	t.Parallel()

	body := buildGrokCompactionTestBody(3, "small")
	got, result, err := compactGrokResponsesInput(body, grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, string(body), string(got))
}

func TestCompactGrokResponsesInput_TrimsOversizedHistory(t *testing.T) {
	t.Parallel()

	filler := strings.Repeat("alpha beta gamma delta epsilon ", 48)
	body := buildGrokCompactionTestBody(400, filler)
	before := gjson.GetBytes(body, "input.#").Int()
	require.Greater(t, before, int64(1000))

	compacted, result, err := compactGrokResponsesInput(body, grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.GreaterOrEqual(t, result.OriginalTokens, 200_000)
	require.Less(t, result.CompactedTokens, result.OriginalTokens)
	require.LessOrEqual(t, result.CompactedTokens, 110_000)
	require.Greater(t, result.DroppedItems, 0)

	after := gjson.GetBytes(compacted, "input.#").Int()
	require.Less(t, after, before)
	// The model, instructions and tools must survive untouched.
	require.Equal(t, "grok-4.6", gjson.GetBytes(compacted, "model").String())
	require.Equal(t, gjson.GetBytes(body, "instructions").String(), gjson.GetBytes(compacted, "instructions").String())
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(compacted, "tools").Raw)

	// The opening task statement is kept, and the note explaining the gap is
	// folded into the first surviving user turn.
	firstText := gjson.GetBytes(compacted, "input.0.content.0.text").String()
	require.Contains(t, firstText, "Original task")
	require.Contains(t, firstText, "[gateway context compaction]")
}

// TestCompactGrokResponsesInput_NeverEmitsConsecutiveUserTurns guards the
// placeholder shape: the cut has to land on a user turn, so appending a second
// user placeholder would add a back-to-back user pair on top of whatever the
// client already sent, which some Responses-compatible upstreams reject with a
// 400.
func TestCompactGrokResponsesInput_NeverEmitsConsecutiveUserTurns(t *testing.T) {
	t.Parallel()

	countUserPairs := func(body []byte) int {
		pairs := 0
		previousUser := false
		gjson.GetBytes(body, "input").ForEach(func(_, item gjson.Result) bool {
			isUser := item.Get("type").String() == "message" && item.Get("role").String() == "user"
			if isUser && previousUser {
				pairs++
			}
			previousUser = isUser
			return true
		})
		return pairs
	}

	filler := strings.Repeat("alpha beta gamma delta epsilon ", 48)
	body := buildGrokCompactionTestBody(400, filler)

	compacted, result, err := compactGrokResponsesInput(body, grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.LessOrEqual(t, countUserPairs(compacted), countUserPairs(body),
		"compaction must not introduce new consecutive user turns")
}

func TestCompactGrokResponsesInput_NeverSplitsToolCallPairs(t *testing.T) {
	t.Parallel()

	filler := strings.Repeat("alpha beta gamma delta epsilon ", 48)
	body := buildGrokCompactionTestBody(400, filler)

	compacted, result, err := compactGrokResponsesInput(body, grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)

	calls := map[string]bool{}
	outputs := map[string]bool{}
	gjson.GetBytes(compacted, "input").ForEach(func(_, item gjson.Result) bool {
		switch item.Get("type").String() {
		case "function_call", "custom_tool_call":
			calls[item.Get("call_id").String()] = true
		case "function_call_output", "custom_tool_call_output":
			outputs[item.Get("call_id").String()] = true
		}
		return true
	})
	require.NotEmpty(t, calls)
	for id := range calls {
		require.True(t, outputs[id], "kept a function_call without its output: %s", id)
	}
	for id := range outputs {
		require.True(t, calls[id], "kept a function_call_output without its call: %s", id)
	}
}

func TestCompactGrokResponsesInput_IdempotentOnSecondRun(t *testing.T) {
	t.Parallel()

	filler := strings.Repeat("alpha beta gamma delta epsilon ", 48)
	body := buildGrokCompactionTestBody(400, filler)
	opts := grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	}

	once, result, err := compactGrokResponsesInput(body, opts)
	require.NoError(t, err)
	require.True(t, result.Applied)

	twice, second, err := compactGrokResponsesInput(once, opts)
	require.NoError(t, err)
	// Already under the trigger: the second pass must be a no-op.
	require.Nil(t, second)
	require.Equal(t, string(once), string(twice))
}

func TestGrokContextCompactionOptions_NormalizesBadTarget(t *testing.T) {
	t.Parallel()

	// Target at or above the trigger would trim nothing useful.
	opts := grokContextCompactionOptions{TargetTokens: 200_000}.normalized()
	require.Less(t, opts.TargetTokens, opts.TriggerTokens)
	require.Equal(t, defaultGrokContextCompactionTargetTokens, opts.TargetTokens)

	// Missing values fall back to the shipped defaults.
	opts = grokContextCompactionOptions{}.normalized()
	require.Equal(t, defaultGrokContextCompactionTriggerTokens, opts.TriggerTokens)
	require.Equal(t, defaultGrokContextCompactionTargetTokens, opts.TargetTokens)
}

// TestCompactGrokResponsesInput_KeepsAdditionalToolsCarrier guards the Responses
// Lite tool carrier: it lives inside input, so a naive middle trim would delete
// the only declaration of the client's tools and the model would call nothing.
func TestCompactGrokResponsesInput_KeepsAdditionalToolsCarrier(t *testing.T) {
	t.Parallel()

	filler := strings.Repeat("alpha beta gamma delta epsilon ", 48)
	body := buildGrokCompactionTestBody(400, filler)

	carrier := map[string]any{
		"type": "additional_tools",
		"tools": []map[string]any{
			{"type": "function", "name": "carrier_only_tool", "description": "Declared only via the carrier."},
		},
	}
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	items, ok := parsed["input"].([]any)
	require.True(t, ok)
	// Put the carrier deep in the trimmable middle.
	mid := len(items) / 2
	items = append(items[:mid], append([]any{carrier}, items[mid:]...)...)
	parsed["input"] = items
	raw, err := json.Marshal(parsed)
	require.NoError(t, err)

	compacted, result, err := compactGrokResponsesInput(raw, grokContextCompactionOptions{
		Enabled:       true,
		TriggerTokens: 200_000,
		TargetTokens:  110_000,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)

	found := false
	gjson.GetBytes(compacted, "input").ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "additional_tools" {
			return true
		}
		if item.Get("tools.0.name").String() == "carrier_only_tool" {
			found = true
		}
		return true
	})
	require.True(t, found, "compaction dropped the additional_tools carrier")
}
