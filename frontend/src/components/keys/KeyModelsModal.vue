<template>
  <BaseDialog :show="show" title="Key 可用模型" width="extra-wide" @close="handleClose">
    <div v-if="apiKey" class="space-y-4">
      <!-- Key 信息 + 刷新 -->
      <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 px-4 py-2.5 text-sm dark:bg-dark-700">
        <span class="font-medium text-gray-900 dark:text-white">{{ apiKey.name }}</span>
        <span class="text-gray-400">|</span>
        <span class="text-gray-600 dark:text-gray-400">{{ apiKey.group?.name || '默认分组' }}</span>
        <span class="text-gray-400">|</span>
        <span class="text-gray-600 dark:text-gray-400">
          共 <span class="font-semibold text-primary-600 dark:text-primary-400">{{ filteredModels.length }}</span> / {{ allModels.length }} 个模型
        </span>
        <button
          type="button"
          class="ml-auto inline-flex items-center gap-1 rounded-lg border border-gray-200 bg-white px-2.5 py-1 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-500 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
          :disabled="loading"
          @click="loadModels"
        >
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </div>

      <!-- 用户侧过滤筛选工具条：模型分类（大语言模型、视觉模型、生图、语音） + 搜索框（无任何上游账号信息泄漏） -->
      <div class="space-y-2 rounded-lg border border-gray-200 bg-gray-50/50 p-3 dark:border-dark-600 dark:bg-dark-800/40">
        <!-- 搜索框 -->
        <div class="relative w-full">
          <input
            v-model="searchQuery"
            type="search"
            placeholder="搜索模型名称（如 qwen、gpt、claude、sambert）..."
            class="w-full rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-700 placeholder-gray-400 focus:border-primary-500 focus:outline-none dark:border-dark-500 dark:bg-dark-700 dark:text-gray-200"
          />
        </div>

        <!-- 纯模型类型分类按钮组（无账号来源，保护隐私） -->
        <div class="flex flex-wrap items-center gap-1.5 pt-1">
          <span class="mr-1 text-xs font-medium text-gray-500 dark:text-gray-400">类型筛选:</span>
          
          <button
            type="button"
            class="rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === '' 
              ? 'bg-primary-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = ''"
          >
            全部 ({{ allModels.length }})
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === 'text' 
              ? 'bg-indigo-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = selectedType === 'text' ? '' : 'text'"
          >
            <span>🧠 大语言模型</span>
            <span class="text-[10px] opacity-80">({{ typeCounts.text || 0 }})</span>
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === 'vision' 
              ? 'bg-teal-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = selectedType === 'vision' ? '' : 'vision'"
          >
            <span>👁️ 视觉模型</span>
            <span class="text-[10px] opacity-80">({{ typeCounts.vision || 0 }})</span>
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === 'image' 
              ? 'bg-emerald-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = selectedType === 'image' ? '' : 'image'"
          >
            <span>🎨 生图模型</span>
            <span class="text-[10px] opacity-80">({{ typeCounts.image || 0 }})</span>
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === 'audio' 
              ? 'bg-amber-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = selectedType === 'audio' ? '' : 'audio'"
          >
            <span>🎙️ 语音模型</span>
            <span class="text-[10px] opacity-80">({{ typeCounts.audio || 0 }})</span>
          </button>

          <button
            v-if="typeCounts.video"
            type="button"
            class="inline-flex items-center gap-1 rounded px-2.5 py-1 text-xs font-medium transition-colors"
            :class="selectedType === 'video' 
              ? 'bg-pink-600 text-white shadow-sm' 
              : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:border-dark-500 dark:hover:bg-dark-600'"
            @click="selectedType = selectedType === 'video' ? '' : 'video'"
          >
            <span>🎬 视频生成</span>
            <span class="text-[10px] opacity-80">({{ typeCounts.video || 0 }})</span>
          </button>
        </div>
      </div>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex justify-center py-12">
        <svg class="h-7 w-7 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <!-- 错误提示 -->
      <div v-else-if="loadError" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
        {{ loadError }}
      </div>

      <!-- 空列表 -->
      <div v-else-if="filteredModels.length === 0" class="py-12 text-center text-sm text-gray-400 dark:text-gray-500">
        {{ allModels.length === 0 ? '该 Key 当前无可用模型' : '未找到匹配的模型' }}
      </div>

      <!-- 模型条目列表（纯用户侧视图：无账号标识，点击一键复制） -->
      <div v-else class="max-h-[28rem] space-y-1.5 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
        <div
          v-for="item in filteredModels"
          :key="item.id"
          class="group flex items-center justify-between gap-3 rounded-lg border border-gray-100 bg-white px-3 py-2.5 transition-colors hover:border-primary-200 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-800 dark:hover:bg-dark-700/60"
        >
          <!-- 模型 ID 与类型徽章 -->
          <div class="flex flex-wrap items-center gap-2 min-w-0 flex-1">
            <span class="font-mono text-xs font-semibold text-gray-800 dark:text-gray-200 break-all select-all">
              {{ item.id }}
            </span>

            <span
              v-if="item.type === 'image'"
              class="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300"
            >
              生图
            </span>
            <span
              v-else-if="item.type === 'vision'"
              class="rounded bg-teal-100 px-1.5 py-0.5 text-[10px] font-medium text-teal-800 dark:bg-teal-900/40 dark:text-teal-300"
            >
              视觉
            </span>
            <span
              v-else-if="item.type === 'audio'"
              class="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-300"
            >
              语音
            </span>
            <span
              v-else-if="item.type === 'video'"
              class="rounded bg-pink-100 px-1.5 py-0.5 text-[10px] font-medium text-pink-800 dark:bg-pink-900/40 dark:text-pink-300"
            >
              视频
            </span>
            <span
              v-else
              class="rounded bg-indigo-100 px-1.5 py-0.5 text-[10px] font-medium text-indigo-800 dark:bg-indigo-900/40 dark:text-indigo-300"
            >
              大语言模型
            </span>
          </div>

          <!-- 右侧复制按键 -->
          <button
            type="button"
            class="shrink-0 rounded-md border border-gray-200 bg-white px-2.5 py-1 text-xs font-medium text-gray-600 shadow-sm transition-colors hover:bg-gray-50 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600 dark:hover:text-primary-400"
            @click="copyModel(item.id)"
          >
            <span v-if="copiedModel === item.id" class="text-green-600 dark:text-green-400 font-semibold">
              已复制 ✓
            </span>
            <span v-else class="inline-flex items-center gap-1">
              <Icon name="copy" size="xs" />
              复制
            </span>
          </button>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { classifyModelType, type ModelDisplayType } from '@/utils/modelType'

