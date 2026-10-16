import { classifyModelType, type ModelDisplayType } from '@/utils/modelType'

export interface ModelAllowlistConfig {
  enabled: boolean
  models: string[]
  /** R6：分组级按模型功能过滤（空 = 不限制），多个模态取并集 */
  modalities?: string[]
  /** R6：不单独放行用换 id 表达挡位的变体 */
  exclude_thinking_variants?: boolean
}

export interface ModelAllowlistItem {
  id: string
  selected: boolean
}

// 分组级 auto（智能路由）sentinel 策略：与后端 auto_router.go 的 ParseAutoSentinel 对齐。
// 白名单里存 sentinel 条目（如 auto::rr），请求基名 auto 时由网关改写为池内真实模型。
export type ModelAllowlistAutoStrategy = 'rr' | 'rand'

export const MODEL_ALLOWLIST_AUTO_SENTINELS: Record<ModelAllowlistAutoStrategy, string> = {
  rr: 'auto::rr',
  rand: 'auto::rand',
}

// parseAutoSentinelEntry 解析白名单条目里的 auto sentinel；非 sentinel 返回 null。
export const parseAutoSentinelEntry = (
  entry: string,
): ModelAllowlistAutoStrategy | null => {
  const trimmed = entry.trim()
  const idx = trimmed.lastIndexOf('::')
  if (idx <= 0) return null
  const name = trimmed.slice(0, idx).trim()
  if (name.toLowerCase() !== 'auto') return null
  const strategy = trimmed
    .slice(idx + 2)
    .trim()
    .toLowerCase()
  if (strategy === 'rr' || strategy === 'round_robin') return 'rr'
  if (strategy === 'rand' || strategy === 'random') return 'rand'
  return null
}

const stripAutoSentinels = (models: string[]): string[] =>
  models.filter(model => parseAutoSentinelEntry(model) === null)

export interface ModelAllowlistState {
  enabled: boolean
  // auto 智能路由面板状态；sentinel 不作为普通条目出现在 items 里，
  // 由 buildModelAllowlistConfig 在提交时按需注入到模型列表首位。
  autoEnabled: boolean
  autoStrategy: ModelAllowlistAutoStrategy
  // R6：分组级「按模型功能过滤」。与上面的白名单条目是两个独立维度（AND 叠加）：
  // 白名单回答"暴露哪些模型"，这里回答"这个分组要不要图像/语音模型"。
  modalities: string[]
  excludeThinkingVariants: boolean
  savedModels: string[]
  items: ModelAllowlistItem[]
}

// 自定义条目校验错误码，由视图映射为 i18n 提示。
export type ModelAllowlistAddError = 'empty' | 'duplicate'

export const createModelAllowlistState = (
  config?: Partial<ModelAllowlistConfig> | null,
): ModelAllowlistState => {
  const savedModels = normalizeModels(config?.models ?? [])
  const state: ModelAllowlistState = {
    enabled: config?.enabled ?? false,
    autoEnabled: false,
    autoStrategy: 'rr',
    modalities: normalizeModalities(config?.modalities ?? []),
    excludeThinkingVariants: config?.exclude_thinking_variants ?? false,
    savedModels,
    items: [],
  }
  for (const model of savedModels) {
    const strategy = parseAutoSentinelEntry(model)
    if (strategy !== null) {
      state.autoEnabled = true
      state.autoStrategy = strategy
      break
    }
  }
  return state
}

export const hydrateModelAllowlistState = (
  config: Partial<ModelAllowlistConfig> | null | undefined,
  candidates: string[],
): ModelAllowlistState => {
  const state = createModelAllowlistState(config)
  setModelAllowlistCandidates(state, candidates)
  return state
}

export const setModelAllowlistCandidates = (
  state: ModelAllowlistState,
  candidates: string[],
) => {
  const normalizedCandidates = stripAutoSentinels(normalizeModels(candidates))
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => item.id),
  )
  const currentKnown = new Set(state.items.map(item => item.id))
  const savedSelected = new Set(stripAutoSentinels(state.savedModels))
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([
    ...state.items.map(item => item.id),
    ...stripAutoSentinels(state.savedModels),
    ...normalizedCandidates,
  ])

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(id)
      : savedSelected.size > 0
        ? savedSelected.has(id)
        : normalizedCandidates.includes(id)

    return {
      id,
      selected: selected && (currentKnown.has(id) || savedSelected.has(id) || savedSelected.size === 0),
    }
  })
}

export const toggleModelAllowlistItem = (
  state: ModelAllowlistState,
  modelID: string,
) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

// selectAllModelAllowlistItems 全选。
//
// 传入 visibleModelIDs 时只作用于可见（筛选后）的条目：分组 14 有 189 个模型，
// 用户点了搜索或模态筛选之后按"全选"，期望的是选中当前看到的那批，
// 而不是把 189 个全部勾上。不传参数时保持原有"全选所有条目"行为。
export const selectAllModelAllowlistItems = (
  state: ModelAllowlistState,
  visibleModelIDs?: Iterable<string>,
) => {
  const visible = visibleModelIDs ? new Set(visibleModelIDs) : null
  state.items.forEach(item => {
    if (!visible || visible.has(item.id)) {
      item.selected = true
    }
  })
}

