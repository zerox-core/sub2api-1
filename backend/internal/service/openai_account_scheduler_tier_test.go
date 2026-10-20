package service

import (
	"testing"
)

func TestOpenAIAccountSchedulingTier(t *testing.T) {
	cases := []struct {
		name    string
		account *Account
		want    int
	}{
		{name: "nil account", account: nil, want: 0},
		{name: "nil extra", account: &Account{}, want: 0},
		{name: "missing key", account: &Account{Extra: map[string]any{}}, want: 0},
		{name: "zero value", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: float64(0)}}, want: 0},
		{name: "int one", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: 1}}, want: 1},
		{name: "float one", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: float64(1)}}, want: 1},
		{name: "string two", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: "2"}}, want: 2},
		{name: "fractional floors", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: 1.9}}, want: 1},
		{name: "negative normalizes to zero", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: -3}}, want: 0},
		{name: "garbage normalizes to zero", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: "backstop"}}, want: 0},
		{name: "bool normalizes to zero", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: true}}, want: 0},
		{name: "clamped to max", account: &Account{Extra: map[string]any{OpenAISchedulerTierExtraKey: 999}}, want: maxOpenAISchedulerTier},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openAIAccountSchedulingTier(tc.account); got != tc.want {
				t.Fatalf("openAIAccountSchedulingTier() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPartitionOpenAIAccountsBySchedulingTier(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		if got := partitionOpenAIAccountsBySchedulingTier(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("all default tier is a single group", func(t *testing.T) {
		a, b := &Account{ID: 1}, &Account{ID: 2}
		groups := partitionOpenAIAccountsBySchedulingTier([]*Account{a, b})
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		if len(groups[0]) != 2 {
			t.Fatalf("expected 2 accounts in tier 0, got %d", len(groups[0]))
		}
	})

	t.Run("mixed tiers ordered low to high", func(t *testing.T) {
		primary := &Account{ID: 1}
		backstop := &Account{ID: 2, Extra: map[string]any{OpenAISchedulerTierExtraKey: 1}}
		deeper := &Account{ID: 3, Extra: map[string]any{OpenAISchedulerTierExtraKey: 3}}
		groups := partitionOpenAIAccountsBySchedulingTier([]*Account{backstop, primary, deeper})
		if len(groups) != 3 {
			t.Fatalf("expected 3 non-empty groups, got %d", len(groups))
		}
		if groups[0][0].ID != 1 {
			t.Fatalf("tier 0 must come first, got account %d", groups[0][0].ID)
		}
		if groups[1][0].ID != 2 {
			t.Fatalf("tier 1 must come second, got account %d", groups[1][0].ID)
		}
		if groups[2][0].ID != 3 {
			t.Fatalf("tier 3 must come last, got account %d", groups[2][0].ID)
		}
	})

	t.Run("nil accounts are skipped", func(t *testing.T) {
		groups := partitionOpenAIAccountsBySchedulingTier([]*Account{nil, {ID: 5}, nil})
		if len(groups) != 1 || len(groups[0]) != 1 {
			t.Fatalf("expected single group with one account, got %v", groups)
		}
	})

	t.Run("only nil accounts returns nil", func(t *testing.T) {
		if got := partitionOpenAIAccountsBySchedulingTier([]*Account{nil, nil}); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
}

func TestHasOpenAISchedulerTierBackstop(t *testing.T) {
	if hasOpenAISchedulerTierBackstop(nil) {
		t.Fatal("empty pool must not report backstop")
	}
	if hasOpenAISchedulerTierBackstop([]*Account{{ID: 1}, {ID: 2}}) {
		t.Fatal("default-tier pool must not report backstop")
	}
	if !hasOpenAISchedulerTierBackstop([]*Account{{ID: 1}, {ID: 2, Extra: map[string]any{OpenAISchedulerTierExtraKey: 1}}}) {
		t.Fatal("pool with a tier>0 account must report backstop")
	}
}

func TestDescribeOpenAISchedulerTierPool(t *testing.T) {
	if got := describeOpenAISchedulerTierPool(nil); got != "" {
		t.Fatalf("expected empty description, got %q", got)
	}
	groups := [][]*Account{
		{{ID: 1}, {ID: 2}, {ID: 3}},
		{{ID: 4, Extra: map[string]any{OpenAISchedulerTierExtraKey: 1}}},
	}
	if got, want := describeOpenAISchedulerTierPool(groups), "0:3,1:1"; got != want {
		t.Fatalf("describeOpenAISchedulerTierPool() = %q, want %q", got, want)
	}
}
