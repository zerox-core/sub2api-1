<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.unsupportedModels.title')"
    width="wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <!-- 说明区：讲清这份清单是"被动学到的"，不是人工配置的白名单，
           以及它的自动过期语义，避免管理员以为需要长期维护。 -->
      <div
        v-if="account"
        class="rounded-xl border border-amber-200 bg-amber-50/60 px-3 py-2 dark:border-amber-900/40 dark:bg-amber-900/10"
      >
        <p class="flex items-center gap-2 text-sm font-medium text-amber-900 dark:text-amber-300">
          <Icon name="exclamationTriangle" size="sm" />
          {{ account.name }}
        </p>
        <p class="mt-1 text-xs leading-5 text-amber-800/80 dark:text-amber-300/70">
          {{ t('admin.accounts.unsupportedModels.hint') }}
        </p>
      </div>

      <p
        v-if="error"
        class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/40 dark:bg-red-900/20 dark:text-red-300"
      >
        {{ error }}
      </p>

      <div
        v-if="records.length === 0"
        class="rounded-lg border border-dashed border-gray-300 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
      >
        {{ t('admin.accounts.unsupportedModels.empty') }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between gap-3">
          <span class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.unsupportedModels.count', { count: records.length }) }}
          </span>
          <button
            type="button"
            class="rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
            :disabled="busy"
            @click="releaseAll"
          >
            {{ t('admin.accounts.unsupportedModels.releaseAll') }}
          </button>
        </div>

        <ul class="max-h-[50vh] space-y-2 overflow-y-auto">
          <li
            v-for="record in records"
            :key="record.model"
            class="flex items-start justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600"
          >
            <div class="min-w-0">
              <p class="truncate font-mono text-sm text-gray-900 dark:text-gray-100">
                {{ record.model }}
              </p>
              <p class="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ t('admin.accounts.unsupportedModels.status', { status: record.status || '—' }) }}</span>
                <span>·</span>
                <span>{{ t('admin.accounts.unsupportedModels.hits', { count: record.count || 0 }) }}</span>
                <span>·</span>
                <span>{{ t('admin.accounts.unsupportedModels.lastSeen', { time: formatTime(record.last_seen) }) }}</span>
              </p>
              <p
                v-if="record.reason"
                class="mt-0.5 truncate text-[11px] text-gray-400 dark:text-gray-500"
                :title="record.reason"
              >
                {{ record.reason }}
              </p>
            </div>
            <button
              type="button"
              class="shrink-0 rounded px-2 py-1 text-xs font-medium text-amber-700 transition-colors hover:bg-amber-50 disabled:opacity-50 dark:text-amber-300 dark:hover:bg-amber-900/20"
              :disabled="busy"
              @click="release(record.model)"
            >
              {{ t('admin.accounts.unsupportedModels.release') }}
            </button>
          </li>
        </ul>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { clearUnsupportedModels } from '@/api/admin/accounts'
import type { Account } from '@/types'

// R5：账号级「上游明确不支持」清单的管理界面。
// 清单本身由网关被动学习（连续 2 次明确不支持/404 才记账，7 天过期），
// 这里只提供人工放行入口：渠道补上模型后不必等 TTL 过期。
interface UnsupportedModelEntry {
  status?: number
  reason?: string
  count?: number
  first_seen?: string
  last_seen?: string
}

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; updated: [account: Account, meta: { model: string | null }] }>()

const { t } = useI18n()
const busy = ref(false)
const error = ref('')

const records = computed(() => {
  const raw = props.account?.extra?.unsupported_models
  if (!raw || typeof raw !== 'object') {
    return [] as Array<UnsupportedModelEntry & { model: string }>
  }
  return Object.entries(raw as Record<string, UnsupportedModelEntry>)
    .map(([model, entry]) => ({ model, ...(entry ?? {}) }))
    // 最近被拒的排前面：那才是当前最需要判断的一条。
    .sort((left, right) => (right.last_seen ?? '').localeCompare(left.last_seen ?? ''))
})

const formatTime = (value?: string): string => {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

watch(
  () => props.account?.id,
  () => {
    error.value = ''
    busy.value = false
  },
)

const release = async (model: string) => {
  if (!props.account || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const updated = await clearUnsupportedModels(props.account.id, model)
    emit('updated', updated, { model })
  } catch (err: any) {
    console.error('Failed to release unsupported model:', err)
    error.value = err?.message || t('admin.accounts.unsupportedModels.releaseFailed')
  } finally {
    busy.value = false
  }
}

const releaseAll = async () => {
  if (!props.account || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const updated = await clearUnsupportedModels(props.account.id)
    emit('updated', updated, { model: null })
  } catch (err: any) {
    console.error('Failed to release unsupported models:', err)
    error.value = err?.message || t('admin.accounts.unsupportedModels.releaseFailed')
  } finally {
    busy.value = false
  }
}

const handleClose = () => {
  error.value = ''
  emit('close')
}
</script>