// =====================
// R7：候选模型的搜索 / 模态筛选 / 排序
// =====================

export type ModelAllowlistSortKey = 'default' | 'name' | 'modality' | 'selected'

export interface ModelAllowlistViewFilter {
  /** 模型名包含匹配（大小写不敏感） */
  query: string
  /** 模态筛选，多选取「任一命中」；空数组表示不按模态过滤 */
  modalities: string[]
  /** 功能归类筛选（大语言/视觉/生图/语音/视频），空表示不限。仅管理端分组配置使用。 */
  type: ModelDisplayType | ''
  /** 来源账号筛选（按账号名包含匹配），空表示不限账号。仅管理端分组配置使用。 */
  sourceAccount: string
  /** 只看已选中 */
  onlySelected: boolean
  sortBy: ModelAllowlistSortKey
}

export const createModelAllowlistViewFilter = (): ModelAllowlistViewFilter => ({
  query: '',
  modalities: [],
  type: '',
  sourceAccount: '',
  onlySelected: false,
  sortBy: 'default',
})

// 模态标签由后端能力目录（R6 model_capability.go）提供；查不到时视为纯文本。
export type ModelModalitiesLookup = (modelID: string) => string[]
/** 模型名 → 声明该模型的来源账号名列表；仅管理端分组配置使用。 */
export type ModelSourceAccountsLookup = (modelID: string) => string[]

export const modelAllowlistModalityOptions = [
  'text',
  'vision',
  'image',
  'audio',
  'video',
  'embedding',
] as const

export const matchesModelAllowlistFilter = (
  item: ModelAllowlistItem,
  filter: ModelAllowlistViewFilter,
  modalitiesOf?: ModelModalitiesLookup,
  sourcesOf?: ModelSourceAccountsLookup,
): boolean => {
  const query = filter.query.trim().toLowerCase()
  if (query && !item.id.toLowerCase().includes(query)) {
    return false
  }
  if (filter.onlySelected && !item.selected) {
    return false
  }
  if (filter.modalities.length > 0) {
    const labels = modalitiesOf ? modalitiesOf(item.id) : []
    if (!filter.modalities.some(modality => labels.includes(modality))) {
      return false
    }
  }
  if (filter.type && classifyModelType(item.id) !== filter.type) {
    return false
  }
  if (filter.sourceAccount) {
    const accounts = sourcesOf ? sourcesOf(item.id) : []
    if (!accounts.includes(filter.sourceAccount)) {
      return false
    }
  }
  return true
}

export const sortModelAllowlistItems = (
  items: ModelAllowlistItem[],
  sortBy: ModelAllowlistSortKey,
  modalitiesOf?: ModelModalitiesLookup,
): ModelAllowlistItem[] => {
  if (sortBy === 'default') {
    return items
  }
  const compareName = (a: ModelAllowlistItem, b: ModelAllowlistItem) =>
    a.id.localeCompare(b.id, undefined, { numeric: true, sensitivity: 'base' })
  const copy = [...items]
  if (sortBy === 'name') {
    copy.sort(compareName)
    return copy
  }
  if (sortBy === 'selected') {
    copy.sort((a, b) => Number(b.selected) - Number(a.selected) || compareName(a, b))
    return copy
  }
  // modality：按首个模态分组，组内按名称
  const primaryModality = (modelID: string): string =>
    modalitiesOf ? (modalitiesOf(modelID)[0] ?? '') : ''
  copy.sort(
    (a, b) =>
      primaryModality(a.id).localeCompare(primaryModality(b.id)) || compareName(a, b),
  )
  return copy
}

// filterModelAllowlistItems 返回当前视图中可见（并按需重排）的条目。
export const filterModelAllowlistItems = (
  state: ModelAllowlistState,
  filter: ModelAllowlistViewFilter,
  modalitiesOf?: ModelModalitiesLookup,
  sourcesOf?: ModelSourceAccountsLookup,
): ModelAllowlistItem[] =>
  sortModelAllowlistItems(
    state.items.filter(item => matchesModelAllowlistFilter(item, filter, modalitiesOf, sourcesOf)),
    filter.sortBy,
    modalitiesOf,
  )

// toggleModelAllowlistModality 切换某个模态筛选标签。
export const toggleModelAllowlistModality = (
  filter: ModelAllowlistViewFilter,
  modality: string,
) => {
  const index = filter.modalities.indexOf(modality)
  if (index >= 0) {
    filter.modalities.splice(index, 1)
  } else {
    filter.modalities.push(modality)
  }
}