interface KeyModelItem {
  id: string
  type: ModelDisplayType
}

const props = defineProps<{
  show: boolean
  apiKey: ApiKey | null
}>()

const emit = defineEmits<{
  close: []
}>()

const appStore = useAppStore()

const loading = ref(false)
const loadError = ref('')
const allModels = ref<KeyModelItem[]>([])
const searchQuery = ref('')
const selectedType = ref<string>('')
const copiedModel = ref('')
let copiedTimer: ReturnType<typeof setTimeout> | null = null

// 分类逻辑统一走 @/utils/modelType，与分组「可用模型」弹窗完全一致（禁止本地复制实现）
const loadModels = async () => {
  if (!props.apiKey || !props.apiKey.key) return
  loading.value = true
  loadError.value = ''
  try {
    const resp = await fetch('/v1/models', {
      headers: { Authorization: 'Bearer ' + props.apiKey.key },
    })
    if (!resp.ok) {
      let msg = 'HTTP ' + resp.status
      try {
        const errBody = await resp.json()
        if (errBody && errBody.error && errBody.error.message) msg = errBody.error.message
        else if (errBody && errBody.message) msg = errBody.message
      } catch { /* ignore */ }
      throw new Error(msg)
    }
    const data = await resp.json()
    const rawList: string[] = (data.data || []).map((m: { id: string }) => m.id).filter(Boolean)
    const items: KeyModelItem[] = rawList.map((id) => ({
      id,
      type: classifyModelType(id),
    }))
    items.sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true, sensitivity: 'base' }))
    allModels.value = items
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : '模型列表加载失败'
    allModels.value = []
  } finally {
    loading.value = false
  }
}

// 统计各类型数量
const typeCounts = computed(() => {
  const counts: Record<string, number> = { text: 0, vision: 0, image: 0, audio: 0, video: 0 }
  for (const m of allModels.value) {
    counts[m.type] = (counts[m.type] || 0) + 1
  }
  return counts
})

// 筛选后的模型列表
const filteredModels = computed(() => {
  let list = allModels.value
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    list = list.filter((item) => item.id.toLowerCase().includes(q))
  }
  if (selectedType.value) {
    list = list.filter((item) => item.type === selectedType.value)
  }
  return list
})

const copyModel = async (model: string) => {
  try {
    await navigator.clipboard.writeText(model)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = model
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
  }
  copiedModel.value = model
  appStore.showToast('success', `已复制模型 ID：${model}`, 2000)
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copiedModel.value = ''
  }, 2000)
}

const handleClose = () => {
  emit('close')
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      searchQuery.value = ''
      selectedType.value = ''
      loadModels()
    }
  },
)
</script>
