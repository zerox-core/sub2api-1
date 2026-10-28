package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.Contains(t, DefaultModelIDs(), "gpt-6")
	var displayName string
	for _, model := range DefaultModels {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestDefaultModelsPreferConcreteGPT56SolForAccountTests(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-5.6-sol", DefaultModels[0].ID)
}

func TestDefaultModelsIncludeGPTImage25(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-flare")
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-sunburst")
}

func TestGPT6SolLunaModelIdentity(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		require.Contains(t, DefaultModelIDs(), model)
		require.True(t, IsGPT6SolOrLunaModelSpelling(model))
	}
	require.False(t, IsGPT6SolOrLunaModelSpelling("gpt-6-astra"))
	require.False(t, IsGPT6SolOrLunaModelSpelling("gpt-6-solitude"))
	require.False(t, IsGPT6SolOrLunaModelSpelling("gpt-6-luna-preview"))
}

func TestGPT61SolIdentityAndEffort(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6.1-sol")
	for _, id := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol-max", "GPT_6.1_SOL", "gpt-6.1-sol-openai-compact"} {
		require.True(t, IsGPT61SolModelSpelling(id), id)
		require.False(t, IsGPT6SolOrLunaModelSpelling(id), id)
	}
	for _, id := range []string{"gpt-6.1", "gpt-6.1-solitude", "gpt-6.1-sol-preview", "gpt-6-sol"} {
		require.False(t, IsGPT61SolModelSpelling(id), id)
	}
	for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		require.NoError(t, ValidateGPT61SolReasoningEffort("gpt-6.1-sol", effort))
	}
	for _, effort := range []string{"none", "minimal"} {
		require.Error(t, ValidateGPT61SolReasoningEffort("gpt-6.1-sol", effort))
		require.NoError(t, ValidateGPT61SolReasoningEffort("gpt-6-sol", effort))
	}
	require.Contains(t, CodexBaseInstructionsForModel("gpt-6.1-sol"), "based on GPT-6")
}

func TestGPT61TerraIdentityAndEffort(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6.1-terra")

	for _, id := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "GPT_6.1_TERRA", "gpt-6.1-terra-openai-compact"} {
		require.True(t, IsGPT61TerraModelSpelling(id), id)
		require.True(t, IsGPT61ModelSpelling(id), id)
		require.False(t, IsGPT61SolModelSpelling(id), id)
		require.False(t, IsGPT6SolOrLunaModelSpelling(id), id)
	}
	for _, id := range []string{"gpt-6.1", "gpt-6.1-terrace", "gpt-6.1-terra-preview", "gpt-6-sol"} {
		require.False(t, IsGPT61TerraModelSpelling(id), id)
	}

	// Terra publishes ultra on top of the Sol ladder.
	for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max", "ultra"} {
		require.NoError(t, ValidateGPT61ReasoningEffort("gpt-6.1-terra", effort), effort)
	}
	for _, effort := range []string{"none", "minimal"} {
		require.Error(t, ValidateGPT61ReasoningEffort("gpt-6.1-terra", effort), effort)
	}
	// Sol must keep rejecting ultra: only Terra advertises it.
	require.NoError(t, ValidateGPT61ReasoningEffort("gpt-6.1-sol", "max"))
	require.True(t, IsGPT61ModelSpelling("gpt-6.1-sol"))
	require.True(t, IsGPT61ModelSpelling("gpt-6.1-terra"))

	require.Contains(t, CodexBaseInstructionsForModel("gpt-6.1-terra"), "based on GPT-6")
}

func TestGPT61TerraDescriptorMatchesOfficialCatalog(t *testing.T) {
	var meta struct {
		Slug             string `json:"slug"`
		ContextWindow    int    `json:"context_window"`
		MaxContextWindow int    `json:"max_context_window"`
		DefaultReasoning string `json:"default_reasoning_level"`
		SupportedLevels  []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
		InputModalities   []string `json:"input_modalities"`
		SupportsReasoning bool     `json:"supports_reasoning_summaries"`
	}
	require.NoError(t, json.Unmarshal(CodexGPT61TerraMetadata, &meta))

	// Values pinned from openai/codex release/0.165.0-alpha.1.2 models.json.
	require.Equal(t, "gpt-6.1-terra", meta.Slug)
	require.Equal(t, 500000, meta.ContextWindow)
	require.Equal(t, 872000, meta.MaxContextWindow)
	require.Equal(t, "low", meta.DefaultReasoning)
	require.Equal(t, []string{"text", "image"}, meta.InputModalities)

	efforts := make([]string, 0, len(meta.SupportedLevels))
	for _, level := range meta.SupportedLevels {
		efforts = append(efforts, level.Effort)
	}
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, efforts)
}