// clearModelAllowlistSelection 取消选中；传入 visibleModelIDs 时只清可见条目。
export const clearModelAllowlistSelection = (
  state: ModelAllowlistState,
  visibleModelIDs?: Iterable<string>,
) => {
  const visible = visibleModelIDs ? new Set(visibleModelIDs) : null
  state.items.forEach(item => {
    if (!visible || visible.has(item.id)) {
      item.selected = false
    }
  })
}

// moveModelAllowlistItemByID 按条目 id 移动一步（delta = -1 上移 / +1 下移）。
//
// 视图可能被搜索、模态筛选或排序重排过，渲染序号不再等于 state.items 的下标，
// 因此上移/下移必须按 id 定位；移动成功后返回 true。
export const moveModelAllowlistItemByID = (
  state: ModelAllowlistState,
  modelID: string,
  delta: -1 | 1,
): boolean => {
  const fromIndex = state.items.findIndex(item => item.id === modelID)
  if (fromIndex < 0) {
    return false
  }
  const toIndex = fromIndex + delta
  if (toIndex < 0 || toIndex >= state.items.length) {
    return false
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
  return true
}

export const canMoveModelAllowlistItemByID = (
  state: ModelAllowlistState,
  modelID: string,
  delta: -1 | 1,
): boolean => {
  const fromIndex = state.items.findIndex(item => item.id === modelID)
  if (fromIndex < 0) {
    return false
  }
  const toIndex = fromIndex + delta
  return toIndex >= 0 && toIndex < state.items.length
}

export const invertModelAllowlistSelection = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = !item.selected
  })
}

export const moveModelAllowlistItem = (
  state: ModelAllowlistState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

// setModelAllowlistAutoEnabled 切换 auto 智能路由面板；strategy 缺省保留当前值。
export const setModelAllowlistAutoEnabled = (
  state: ModelAllowlistState,
  enabled: boolean,
  strategy?: ModelAllowlistAutoStrategy,
) => {
  state.autoEnabled = enabled
  if (strategy) {
    state.autoStrategy = strategy
  }
}

export const setModelAllowlistAutoStrategy = (
  state: ModelAllowlistState,
  strategy: ModelAllowlistAutoStrategy,
) => {
  state.autoStrategy = strategy
}

// addCustomModelAllowlistItem 把手工输入的条目追加到白名单末尾（选中状态）。
// 去重；`*` 可出现在任意位置。返回错误码或 null（成功）。
export const addCustomModelAllowlistItem = (
  state: ModelAllowlistState,
  raw: string,
): ModelAllowlistAddError | null => {
  const entry = raw.trim()
  if (!entry) {
    return 'empty'
  }
  if (
    state.items.some(item => item.id.toLowerCase() === entry.toLowerCase()) ||
    state.savedModels.some(model => model.toLowerCase() === entry.toLowerCase())
  ) {
    return 'duplicate'
  }
  state.items.push({ id: entry, selected: true })
  return null
}

// toggleModelAllowlistStateModality 切换**分组级**功能过滤的模态。
// 与 toggleModelAllowlistModality 刻意分开：那个只改列表显示（view filter），
// 这个会被写进分组配置、并同时影响模型列表与网关准入。
export const toggleModelAllowlistStateModality = (
  state: ModelAllowlistState,
  modality: string,
): string[] => {
  const key = modality.trim().toLowerCase()
  const next = state.modalities.includes(key)
    ? state.modalities.filter(item => item !== key)
    : [...state.modalities, key]
  state.modalities = normalizeModalities(next)
  return state.modalities
}

// modelAllowlistCapabilityActive 报告分组级功能过滤是否已配置。
export const modelAllowlistCapabilityActive = (state: ModelAllowlistState): boolean =>
  normalizeModalities(state.modalities).length > 0 || state.excludeThinkingVariants

export const buildModelAllowlistConfig = (
  state: ModelAllowlistState,
): ModelAllowlistConfig => {
  const models = state.items.length > 0
    ? state.items.filter(item => item.selected).map(item => item.id)
    : stripAutoSentinels(state.savedModels)
  return {
    enabled: state.enabled,
    models: state.autoEnabled
      ? [MODEL_ALLOWLIST_AUTO_SENTINELS[state.autoStrategy], ...models]
      : models,
    // R6：功能过滤始终提交，空数组即"不限制"（后端 normalize 会折叠为 nil）。
    modalities: normalizeModalities(state.modalities),
    exclude_thinking_variants: state.excludeThinkingVariants,
  }
}

export const selectedModelAllowlistCount = (state: ModelAllowlistState): number =>
  state.items.filter(item => item.selected).length

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    out.push(model)
  }
  return out
}

// normalizeModalities 归一化模态过滤键：小写、去空白、去重保序。
// 只接受后端 ModelModalities 里的标识，未知值丢弃（后端会 400，前端不该提交脏值）。
const normalizeModalities = (modalities: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of modalities) {
    const modality = raw.trim().toLowerCase()
    if (!modality || seen.has(modality)) {
      continue
    }
    if (!(modelAllowlistModalityOptions as readonly string[]).includes(modality)) {
      continue
    }
    seen.add(modality)
    out.push(modality)
  }
  return out
}
