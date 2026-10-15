export interface ModelAllowlistConfig {
  enabled: boolean
  models: string[]
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

export const selectAllModelAllowlistItems = (state: ModelAllowlistState) => {
  state.items.forEach(item => {
    item.selected = true
  })
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
