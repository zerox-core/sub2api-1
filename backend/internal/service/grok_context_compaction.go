package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/tiktoken-go/tokenizer"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Grok request-side context compaction.
//
// xAI prices grok-4.x with a session-wide long-context multiplier: once
// (input + cache_creation + cache_read) reaches 200k tokens, every input,
// output and cache-read token of that request is billed at 2x. Clients do not
// trim at that point because the model window is 500k and client-side
// auto-compaction only fires near the window limit, so a long session drifts
// through the whole 200k..500k band paying double.
//
// The gateway therefore trims the request itself: when the estimated input of a
// Grok turn crosses the trigger, the oldest complete turns are dropped from the
// middle of the input history and replaced by a single placeholder item, which
// brings the turn back under the flat-price threshold.
//
// Cost note: the trimmed body no longer shares a byte prefix with the previous
// turn, so the first compacted request pays full price for the tokens it keeps.
// Subsequent turns share the compacted prefix again and return to cache pricing.
// Compacting once deeply (110k) is therefore much cheaper than compacting
// shallowly and often.

const (
	defaultGrokContextCompactionTriggerTokens = 200_000
	defaultGrokContextCompactionTargetTokens  = 110_000
	// grokContextCompactionHeadTokenShare caps how much of the input budget the
	// preserved opening item may consume (budget/N).
	grokContextCompactionHeadTokenShare = 5
	// grokContextCompactionMinBudgetTokens keeps the tail budget usable even when
	// instructions+tools already eat most of the target.
	grokContextCompactionMinBudgetTokens = 2_000
	// grokContextCompactionPlaceholderReserveTokens is held back from the history
	// budget so the placeholder item still fits inside the target.
	grokContextCompactionPlaceholderReserveTokens = 256
	// grokContextCompactionBytesPerToken is the conservative lower bound used for
	// the cheap pre-reject; JSON bodies always carry at least this many bytes per
	// token, so a shorter body cannot cross the trigger.
	grokContextCompactionBytesPerToken = 3

	grokContextCompactionContextKey = "grok_context_compaction"
)

// grokContextCompactionOptions configures one compaction round.
type grokContextCompactionOptions struct {
	Enabled       bool
	TriggerTokens int
	TargetTokens  int
}

func (o grokContextCompactionOptions) normalized() grokContextCompactionOptions {
	if o.TriggerTokens <= 0 {
		o.TriggerTokens = defaultGrokContextCompactionTriggerTokens
	}
	if o.TargetTokens <= 0 || o.TargetTokens >= o.TriggerTokens {
		o.TargetTokens = defaultGrokContextCompactionTargetTokens
	}
	if o.TargetTokens >= o.TriggerTokens {
		o.TargetTokens = o.TriggerTokens * 3 / 5
	}
	return o
}

// grokContextCompactionResult reports what one compaction round changed.
type grokContextCompactionResult struct {
	Applied         bool
	OriginalTokens  int
	CompactedTokens int
	DroppedItems    int
	DroppedTokens   int
	TriggerTokens   int
	TargetTokens    int
}

func (r *grokContextCompactionResult) zapFields() []zap.Field {
	if r == nil {
		return nil
	}
	return []zap.Field{
		zap.Int("original_tokens", r.OriginalTokens),
		zap.Int("compacted_tokens", r.CompactedTokens),
		zap.Int("dropped_items", r.DroppedItems),
		zap.Int("dropped_tokens", r.DroppedTokens),
		zap.Int("trigger_tokens", r.TriggerTokens),
		zap.Int("target_tokens", r.TargetTokens),
	}
}

// compactGrokRequestBody trims a Responses-shaped Grok body when its estimated
// input crosses the configured trigger. Errors are returned for logging only:
// callers must keep forwarding the original body on failure.
func (s *OpenAIGatewayService) compactGrokRequestBody(
	c *gin.Context,
	body []byte,
	account *Account,
	upstreamModel string,
) ([]byte, *grokContextCompactionResult, error) {
	opts := s.grokContextCompactionOptions()
	if !opts.Enabled {
		return body, nil, nil
	}
	compacted, result, err := compactGrokResponsesInput(body, opts)
	if err != nil {
		return body, result, err
	}
	if result == nil || !result.Applied {
		return body, result, nil
	}
	fields := result.zapFields()
	fields = append(fields,
		zap.String("upstream_model", upstreamModel),
	)
	if account != nil {
		fields = append(fields, zap.Int64("account_id", account.ID))
	}
	logger.L().Info("grok context compaction applied", fields...)
	if c != nil {
		c.Set(grokContextCompactionContextKey, result)
	}
	return compacted, result, nil
}

