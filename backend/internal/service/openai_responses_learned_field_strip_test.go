package service

import (
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesLearnedFieldStoreRemembersPerAccountAndModel(t *testing.T) {
	store := newOpenAIResponsesLearnedFieldStore(time.Minute)
	now := time.Unix(1700000000, 0)
	store.now = func() time.Time { return now }

	store.remember(40, "deepseek-v4.1-flash", "summary")
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 1 || got[0] != "summary" {
		t.Fatalf("expected summary for account 40, got %v", got)
	}
	// A different account serving the same model must not inherit the record.
	if got := store.params(41, "deepseek-v4.1-flash"); len(got) != 0 {
		t.Fatalf("account 41 must not inherit, got %v", got)
	}
	// The same account serving a different model must not inherit either.
	if got := store.params(40, "gemini-3.8-flash"); len(got) != 0 {
		t.Fatalf("other model must not inherit, got %v", got)
	}
	// Only the two optional fields may ever be recorded.
	store.remember(40, "deepseek-v4.1-flash", "max_output_tokens")
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 1 {
		t.Fatalf("non-optional param must be ignored, got %v", got)
	}
}

func TestOpenAIResponsesLearnedFieldStoreExpires(t *testing.T) {
	store := newOpenAIResponsesLearnedFieldStore(time.Minute)
	now := time.Unix(1700000000, 0)
	store.now = func() time.Time { return now }

	store.remember(40, "deepseek-v4.1-flash", "verbosity")
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 1 {
		t.Fatalf("expected verbosity, got %v", got)
	}

	now = now.Add(2 * time.Minute)
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 0 {
		t.Fatalf("entry must expire, got %v", got)
	}
}

func TestStripOpenAIResponsesLearnedFieldsRemovesOnlyKnownOptionalFields(t *testing.T) {
	body := []byte(`{"model":"x","reasoning":{"effort":"high","summary":"auto"},"text":{"verbosity":"medium","format":{"type":"text"}},"input":[{"summary":"keep"}]}`)

	out, applied := stripOpenAIResponsesLearnedFields(body, []string{"summary", "verbosity", "max_output_tokens"})
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied fields, got %v", applied)
	}
	if gjson.GetBytes(out, "reasoning.summary").Exists() {
		t.Fatal("reasoning.summary must be removed")
	}
	if gjson.GetBytes(out, "text.verbosity").Exists() {
		t.Fatal("text.verbosity must be removed")
	}
	if !gjson.GetBytes(out, "reasoning.effort").Exists() {
		t.Fatal("reasoning.effort must survive")
	}
	if !gjson.GetBytes(out, "text.format.type").Exists() {
		t.Fatal("text.format must survive")
	}
	if got := gjson.GetBytes(out, "input.0.summary").String(); got != "keep" {
		t.Fatalf("input summary must survive, got %q", got)
	}
}

func TestRememberOpenAIResponsesRejectedFieldsFromStripDetectsRemovedFields(t *testing.T) {
	store := newOpenAIResponsesLearnedFieldStore(time.Minute)
	store.now = func() time.Time { return time.Unix(1700000000, 0) }
	previous := openAIResponsesLearnedFields
	openAIResponsesLearnedFields = store
	defer func() { openAIResponsesLearnedFields = previous }()

	before := []byte(`{"reasoning":{"effort":"high","summary":"auto"}}`)
	after := []byte(`{"reasoning":{"effort":"high"}}`)

	rememberOpenAIResponsesRejectedFieldsFromStrip(before, after, 40, "deepseek-v4.1-flash")
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 1 || got[0] != "summary" {
		t.Fatalf("expected summary learned, got %v", got)
	}

	// A transform that removes nothing must not record anything.
	rememberOpenAIResponsesRejectedFieldsFromStrip(before, before, 40, "deepseek-v4.1-flash")
	if got := store.params(40, "deepseek-v4.1-flash"); len(got) != 1 {
		t.Fatalf("no-op strip must not change records, got %v", got)
	}
}

func TestApplyOpenAIResponsesLearnedFieldStripsEndToEnd(t *testing.T) {
	store := newOpenAIResponsesLearnedFieldStore(time.Minute)
	store.now = func() time.Time { return time.Unix(1700000000, 0) }
	previous := openAIResponsesLearnedFields
	openAIResponsesLearnedFields = store
	defer func() { openAIResponsesLearnedFields = previous }()

	body := []byte(`{"reasoning":{"summary":"auto"},"text":{"verbosity":"medium"}}`)

	out, applied := applyOpenAIResponsesLearnedFieldStrips(body, 40, "deepseek-v4.1-flash")
	if len(applied) != 0 || string(out) != string(body) {
		t.Fatalf("nothing learned yet, body must be untouched (applied=%v)", applied)
	}

	store.remember(40, "deepseek-v4.1-flash", "summary", "verbosity")
	out, applied = applyOpenAIResponsesLearnedFieldStrips(body, 40, "deepseek-v4.1-flash")
	if len(applied) != 2 {
		t.Fatalf("expected both fields stripped, got %v", applied)
	}
	if gjson.GetBytes(out, "reasoning.summary").Exists() || gjson.GetBytes(out, "text.verbosity").Exists() {
		t.Fatal("learned fields must be stripped")
	}
}
