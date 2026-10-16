package service

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// 本文件是「分组真实可调用模型列表」的唯一实现。
//
// 存在两套口径，必须严格区分：
//   - 候选超集（defaultModelsListCandidateIDs + 账号映射键）：给分组白名单
//     编辑器挑选用，允许包含当前不可调用的模型。
//   - 真实可调用列表（本文件）：给管理员查看「这个分组到底能调什么」以及
//     用户端 /v1/models 用，必须与网关的列表输出逐字一致。
//
// 网关与用户端 Key 页看到的是同一份列表；管理端分组弹窗必须复用同一条
// 计算路径，否则会出现「面板说能调、实际调不到」的偏差。

// availableModelsFromAccounts 是 GatewayService.GetAvailableModels 的纯计算核心。
// 抽出为纯函数后，管理端不需要持有 GatewayService（也就不需要缓存与 wire 变更）
// 就能得到与网关完全一致的账号映射键集合。
func availableModelsFromAccounts(accounts []Account, platform string) []string {
	if len(accounts) == 0 {
		return nil
	}

	// Filter by platform if specified. Mixed scheduling (a gemini group routing
	// to antigravity accounts) is honoured here as well, so the advertised list
	// stays in sync with what the request path can actually serve.
	if platform != "" {
		filtered := make([]Account, 0, len(accounts))
		for i := range accounts {
			if accounts[i].Platform == platform || mixedListingAccountAllowed(platform, &accounts[i]) {
				filtered = append(filtered, accounts[i])
			}
		}
		accounts = filtered
	}

	modelSet := make(map[string]struct{})
	hasAnyMapping := false

	for i := range accounts {
		// Passthrough routing accepts models independently of model_mapping, so a
		// stale mapping on a passthrough account must not narrow the public list.
		// Treat it like an unmapped account: skip its mapping here and let
		// supplementUnmappedOpenAIModels contribute the default set.
		if platform == PlatformOpenAI && accounts[i].IsOpenAIPassthroughEnabled() {
			continue
		}

		for model := range accounts[i].GetModelMapping() {
			// Accounts pulled in through mixed scheduling only contribute the
			// models that belong to the listing platform.
			if platform != "" && accounts[i].Platform != platform && !mixedListingModelAllowed(platform, model) {
				continue
			}
			modelSet[model] = struct{}{}
			hasAnyMapping = true
		}
	}

	if !hasAnyMapping {
		return nil
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	if platform == PlatformOpenAI {
		models = supplementUnmappedOpenAIModels(accounts, models)
	}
	return models
}

// modelListingSourceFor 汇总模型列表过滤的候选来源：账号映射键（availableModels）
// 与平台默认列表（fallbackModels）。账号映射为空时回落默认列表；Anthropic
// 平台两者取并集，其余平台以账号映射键为准。
func modelListingSourceFor(platform string, availableModels, fallbackModels []string) []string {
	if len(availableModels) == 0 {
		return fallbackModels
	}
	if platform == PlatformAnthropic {
		return mergeModelIDsFor(availableModels, fallbackModels)
	}
	return availableModels
}

func mergeModelIDsFor(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, models := range [][]string{primary, secondary} {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	return merged
}

// ModelListingSource 是 modelListingSourceFor 的导出形式，供 gateway handler
// 复用，确保「列表过滤来源」在全服务端只有一条实现。
func ModelListingSource(platform string, availableModels, fallbackModels []string) []string {
	return modelListingSourceFor(platform, availableModels, fallbackModels)
}

// DefaultModelIDsForPlatform 暴露平台静态默认模型目录，供网关 handler 与管理端
// 共用同一份数据源，避免两处静态列表（以及 composite 的 TypeSafe 排除规则）漂移。
func DefaultModelIDsForPlatform(platform string) []string {
	return defaultModelsListCandidateIDs(platform)
}

// GroupEffectiveModelsInput 汇总计算真实可调用列表所需的全部输入。
// 两个调用方（网关 handler / 管理端 admin service）各自负责取数，
// 计算本身只走 ComputeGroupEffectiveModels 这一条路径。
type GroupEffectiveModelsInput struct {
	// Allowlist 是分组模型白名单；未启用时按「看到什么 = 能调什么」直接输出账号能力。
	Allowlist GroupModelAllowlist
	// Platform 为分组平台（composite 需逐个具体平台求并集）。
	Platform string
	// AvailableByPlatform 是各平台的账号映射键集合（availableModelsFromAccounts 口径）。
	AvailableByPlatform map[string][]string
	// SchedulablePlatforms 是组内当前有可调度账号的具体平台。
	SchedulablePlatforms map[string]struct{}
	// CompositeRouteModels 是 composite 精确路由公开的模型（可为空）。
	CompositeRouteModels []string
	// IncludeSystemOne 控制是否纳入只在 /v1/systemone 生效的 TypeSafe 模型。
	IncludeSystemOne bool
}

// ComputeGroupEffectiveModels 返回分组的真实可调用模型列表。
// 与 GatewayHandler.Models 的取值逐字一致：白名单启用时以白名单顺序输出，
// 未启用时输出账号映射键（空则回落平台默认列表）。
func ComputeGroupEffectiveModels(in GroupEffectiveModelsInput) []string {
	platform := strings.TrimSpace(in.Platform)
	if platform == "" {
		platform = PlatformAnthropic
	}
	if platform == PlatformComposite {
		return compositeEffectiveModels(in)
	}

	available := in.AvailableByPlatform[platform]
	if in.Allowlist.Enabled {
		source := modelListingSourceFor(platform, available, defaultModelsListCandidateIDs(platform))
		return in.Allowlist.FilterForListing(source)
	}
	if len(available) > 0 {
		return available
	}
	return defaultModelsListCandidateIDs(platform)
}

func compositeEffectiveModels(in GroupEffectiveModelsInput) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, platform := range domain.CompositePrecedencePlatformIDs() {
		// TypeSafe 只在 /v1/systemone 生效；LLM 客户端的目录必须排除。
		if platform == PlatformTypeSafe && !in.IncludeSystemOne {
			continue
		}
		platformModels := in.AvailableByPlatform[platform]
		if len(platformModels) == 0 {
			// CN 供应商没有静态默认模型列表（defaultModelsListCandidateIDs 的
			// default 分支是 Claude 列表），composite 下只暴露账号映射键。
			if _, ok := in.SchedulablePlatforms[platform]; ok && !IsMultiProtocolAPIKeyProvider(platform) {
				platformModels = defaultModelsListCandidateIDs(platform)
			}
		}
		for _, model := range platformModels {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			models = append(models, model)
		}
	}
	// A route can expose a public ID that no account model mapping contains.
	for _, model := range in.CompositeRouteModels {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}

	if in.Allowlist.Enabled {
		source := models
		if len(source) == 0 {
			source = defaultModelsListCandidateIDs(PlatformComposite)
		}
		return in.Allowlist.FilterForListing(source)
	}
	if len(models) > 0 {
		return models
	}
	return defaultModelsListCandidateIDs(PlatformComposite)
}