func (s *OpenAIGatewayService) grokContextCompactionOptions() grokContextCompactionOptions {
	if s == nil || s.cfg == nil {
		return grokContextCompactionOptions{}
	}
	grok := s.cfg.Gateway.Grok
	return grokContextCompactionOptions{
		Enabled:       grok.ContextCompactionEnabled,
		TriggerTokens: grok.ContextCompactionTriggerTokens,
		TargetTokens:  grok.ContextCompactionTargetTokens,
	}.normalized()
}

// compactGrokResponsesInput drops the oldest complete turns from the middle of
// a Responses request body's input array once the estimated input reaches the
// trigger, and inserts a placeholder describing what was removed.
func compactGrokResponsesInput(
	body []byte,
	opts grokContextCompactionOptions,
) ([]byte, *grokContextCompactionResult, error) {
	opts = opts.normalized()
	if !opts.Enabled {
		return body, nil, nil
	}
	if len(body) < opts.TriggerTokens*grokContextCompactionBytesPerToken {
		return body, nil, nil
	}
	inputRaw := gjson.GetBytes(body, "input")
	if !inputRaw.IsArray() {
		return body, nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(inputRaw.Raw), &items); err != nil {
		return body, nil, nil
	}
	if len(items) < 4 {
		return body, nil, nil
	}
	codec, err := openAIInputTokensCodecForModel(gjson.GetBytes(body, "model").String())
	if err != nil {
		return body, nil, err
	}

	itemTokens := make([]int, len(items))
	inputTokens := 0
	for i := range items {
		n, countErr := grokCompactionEstimateItemTokens(codec, items[i])
		if countErr != nil {
			return body, nil, countErr
		}
		itemTokens[i] = n
		inputTokens += n
	}
	overhead := grokContextCompactionFixedOverheadTokens(body)
	originalTokens := inputTokens + overhead
	if originalTokens < opts.TriggerTokens {
		return body, nil, nil
	}

	// Reserve room for the placeholder sentence so the compacted request still
	// lands inside the target rather than just under it.
	budget := opts.TargetTokens - overhead - grokContextCompactionPlaceholderReserveTokens
	if budget < grokContextCompactionMinBudgetTokens {
		budget = grokContextCompactionMinBudgetTokens
	}
	pairs := newGrokItemPairIndex(items)

	// Preserve the opening request item when it is cheap: it usually carries the
	// task statement, and dropping it costs more quality than it saves. A leading
	// tool carrier is always preserved regardless of size, because dropping it
	// would silently strip the client's tools from the upstream request.
	headEnd := 0
	headTokens := 0
	if len(items) > 2 {
		switch grokCompactionItemType(items[0]) {
		case "additional_tools":
			headEnd = 1
			headTokens = itemTokens[0]
		case "message":
			if itemTokens[0] <= budget/grokContextCompactionHeadTokenShare {
				headEnd = 1
				headTokens = itemTokens[0]
			}
		}
	}

	remain := budget - headTokens
	tailStart := len(items) - 1
	tailTokens := itemTokens[tailStart]
	for i := len(items) - 2; i > headEnd; i-- {
		if tailTokens+itemTokens[i] > remain {
			break
		}
		tailTokens += itemTokens[i]
		tailStart = i
	}
	if tailStart <= headEnd {
		return body, nil, nil
	}

	// Snap the cut forward to a turn boundary so no function_call is separated
	// from its function_call_output, which xAI rejects with a 400.
	cut := -1
	for j := tailStart; j < len(items); j++ {
		if grokCompactionCutAllowed(items, j, pairs) {
			cut = j
			break
		}
	}
	if cut <= headEnd {
		return body, nil, nil
	}

	droppedItems := cut - headEnd
	droppedTokens := 0
	// A dropped additional_tools carrier would silently remove client tools from
	// the upstream request, so rescue every carrier before computing the budget.
	var rescuedItems []json.RawMessage
	rescuedTokens := 0
	for i := headEnd; i < cut; i++ {
		droppedTokens += itemTokens[i]
		if grokCompactionItemType(items[i]) == "additional_tools" {
			rescuedItems = append(rescuedItems, items[i])
			rescuedTokens += itemTokens[i]
		}
	}
	droppedItems -= len(rescuedItems)
	droppedTokens -= rescuedTokens

	// The cut always lands on a user turn. Two things follow: a standalone user
	// placeholder would add another user turn, and a preserved opening user turn
	// would sit directly next to the cut user turn. Both create back-to-back user
	// turns, which some Responses-compatible upstreams reject. Fold the note into
	// the surviving turn, merging the two user turns when the head is kept.
	note := grokCompactionPlaceholderText(droppedItems, droppedTokens)
	headEndBeforeMerge := headEnd
	replacement := make([]json.RawMessage, 0, len(items)-cut+1)
	switch {
	case headEnd > 0 && grokCompactionUserMessage(items[headEnd-1]) && grokCompactionUserMessage(items[cut]):
		merged, ok := grokCompactionMergeUserMessages(items[headEnd-1], items[cut], note)
		if !ok {
			return body, nil, nil
		}
		headEnd--
		replacement = append(replacement, merged)
		replacement = append(replacement, items[cut+1:]...)
	default:
		if merged, ok := grokCompactionMergePlaceholderText(items[cut], note); ok {
			replacement = append(replacement, merged)
			replacement = append(replacement, items[cut+1:]...)
		} else {
			replacement = append(replacement, grokCompactionPlaceholderItem(note))
			replacement = append(replacement, items[cut:]...)
		}
	}
	if headEnd != headEndBeforeMerge {
		headTokens = 0
		for i := 0; i < headEnd; i++ {
			headTokens += itemTokens[i]
		}
	}
	replacementTokens := 0
	for i := range replacement {
		n, countErr := grokCompactionEstimateItemTokens(codec, replacement[i])
		if countErr != nil {
			return body, nil, countErr
		}
		replacementTokens += n
	}

	compactedTokens := headTokens + replacementTokens + rescuedTokens + overhead
	if compactedTokens >= originalTokens {
		return body, nil, nil
	}

	newItems := make([]json.RawMessage, 0, headEnd+len(rescuedItems)+len(replacement))
	newItems = append(newItems, items[:headEnd]...)
	newItems = append(newItems, rescuedItems...)
	newItems = append(newItems, replacement...)
	newInput, err := json.Marshal(newItems)
	if err != nil {
		return body, nil, err
	}
	compactedBody, err := sjson.SetRawBytes(body, "input", newInput)
	if err != nil {
		return body, nil, err
	}

	return compactedBody, &grokContextCompactionResult{
		Applied:         true,
		OriginalTokens:  originalTokens,
		CompactedTokens: compactedTokens,
		DroppedItems:    droppedItems,
		DroppedTokens:   droppedTokens,
		TriggerTokens:   opts.TriggerTokens,
		TargetTokens:    opts.TargetTokens,
	}, nil
}

