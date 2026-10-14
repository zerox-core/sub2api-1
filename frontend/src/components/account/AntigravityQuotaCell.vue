<template>
  <div v-if="visibleBuckets.length" class="mt-0.5 flex flex-wrap items-center gap-1" :title="tooltip">
    <span
      v-for="b in visibleBuckets"
      :key="b.key"
      class="inline-flex items-center rounded px-1.5 py-0.5 font-mono text-[10px] font-bold"
      :class="b.cls"
    >
      {{ b.label }} {{ b.pct }}
    </span>
    <button
      class="ml-0.5 text-[10px] leading-none text-gray-400 hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
      :disabled="refreshing"
      title="强制刷新配额"
      @click.stop="load(true)"
    >
      <svg class="h-2.5 w-2.5" :class="{ 'animate-spin': refreshing }" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getAntigravityQuota, type AntigravityQuotaSnapshot } from '@/api/admin/accounts'

const props = defineProps<{ accountId: number }>()

const snapshot = ref<AntigravityQuotaSnapshot | null>(null)
const refreshing = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

const visibleBuckets = computed(() => {
  const groups = snapshot.value?.groups ?? []
  const out: { key: string; label: string; pct: string; cls: string }[] = []
  for (const g of groups) {
    const gl = g.key === 'gemini' ? 'G' : 'C'
    for (const b of g.buckets) {
      if (b.remaining == null) continue
      const pctVal = Math.round(b.remaining * 100)
      const pct = `${pctVal}%`
      let cls = 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
      if (b.remaining <= 0.08) {
        cls = 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
      } else if (b.remaining <= 0.25) {
        cls = 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
      }
      out.push({ key: `${gl}-${b.window}`, label: gl + (b.window === 'weekly' ? '周' : '5h'), pct, cls })
    }
  }
  return out
})

const tooltip = computed(() => {
  const s = snapshot.value
  if (!s) return ''
  const lines: string[] = [`查询时间: ${s.fetched_at}`]
  for (const g of s.groups) {
    for (const b of g.buckets) {
      lines.push(`${g.display_name} · ${b.window === 'weekly' ? '周额度' : '5h窗口'} · 剩余 ${b.remaining == null ? '-' : Math.round(b.remaining * 100) + '%'} · 重置 ${b.reset_time || '-'}`)
    }
  }
  return lines.join('\n')
})

async function load(force = false) {
  refreshing.value = true
  try {
    snapshot.value = await getAntigravityQuota(props.accountId, force)
  } catch {
    // 静默失败：无数据就不渲染
  } finally {
    refreshing.value = false
  }
}

onMounted(() => {
  load()
  // 5 分钟轮询，保持额度新鲜
  timer = setInterval(() => load(), 5 * 60 * 1000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>
