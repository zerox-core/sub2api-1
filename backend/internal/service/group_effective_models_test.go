//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// effectiveModelsTestRepo 供管理端与管理端口径对照使用的账号仓库桩。
type effectiveModelsTestRepo struct {
	accountRepoStub
	accounts []Account
}

func (s *effectiveModelsTestRepo) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	return s.accounts, nil
}

// 分组未启用白名单时，真实可调用列表必须等于账号映射键的原样输出，
// 不得掺入平台默认模型（否则面板会显示调不到的模型）。
func TestComputeGroupEffectiveModels_WithoutAllowlistReturnsAccountMappingsOnly(t *testing.T) {
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-pool": "gpt-5.5"}},
		},
	}

	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Platform:            PlatformOpenAI,
		AvailableByPlatform: map[string][]string{PlatformOpenAI: availableModelsFromAccounts(accounts, PlatformOpenAI)},
	})

	require.Equal(t, []string{"gpt-pool"}, got)
	// 默认目录里的模型不得出现：本组只声明了 gpt-pool。
	require.NotContains(t, got, "gpt-5.4")
}

// 启用白名单后，真实可调用列表 = 白名单对来源集合的过滤结果，
// 且保留白名单自身的书写顺序（FilterForListing 语义）。
func TestComputeGroupEffectiveModels_WithAllowlistFiltersAndKeepsAllowlistOrder(t *testing.T) {
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-pool": "gpt-5.5", "gpt-other": "gpt-5.4"}},
		},
	}
	allowlist := GroupModelAllowlist{
		Enabled: true,
		// 白名单只放行 gpt-other，且顺序与来源相反。
		Models: []string{"gpt-other"},
	}

	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Allowlist:           allowlist,
		Platform:            PlatformOpenAI,
		AvailableByPlatform: map[string][]string{PlatformOpenAI: availableModelsFromAccounts(accounts, PlatformOpenAI)},
	})

	require.Equal(t, []string{"gpt-other"}, got)
}

// 账号映射为空且启用白名单时，来源回落到平台默认目录（与网关
// modelListingSource 一致），过滤后输出白名单交集。
func TestComputeGroupEffectiveModels_AllowlistEmptyMappingsFallBackToPlatformDefaults(t *testing.T) {
	allowlist := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}

	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Allowlist: allowlist,
		Platform:  PlatformOpenAI,
	})

	require.Equal(t, []string{"gpt-5.4"}, got)
}

// 既无映射又无白名单时回落平台默认目录，且不得返回 nil（面板需要稳定输出）。
func TestComputeGroupEffectiveModels_NoMappingsNoAllowlistReturnsPlatformDefaults(t *testing.T) {
	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{Platform: PlatformOpenAI})

	require.NotEmpty(t, got)
	require.Equal(t, DefaultModelIDsForPlatform(PlatformOpenAI), got)
}

// composite 必须逐平台求并集，并在 includeSystemOne=false 时排除 TypeSafe
// （jev-latest 只在 /v1/systemone 生效，不应出现在 LLM 客户端目录里）。
func TestComputeGroupEffectiveModels_CompositeMergesPlatformsAndExcludesTypeSafe(t *testing.T) {
	openaiAccounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-pool": "gpt-5.5"}}},
	}
	geminiAccounts := []Account{
		{ID: 2, Platform: PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-pool": "gemini-2.5-flash"}}},
	}
	available := map[string][]string{
		PlatformOpenAI: availableModelsFromAccounts(openaiAccounts, PlatformOpenAI),
		PlatformGemini: availableModelsFromAccounts(geminiAccounts, PlatformGemini),
	}

	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Platform:             PlatformComposite,
		AvailableByPlatform:  available,
		IncludeSystemOne:     false,
		SchedulablePlatforms: map[string]struct{}{PlatformOpenAI: {}, PlatformGemini: {}},
	})

	require.Contains(t, got, "gpt-pool")
	require.Contains(t, got, "gemini-pool")
	require.NotContains(t, got, "jev-latest")
}

// composite 的路由公开展示模型即使不在任何账号映射中，也必须出现在结果里。
func TestComputeGroupEffectiveModels_CompositeIncludesRouteModels(t *testing.T) {
	got := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Platform:            PlatformComposite,
		AvailableByPlatform: map[string][]string{},
		CompositeRouteModels: []string{
			"route-only-model",
		},
	})

	require.Contains(t, got, "route-only-model")
}

// 管理端 GetGroupEffectiveModels 必须与网关同源：对同一组账号与白名单，
// 结果需等于 ComputeGroupEffectiveModels（即用户端 /v1/models 的口径）。
func TestAdminService_GetGroupEffectiveModels_MatchesSharedComputation(t *testing.T) {
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{"gpt-a": "gpt-5.5", "gpt-b": "gpt-5.4"}},
		},
	}
	allowlist := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-b"}}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			77: {ID: 77, Platform: PlatformOpenAI, ModelAllowlist: allowlist},
		},
	}
	svc := &adminServiceImpl{
		groupRepo:   groupRepo,
		accountRepo: &effectiveModelsTestRepo{accounts: accounts},
	}

	got, err := svc.GetGroupEffectiveModels(context.Background(), 77, "")
	require.NoError(t, err)

	want := ComputeGroupEffectiveModels(GroupEffectiveModelsInput{
		Allowlist:           allowlist,
		Platform:            PlatformOpenAI,
		AvailableByPlatform: map[string][]string{PlatformOpenAI: availableModelsFromAccounts(accounts, PlatformOpenAI)},
	})
	require.Equal(t, want, got)
	require.Equal(t, []string{"gpt-b"}, got)
}

// 管理端与网关的「来源集合」必须逐字一致：availableModelsForPlatform 是
// GetAvailableModels 的纯函数等价物，两者对同一账号集合输出相同切片。
func TestAvailableModelsForPlatform_MatchesAvailableModelsFromAccounts(t *testing.T) {
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-a": "x"}}},
		{ID: 2, Platform: PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-a": "y"}}},
	}

	scoped := make([]Account, 0, len(accounts))
	for i := range accounts {
		if accounts[i].Platform == PlatformOpenAI {
			scoped = append(scoped, accounts[i])
		}
	}

	require.Equal(t, availableModelsFromAccounts(scoped, PlatformOpenAI), availableModelsForPlatform(accounts, PlatformOpenAI))
}

// 缺少分组与账号仓库时不得 panic，直接按平台默认目录回应（Create 流程复用）。
func TestAdminService_GetGroupEffectiveModels_NoGroupNoAccounts(t *testing.T) {
	svc := &adminServiceImpl{}

	got, err := svc.GetGroupEffectiveModels(context.Background(), 0, PlatformOpenAI)

	require.NoError(t, err)
	require.Equal(t, DefaultModelIDsForPlatform(PlatformOpenAI), got)
}