// grokCompactionEstimateItemTokens counts one input item by its serialized
// JSON. This slightly over-estimates versus the field-wise estimator, which is
// the safe direction for a trigger threshold.
func grokCompactionEstimateItemTokens(codec tokenizer.Codec, raw json.RawMessage) (int, error) {
	return codec.Count(string(raw))
}

// grokContextCompactionFixedOverheadTokens counts the part of the request that
// compaction cannot touch (instructions, tools, tool_choice).
func grokContextCompactionFixedOverheadTokens(body []byte) int {
	var req openAIInputTokensCountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	req.Input = nil
	n, err := estimateOpenAIInputTokens(req)
	if err != nil {
		return 0
	}
	return n
}

type grokItemPairIndex struct {
	callIdx map[string]int
	outIdx  map[string]int
}

func newGrokItemPairIndex(items []json.RawMessage) grokItemPairIndex {
	index := grokItemPairIndex{
		callIdx: make(map[string]int),
		outIdx:  make(map[string]int),
	}
	for i, raw := range items {
		callID := strings.TrimSpace(gjson.GetBytes(raw, "call_id").String())
		if callID == "" {
			continue
		}
		switch grokCompactionItemType(raw) {
		case "function_call", "custom_tool_call":
			if _, ok := index.callIdx[callID]; !ok {
				index.callIdx[callID] = i
			}
		case "function_call_output", "custom_tool_call_output":
			if _, ok := index.outIdx[callID]; !ok {
				index.outIdx[callID] = i
			}
		}
	}
	return index
}

