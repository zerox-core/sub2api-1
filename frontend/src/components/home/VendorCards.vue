<template>
  <div class="relative w-full py-6">
    <!-- 模块标题区 -->
    <div class="mb-10 text-center">
      <div class="inline-flex items-center gap-2 rounded-full border border-primary-500/30 bg-primary-500/10 px-3.5 py-1 text-xs font-semibold uppercase tracking-wider text-primary-400">
        <span class="inline-block h-1.5 w-1.5 rounded-full bg-primary-400 animate-pulse"></span>
        Multi-Provider Gateway Matrix
      </div>
      <h2 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-4xl">
        核心支持分组与 <span class="bg-gradient-to-r from-primary-400 to-amber-300 bg-clip-text text-transparent">全厂商通道</span>
      </h2>
      <p class="mx-auto mt-3 max-w-2xl text-sm text-zinc-400 sm:text-base">
        零掺假官方直连通道 · 毫秒级极速响应 · 注册即享专属算力池 · 故障无感热切
      </p>
    </div>

    <!-- 分组与厂商卡片网格 (御三家 + 限时福利分组) -->
    <div class="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-2">
      <div
        v-for="v in vendors"
        :key="v.id"
        class="group relative flex flex-col justify-between overflow-hidden rounded-2xl border border-white/[0.08] bg-[#0E1118]/90 p-6 backdrop-blur-xl transition-all duration-300 hover:-translate-y-1 hover:border-primary-500/40 hover:shadow-card-hover hover:shadow-primary-500/10"
        :class="{ 'border-primary-500/30 bg-gradient-to-b from-primary-500/[0.08] to-[#0E1118]/90': v.isWelfare }"
      >
        <!-- 顶部微质感发丝线 -->
        <div class="absolute inset-x-0 top-0 h-[2px] bg-gradient-to-r from-transparent via-primary-500/40 to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100"></div>

        <div>
          <!-- 卡片头部：图标 + 厂商名 + 状态探针 -->
          <div class="flex items-start justify-between gap-4">
            <div class="flex items-center gap-3.5">
              <!-- 立体微徽章 -->
              <div
                class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl border border-white/10 shadow-inner transition-transform duration-300 group-hover:scale-105"
                :style="{ background: v.iconBg }"
              >
                <!-- SVG 图标 -->
                <div v-html="v.iconSvg" class="h-6 w-6"></div>
              </div>
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="text-base font-bold text-white group-hover:text-primary-300 transition-colors">
                    {{ v.name }}
                  </h3>
                  <span
                    class="rounded px-1.5 py-0.5 text-[10px] font-mono"
                    :class="v.isWelfare ? 'border border-primary-500/40 bg-primary-500/20 text-primary-300 font-bold' : 'bg-white/[0.06] text-zinc-400'"
                  >
                    {{ v.tag }}
                  </span>
                </div>
                <div class="mt-0.5 text-xs text-zinc-400">
                  {{ v.desc }}
                </div>
              </div>
            </div>

            <!-- 测速健康灯 -->
            <div class="flex shrink-0 items-center gap-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[11px] font-mono font-medium text-emerald-400">
              <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
              {{ v.ping }}
            </div>
          </div>

          <!-- 特性标签（不外露普通倍率，等注册后在控制台查看） -->
          <div class="mt-4 flex flex-wrap items-center gap-2">
            <span
              v-if="v.highlightBadge"
              class="inline-flex items-center rounded-lg border border-primary-500/30 bg-primary-500/10 px-2.5 py-1 text-xs font-semibold text-primary-300"
            >
              🔥 {{ v.highlightBadge }}
            </span>
            <span
              v-for="feature in v.features"
              :key="feature"
              class="rounded-lg border border-white/[0.05] bg-white/[0.03] px-2 py-1 text-[11px] text-zinc-300"
            >
              {{ feature }}
            </span>
          </div>

          <!-- 主力代表模型列表 -->
          <div class="mt-4 space-y-1.5">
            <div class="text-[11px] font-medium uppercase tracking-wider text-zinc-500">
              代表主力模型
            </div>
            <div class="flex flex-wrap gap-1.5">
              <span
                v-for="model in v.models"
                :key="model"
                class="rounded bg-black/40 px-2 py-0.5 font-mono text-xs text-zinc-300 border border-white/[0.04]"
                :class="{ 'text-primary-300 border-primary-500/30 font-bold': model.includes('gpt6asutr') || model.includes('opus5.5') || model.includes('3.8f') }"
              >
                {{ model }}
              </span>
            </div>
          </div>
        </div>

        <!-- 卡片底部：SLA保障与配置模板复制 -->
        <div class="mt-6 flex items-center justify-between border-t border-white/[0.06] pt-4">
          <div class="text-[11px] text-zinc-400 font-mono">
            {{ v.sla }}
          </div>
          <button
            @click="copyEndpoint(v)"
            class="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-all hover:border-primary-500/50 hover:bg-primary-500/10 hover:text-primary-300"
          >
            <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
            {{ copiedId === v.id ? '已复制接入端点' : '一键接入模板' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const copiedId = ref<string | null>(null)

interface VendorItem {
  id: string
  name: string
  tag: string
  desc: string
  iconBg: string
  iconSvg: string
  ping: string
  highlightBadge?: string
  features: string[]
  models: string[]
  sla: string
  snippet: string
  isWelfare?: boolean
}

const vendors: VendorItem[] = [
  {
    id: 'openai',
    name: 'OpenAI 官方旗舰',
    tag: '御三家旗舰 · 原生直连',
    desc: '主力支持 gpt-6.1-sol 及全系列 Codex 高可用通道',
    iconBg: 'linear-gradient(135deg, #10B981 0%, #081C15 100%)',
    iconSvg: `<svg viewBox="0 0 24 24" fill="white"><path d="M22.28 9.94a5.95 5.95 0 0 0-.52-4.91 6.05 6.05 0 0 0-6.52-2.8A6.06 6.06 0 0 0 10.7.35a6.05 6.05 0 0 0-5.78 4.2 6.04 6.04 0 0 0-3.9 2.84 6.05 6.05 0 0 0 .74 7.07 5.96 5.96 0 0 0 .52 4.9 6.05 6.05 0 0 0 6.52 2.82 6.04 6.04 0 0 0 4.54 1.88 6.05 6.05 0 0 0 5.78-4.2 6.04 6.04 0 0 0 3.9-2.84 6.05 6.05 0 0 0-.74-7.08z"/></svg>`,
    ping: '28ms 专线直达',
    highlightBadge: '支持 Function Call & 结构化输出',
    features: ['JSON Schema 严格约束', '流式无缓冲打字', 'Session 粘性保持'],
    models: ['openAI--gpt6asutr', 'o3-mini', 'gpt-4o', 'text-embedding-3-large'],
    sla: 'SLA 99.99% · 自动 Failover',
    snippet: `export OPENAI_BASE_URL="https://api.zeroxcore.tech/v1"\nexport OPENAI_API_KEY="sk-sub2api-your-key"`
  },
  {
    id: 'anthropic',
    name: 'Anthropic Claude',
    tag: '御三家旗舰 · 官方源',
    desc: '主力支持 claude-opus-5-5，原生思考协议与 Prompt Caching',
    iconBg: 'linear-gradient(135deg, #CC785C 0%, #1F1916 100%)',
    iconSvg: `<svg viewBox="0 0 24 24" fill="white"><path d="M17.47 3.03H13.8L8.68 15.65h3.66l1.09-2.73h4.37l1.07 2.73h3.76L17.47 3.03zm-2.81 7.21l1.1-2.83 1.12 2.83h-2.22zM6.53 3.03H2.87L8 15.65h3.66L6.53 3.03z"/></svg>`,
    ping: '24ms 极速响应',
    highlightBadge: 'Extended Thinking 原生思考',
    features: ['缓存减免高达 90%', 'Session 粘性保活', '企业级零封号隔离'],
    models: ['claude-opus5.5', 'claude-sonnet-5', 'claude-haiku-4.5'],
    sla: 'SLA 99.98% · 零封号隔离',
    snippet: `export ANTHROPIC_BASE_URL="https://api.zeroxcore.tech"\nexport ANTHROPIC_API_KEY="sk-sub2api-your-key"`
  },
  {
    id: 'google',
    name: 'Google Gemini',
    tag: '御三家旗舰 · 多模态',
    desc: '主力支持 gemini-3.8-flash-high，超大上下文窗口全速支持',
    iconBg: 'linear-gradient(135deg, #F59E0B 0%, #261605 100%)',
    iconSvg: `<svg viewBox="0 0 24 24" fill="white"><path d="M12 24c6.627 0 12-5.373 12-12S18.627 0 12 0 0 5.373 0 12s5.373 12 12 12zm0-4.5a7.5 7.5 0 1 1 0-15 7.5 7.5 0 0 1 0 15z"/></svg>`,
    ping: '32ms 亚太直连',
    highlightBadge: '200 万 Token 超大上下文',
    features: ['全模态音视频支持', '极速并发低等待', '纯净企业级网关'],
    models: ['google-3.8f', 'gemini-2.5-flash', 'gemini-2.5-pro'],
    sla: 'SLA 99.9% · 纯净企业级',
    snippet: `export GEMINI_API_BASE="https://api.zeroxcore.tech/v1"\nexport GEMINI_API_KEY="sk-sub2api-your-key"`
  },
  {
    id: 'welfare',
    name: '现时福利分组',
    tag: '全网超值算力专区',
    desc: '聚合国内外顶级大模型集群，限时特惠，倍率低至 0.03',
    iconBg: 'linear-gradient(135deg, #F59E0B 0%, #78350F 100%)',
    iconSvg: `<svg viewBox="0 0 24 24" fill="white"><path d="M12 2L1 21h22L12 2zm0 4.5l7.5 13H4.5L12 6.5z"/></svg>`,
    ping: '15ms 智能优选',
    highlightBadge: '倍率低至 0.03 极具性价比',
    features: ['含国内外主流大模型', '自研智算集群专线', '极速代码与推理加速'],
    models: ['国内外前沿多模态大模型', '满血深度思考模型', '高性能代码智算大模型'],
    sla: 'SLA 99.95% · 注册即刻选用',
    snippet: `export OPENAI_BASE_URL="https://api.zeroxcore.tech/v1"\nexport OPENAI_API_KEY="sk-sub2api-your-key"`,
    isWelfare: true
  }
]

function copyEndpoint(v: VendorItem) {
  navigator.clipboard.writeText(v.snippet)
  copiedId.value = v.id
  setTimeout(() => {
    if (copiedId.value === v.id) copiedId.value = null
  }, 2500)
}
</script>
