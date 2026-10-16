<template>
  <div class="space-y-6 text-zinc-100">
    <!-- 顶部价格计算规则看板 -->
    <div class="relative overflow-hidden rounded-2xl border border-primary-500/30 bg-gradient-to-r from-[#0E1118]/95 via-primary-500/[0.08] to-[#0E1118]/95 p-5 backdrop-blur-xl shadow-xl">
      <div class="absolute -right-20 -top-20 h-48 w-48 rounded-full bg-primary-500/10 blur-3xl"></div>
      <div class="relative z-10 flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div class="space-y-2">
          <div class="flex items-center gap-2">
            <span class="inline-flex items-center gap-1.5 rounded-full border border-primary-500/40 bg-primary-500/20 px-3 py-0.5 text-xs font-mono font-bold text-primary-300">
              <span class="h-1.5 w-1.5 rounded-full bg-primary-400 animate-pulse"></span> 价格计算规则
            </span>
            <span class="text-xs text-zinc-400 font-mono">数据来自后台真实分组配置</span>
          </div>
          <div class="flex flex-wrap items-baseline gap-2">
            <h2 class="text-lg font-bold text-white">付费价格 = 官方原价 × 分组倍率</h2>
          </div>
          <p class="text-xs text-zinc-300 max-w-3xl leading-relaxed"> 本页展示当前开放分组与真实模型清单，倍率列为本站默认计费倍率；管理员调整分组配置后本页自动同步默认倍率。综合价按近 7 天全站真实消费折算（含缓存），与账单同口径；暂无真实用量的按官方输入目录价展示，实际计费以本站扣费为准。 </p>
        </div>

        <!-- 快速跳转分组筛选 -->
        <div class="flex shrink-0 flex-wrap gap-2">
          <button
            v-for="p in platformTabs"
            :key="p.key"
            @click="selectedPlatform = p.key"
            class="rounded-xl border px-3.5 py-1.5 text-xs font-medium transition-all"
            :class="selectedPlatform === p.key
              ? 'border-primary-500 bg-primary-500/20 text-primary-300 font-bold shadow-sm'
              : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:border-white/20 hover:text-white'"
          >
            {{ p.label }}
          </button>
        </div>
      </div>
    </div>

    <!-- 核心计费与充值优势提示条 (1 RMB = 1 USD 政策声明) -->
    <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-xs text-emerald-300 backdrop-blur-md">
      <div class="flex items-center gap-2">
        <span class="flex h-2 w-2 rounded-full bg-emerald-400 animate-pulse"></span>
        <span class="font-bold text-white">💰 核心充值汇率规则：</span>
        <span>中转站账户余额虽以美元符号 ($) 计量，但实际充值兑换为 <strong>1 元人民币 (RMB) = 1 刀余额 ($ USD)</strong>！</span>
      </div>
      <span class="rounded bg-emerald-500/20 px-2 py-0.5 font-mono text-[11px] font-bold text-emerald-200">
        无外汇溢价 · 扣 $1.00 刀 = 实付 ¥1.00 元
      </span>
    </div>

    <!-- 顶部说明与搜索过滤条 -->
    <div class="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4 rounded-2xl border border-white/[0.08] bg-[#0E1118]/85 p-4 backdrop-blur-md">
      <div class="flex items-center gap-2">
        <div class="h-2 w-2 rounded-full bg-primary-400"></div>
        <span class="text-sm font-semibold text-white">模型广场与智能花费测算</span>
        <span class="text-xs text-zinc-400 hidden md:inline font-mono">（直接呈现每百万 Token 综合预计值，无需拆算输入输出）</span>
      </div>

      <div class="flex items-center gap-3">
        <!-- 搜索框 -->
        <div class="relative w-full sm:w-64">
          <input
            v-model="searchQuery"
            type="text"
            placeholder="搜索模型，如 gpt-6, claude-opus-5-5..."
            class="w-full rounded-xl border border-white/10 bg-black/40 px-3.5 py-2 text-xs text-white placeholder-zinc-500 focus:border-primary-500/50 focus:outline-none focus:ring-1 focus:ring-primary-500/50"
          />
          <button
            v-if="searchQuery"
            @click="searchQuery = ''"
            class="absolute right-2.5 top-2 text-xs text-zinc-400 hover:text-white"
          >
            ✕
          </button>
        </div>
      </div>
    </div>

    <!-- 加载中状态 -->
    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500/25 border-t-primary-500"></div>
    </div>

    <!-- 各分组数据卡片 -->
    <div v-else class="space-y-6">
      <div
        v-for="group in filteredGroups"
        :key="group.id"
        class="overflow-hidden rounded-2xl border border-white/[0.08] bg-[#0E1118]/90 shadow-2xl backdrop-blur-xl"
        :class="{ 'border-primary-500/40 ring-1 ring-primary-500/20': group.isWelfare }"
      >
        <!-- 分组头部 + 实时倍率测算控制器 -->
        <header class="border-b border-white/[0.08] bg-white/[0.02] p-5">
          <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
            <!-- 分组标题信息 -->
            <div class="space-y-1.5">
              <div class="flex items-center gap-2.5 flex-wrap">
                <span class="text-base font-bold text-white">{{ group.name }}</span>
                <span
                  class="rounded border px-2 py-0.5 text-xs font-mono font-semibold"
                  :class="group.isWelfare ? 'border-primary-500/50 bg-primary-500/20 text-primary-300' : 'border-white/10 bg-white/[0.05] text-zinc-300'"
                >
                  {{ group.platformLabel }}
                </span>
                <span
                  v-if="group.isWelfare"
                  class="rounded border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-[11px] font-bold text-amber-300 animate-pulse"
                >
                  🔥 倍率低至 0.03
                </span>
              </div>
              <p class="text-xs text-zinc-400">
                {{ group.description }}
              </p>
            </div>

            <!-- 倍率调节与手动刷新重算控制器 (用户核心要求) -->
            <div class="flex flex-wrap items-center gap-3 rounded-xl border border-white/10 bg-black/40 p-2.5">
              <span class="text-xs text-zinc-300 font-medium">当前测算倍率:</span>

              <!-- 倍率数值调节框 -->
              <div class="flex items-center gap-1.5">
                <input
                  type="number"
                  step="0.01"
                  min="0.001"
                  max="5.0"
                  v-model.number="group.currentRate"
                  class="w-20 rounded-lg border border-primary-500/40 bg-[#07080C] px-2 py-1 text-center font-mono text-xs font-bold text-primary-300 focus:outline-none focus:ring-1 focus:ring-primary-500"
                />
                <span class="text-xs text-zinc-400 font-mono">x</span>
              </div>

              <!-- 快捷倍率切换按钮 -->
              <div class="flex items-center gap-1">
                <button
                  v-for="r in [0.03, 0.2, 0.4, 0.7, 0.85]"
                  :key="r"
                  @click="group.currentRate = r; handleRecalculate(group)"
                  class="rounded px-1.5 py-0.5 text-[10px] font-mono transition-colors"
                  :class="group.currentRate === r ? 'bg-primary-500 text-black font-bold' : 'bg-white/5 text-zinc-400 hover:text-white'"
                >
                  {{ r }}x
                </button>
              </div>

              <!-- 手动刷新计算按钮 -->
              <button
                @click="handleRecalculate(group)"
                class="flex items-center gap-1.5 rounded-lg bg-gradient-to-r from-primary-500 to-amber-600 px-3 py-1 text-xs font-bold text-black shadow hover:brightness-110 active:scale-95 transition-all"
                title="根据当前填写的倍率手动刷新计算预计值"
              >
                <svg
                  class="h-3.5 w-3.5 transition-transform"
                  :class="{ 'animate-spin': isRecalculating === group.id }"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                <span>刷新重算</span>
              </button>
            </div>
          </div>
        </header>

        <!-- 表格：直接呈现每百万 Token 预计花费（不要把输入输出缓存分开算），图片按张列出 -->
        <div class="overflow-x-auto">
          <table class="w-full min-w-[760px] table-auto border-collapse text-left text-xs tabular-nums">
            <thead>
              <tr class="border-b border-white/[0.08] bg-white/[0.02] text-zinc-400 font-semibold uppercase tracking-wider">
                <th class="py-3 px-5">模型名称与特征</th>
                <th class="py-3 px-4">模型类别</th>
                <th class="py-3 px-4">官方基准原价</th>
                <th class="py-3 px-4 bg-primary-500/[0.06] text-primary-300 font-bold border-x border-primary-500/20">
                  <div class="flex items-center gap-1">
                    <span>折后预计花费 (根据 {{ group.currentRate }}x 倍率)</span>
                    <span class="rounded bg-primary-500/20 px-1 py-0.2 text-[9px]">直观总花费</span>
                  </div>
                </th>
                <th class="py-3 px-4">计费维度与说明</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-white/[0.05]">
              <tr
                v-for="model in group.models"
                :key="model.name"
                class="transition-colors hover:bg-white/[0.02]"
              >
                <!-- 模型名称 -->
                <td class="py-3.5 px-5">
                  <div class="space-y-0.5">
                    <div class="flex items-center gap-2">
                      <span class="font-bold text-white text-sm font-mono">{{ model.name }}</span>
                      <span
                        v-if="model.name.includes('gpt6asutr') || model.name.includes('opus5.5') || model.name.includes('3.8f')"
                        class="rounded border border-primary-500/50 bg-primary-500/20 px-1.5 py-0.2 text-[10px] font-mono font-bold text-primary-300"
                      >
                        御三家焦点
                      </span>
                    </div>
                    <p class="text-[11px] text-zinc-400">{{ model.desc }}</p>
                  </div>
                </td>

                <!-- 模型类别 -->
                <td class="py-3.5 px-4">
                  <span
                    class="inline-flex items-center rounded-md px-2 py-0.5 text-[11px] font-medium"
                    :class="{
                      'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20': model.type === 'llm',
                      'bg-amber-500/10 text-amber-300 border border-amber-500/20': model.type === 'image',
                      'bg-zinc-500/10 text-zinc-300 border border-zinc-500/20': model.type === 'embedding'
                    }"
                  >
                    {{ model.type === 'llm' ? '大语言模型' : (model.type === 'image' ? '🎨 图片生成' : '文本向量') }}
                  </span>
                </td>

                <!-- 官方基准原价 -->
                <td class="py-3.5 px-4 font-mono text-zinc-400">
                  <template v-if="model.type === 'image'">
                    <span class="font-medium text-zinc-300">${{ model.officialImageUnitPrice?.toFixed(3) }}</span> / 张
                  </template>
                  <template v-else>
                    <span class="font-medium text-zinc-300">${{ model.officialBaseCost1M?.toFixed(2) }}</span> / 1M Token
                  </template>
                </td>

                <!-- 核心列：折后预计花费值（不把输入输出分开算，图片模型只列单张价格） -->
                <td class="py-3.5 px-4 bg-primary-500/[0.04] border-x border-primary-500/15">
                  <template v-if="model.type === 'image'">
                    <!-- 图片生成模型：不计算 token，只列单张价格 (用户核心指令) -->
                    <div class="space-y-0.5">
                      <div class="font-mono text-sm font-extrabold text-amber-300">
                        ${{ (model.officialImageUnitPrice || 0.04).toFixed(3) }} <span class="text-xs font-normal text-zinc-400">/ 张</span>
                      </div>
                      <div class="text-[10px] text-emerald-400/90 font-mono">
                        实付 ¥{{ (model.officialImageUnitPrice || 0.04).toFixed(3) }} 元 / 张 (1RMB=1USD)
                      </div>
                    </div>
                  </template>

                  <template v-else>
                    <!-- 大语言模型：根据当前倍率自动刷新计算的每百万 Token 预计花费 -->
                    <div class="space-y-0.5">
                      <div class="font-mono text-sm font-extrabold text-primary-300 flex items-center gap-1.5">
                        <span>${{ calculateCost1M(model.officialBaseCost1M || 0, group.currentRate) }}</span>
                        <span class="text-xs font-normal text-zinc-400">/ 1M Token</span>
                      </div>
                      <div class="text-[10px] text-emerald-400/90 font-mono flex items-center gap-1">
                        <span>实付 ¥{{ calculateCost1M(model.officialBaseCost1M || 0, group.currentRate) }} 元 / 百万 Token</span>
                        <span class="text-zinc-500 text-[9px]">(1RMB=1USD)</span>
                      </div>
                    </div>
                  </template>
                </td>

                <!-- 计费维度与说明 -->
                <td class="py-3.5 px-4 text-[11px] text-zinc-400">
                  <span v-if="model.type === 'image'" class="text-amber-400/90 font-medium">
                    单张固定计费，无输入/输出概念
                  </span>
                  <span v-else>
                    综合每百万 Token 花费 (倍率 {{ group.currentRate }}x)
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive } from 'vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'

defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  embedded?: boolean
}>()

const selectedPlatform = ref('all')
const searchQuery = ref('')
const isRecalculating = ref<string | null>(null)

const platformTabs = [
  { key: 'all', label: '全部支持分组' },
  { key: 'gpt', label: 'OpenAI GPT / Codex' },
  { key: 'claude', label: 'Anthropic Claude' },
  { key: 'gemini', label: 'Google Gemini' },
  { key: 'guomo', label: '国产模型渠道' }
]

interface ModelItem {
  name: string
  type: 'llm' | 'image' | 'embedding'
  desc: string
  officialBaseCost1M?: number
  officialImageUnitPrice?: number
}

interface GroupItem {
  id: string
  name: string
  platform: string
  platformLabel: string
  description: string
  defaultRate: number
  currentRate: number
  isWelfare?: boolean
  models: ModelItem[]
}

// 分组与模型清单来自后台生成的 /plaza-data.json（管理员调整分组配置后自动同步）
const presetGroups = reactive<GroupItem[]>([])
fetch(`/plaza-data.json?t=${Date.now()}`)
  .then((res) => res.json())
  .then((data) => {
    if (Array.isArray(data?.groups)) {
      presetGroups.push(...(data.groups as GroupItem[]))
    }
  })
  .catch(() => {
    // 静默失败：拿不到数据时宁可为空，也不展示演示数据
  })

// 计算每百万 Token 预计花费（不分开算输入输出缓存，直接综合折算单价）
function calculateCost1M(baseCost1M: number, rateMultiplier: number): string {
  const result = baseCost1M * rateMultiplier
  if (result < 0.001) {
    return result.toFixed(4)
  } else if (result < 0.01) {
    return result.toFixed(4)
  } else {
    return result.toFixed(3)
  }
}

// 手动刷新重算动画反馈
function handleRecalculate(group: GroupItem) {
  isRecalculating.value = group.id
  setTimeout(() => {
    isRecalculating.value = null
  }, 400)
}

// 筛选与搜索
const filteredGroups = computed(() => {
  let list = presetGroups

  if (selectedPlatform.value !== 'all') {
    list = list.filter((g) => g.platform === selectedPlatform.value)
  }

  const query = searchQuery.value.trim().toLowerCase()
  if (!query) {
    return list
  }

  return list
    .map((g) => ({
      ...g,
      models: g.models.filter(
        (m) => m.name.toLowerCase().includes(query) || m.desc.toLowerCase().includes(query)
      )
    }))
    .filter((g) => g.models.length > 0)
})
</script>