func grokCompactionItemType(raw json.RawMessage) string {
	typ := strings.TrimSpace(gjson.GetBytes(raw, "type").String())
	if typ == "" {
		return "message"
	}
	return typ
}

// grokCompactionCutAllowed reports whether the history may be cut at idx: the
// item must start a new turn, and no tool call may straddle the cut.
func grokCompactionCutAllowed(items []json.RawMessage, idx int, pairs grokItemPairIndex) bool {
	if idx <= 0 || idx > len(items)-1 {
		return false
	}
	if grokCompactionItemType(items[idx]) != "message" {
		return false
	}
	if strings.TrimSpace(gjson.GetBytes(items[idx], "role").String()) != "user" {
		return false
	}
	for callID, callAt := range pairs.callIdx {
		outputAt, ok := pairs.outIdx[callID]
		if !ok {
			continue
		}
		if callAt < idx && outputAt >= idx {
			return false
		}
	}
	return true
}

func grokCompactionPlaceholderText(droppedItems, droppedTokens int) string {
	return fmt.Sprintf(
		"[gateway context compaction] %d earlier messages (~%dk tokens of prior work and tool output) "+
			"were removed from this conversation to keep the request inside the flat-price context window. "+
			"Continue from the remaining messages; re-read files or re-run commands if you still need their contents.",
		droppedItems, droppedTokens/1000,
	)
}

func grokCompactionPlaceholderItem(text string) json.RawMessage {
	item := map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{
			{"type": "input_text", "text": text},
		},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"[gateway context compaction] earlier messages were removed to keep this request inside the flat-price context window."}]}`)
	}
	return raw
}

// grokCompactionMergePlaceholderText prepends the compaction note to the text of
// an existing user message item, so no extra user turn is introduced. It reports
// false when the item is not a plain text user message that can absorb the note.
func grokCompactionMergePlaceholderText(raw json.RawMessage, text string) (json.RawMessage, bool) {
	if !grokCompactionUserMessage(raw) {
		return nil, false
	}
	return grokCompactionMergeUserMessages(raw, nil, text)
}

// grokCompactionUserMessage reports whether raw is a plain user message item.
func grokCompactionUserMessage(raw json.RawMessage) bool {
	if grokCompactionItemType(raw) != "message" {
		return false
	}
	return strings.TrimSpace(gjson.GetBytes(raw, "role").String()) == "user"
}

// grokCompactionMergeUserMessages collapses two consecutive user messages into
// the first one, optionally prefixing a note, so the history keeps a single user
// turn where the trim happened. It returns false when the shapes are not text
// messages it can safely rewrite.
func grokCompactionMergeUserMessages(
	first json.RawMessage,
	second json.RawMessage,
	note string,
) (json.RawMessage, bool) {
	firstText, ok := grokCompactionUserMessageText(first)
	if !ok {
		return nil, false
	}
	text := note + "\n\n" + firstText
	if second != nil {
		secondText, secondOK := grokCompactionUserMessageText(second)
		if !secondOK {
			return nil, false
		}
		if secondText != "" {
			text += "\n\n" + secondText
		}
	}
	return grokCompactionUserMessageWithText(first, text)
}

// grokCompactionUserMessageText extracts the text of a plain user message item.
func grokCompactionUserMessageText(raw json.RawMessage) (string, bool) {
	content := gjson.GetBytes(raw, "content")
	if content.Type == gjson.String {
		return content.String(), true
	}
	first := gjson.GetBytes(raw, "content.0")
	if !first.Exists() {
		return "", false
	}
	if typ := strings.TrimSpace(first.Get("type").String()); typ != "" && typ != "input_text" && typ != "text" {
		return "", false
	}
	if !first.Get("text").Exists() {
		return "", false
	}
	return first.Get("text").String(), true
}

// grokCompactionUserMessageWithText writes text back into a user message item,
// preserving the item's original content shape.
func grokCompactionUserMessageWithText(raw json.RawMessage, text string) (json.RawMessage, bool) {
	if gjson.GetBytes(raw, "content").Type == gjson.String {
		merged, err := sjson.SetBytes(append([]byte(nil), raw...), "content", text)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(merged), true
	}
	merged, err := sjson.SetBytes(append([]byte(nil), raw...), "content.0.text", text)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(merged), true
}
