<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
            class="h-9 w-9 shrink-0 rounded-lg object-contain"
          />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="md" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img
          :src="siteLogo || '/logo.svg'"
          alt="Logo"
          class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain"
        />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 [overflow-wrap:anywhere] sm:px-6 dark:border-dark-800 dark:text-dark-400">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    class="relative flex min-h-screen flex-col overflow-hidden bg-[#07080C] text-zinc-100 selection:bg-primary-500/30 selection:text-primary-200"
  >
    <!-- 宇宙星空深色背景（纯白色微光点，呼吸微闪，杜绝蓝紫色） -->
    <CosmicBackground />

    <!-- Header -->
    <header class="relative z-20 border-b border-white/[0.07] bg-[#0A0C12]/80 backdrop-blur-xl px-6 py-3.5">
      <nav class="mx-auto flex max-w-7xl items-center justify-between">
        <!-- Z-Prism 现代极客折线几何 Logo -->
        <div class="flex items-center gap-3">
          <div class="relative flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-amber-500/25 to-orange-600/15 border border-primary-500/40 shadow-inner">
            <svg class="h-5 w-5 text-primary-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <div class="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-primary-400 animate-pulse"></div>
          </div>
          <div>
            <div class="flex items-center gap-2 font-mono">
              <span class="text-base font-extrabold tracking-tight text-white">sub2api</span>
              <span class="rounded border border-primary-500/40 bg-primary-500/10 px-1.5 py-0.5 text-[10px] font-sans font-semibold text-primary-300">
                ZeroX Core
              </span>
            </div>
            <span class="text-[10px] text-zinc-500 block font-mono">AI API Gateway Platform</span>
          </div>
        </div>

        <!-- 导航操作项 -->
        <div class="flex items-center gap-3">
          <LocaleSwitcher />

          <!-- 模型广场：默认首页也要给入口，否则开启了该功能却无处可达 -->
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300"
            :title="t('nav.modelPlaza')"
          >
            <Icon name="grid" size="sm" />
            <span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span>
          </router-link>

          <!-- 文档链接 -->
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300"
            title="查看官方开发文档"
          >
            <Icon name="book" size="sm" />
            <span class="hidden sm:inline">文档</span>
          </a>

          <!-- 免登录查 Key 用量 -->
          <router-link
            to="/key-usage"
            class="flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300"
            title="免登录查 Key 额度"
          >
            <Icon name="chart" size="sm" />
            <span class="hidden sm:inline">Key查询</span>
          </router-link>

          <!-- 登录 / 控制台按钮 -->
          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="inline-flex items-center gap-2 rounded-xl border border-primary-500/50 bg-gradient-to-r from-primary-500 to-amber-600 px-4 py-1.5 text-xs font-bold text-black shadow-md hover:brightness-110 transition-all"
          >
            <span>进入工作台</span>
            <svg class="h-3 w-3 text-black" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
            </svg>
          </router-link>
          <router-link
            v-else
            to="/login"
            class="inline-flex items-center gap-1.5 rounded-xl border border-primary-500/50 bg-gradient-to-r from-primary-500 to-amber-600 px-4 py-1.5 text-xs font-bold text-black shadow-md hover:brightness-110 transition-all"
          >
            <span>登录 / 注册</span>
          </router-link>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="relative z-10 flex-1 px-4 sm:px-6 py-12 md:py-16">
      <div class="mx-auto max-w-7xl">
        <!-- Hero Section: 左侧权威文案 + 右侧极速终端 -->
        <div class="mb-16 grid grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-8">
          <!-- Left: Hero 文案 -->
          <div class="lg:col-span-6 space-y-6 text-center lg:text-left">
            <div class="inline-flex items-center gap-2 rounded-full border border-primary-500/30 bg-primary-500/10 px-3.5 py-1 text-xs font-semibold text-primary-300">
              <span class="inline-block h-2 w-2 rounded-full bg-primary-400 animate-pulse"></span>
              亚太高速中转 · 毫秒级流式响应 · 纯净直连零掺水
            </div>

            <h1 class="text-4xl font-extrabold tracking-tight text-white sm:text-5xl lg:text-6xl leading-[1.15]">
              商业级全模态<br />
              <span class="bg-gradient-to-r from-primary-400 via-amber-300 to-orange-400 bg-clip-text text-transparent">
                AI API 网关平台
              </span>
            </h1>

            <p class="text-base sm:text-lg text-zinc-300 leading-relaxed max-w-xl mx-auto lg:mx-0">
              专为 Claude Code、Cursor、Windsurf 及企业级 Agent 打造。聚合 Anthropic Claude 3.7、DeepSeek R1 满血、OpenAI 及 Gemini 原生通道，提供智能负载均衡、Session 粘性保活与 Token 级精准计费。
            </p>

            <!-- 操作按钮组 -->
            <div class="flex flex-wrap items-center justify-center lg:justify-start gap-4 pt-2">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="inline-flex items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-primary-500 to-amber-600 px-7 py-3 text-sm font-bold text-black shadow-lg shadow-primary-500/25 hover:brightness-110 active:scale-95 transition-all"
              >
                <span>{{ isAuthenticated ? '进入用户控制台' : '免费开启接入' }}</span>
                <Icon name="arrowRight" size="sm" :stroke-width="2.5" />
              </router-link>

              <a
                href="#quickstart"
                class="inline-flex items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-6 py-3 text-sm font-medium text-zinc-200 hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300 transition-all"
              >
                <Icon name="terminal" size="sm" />
                <span>30秒快速上手</span>
              </a>
            </div>

            <!-- 信任标签 -->
            <div class="pt-2 flex flex-wrap items-center justify-center lg:justify-start gap-6 text-xs font-mono text-zinc-400">
              <div class="flex items-center gap-2">
                <span class="text-emerald-400">✓</span>
                <span>Claude 官方直连 (含 Extended Thinking)</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-emerald-400">✓</span>
                <span>支持 Prompt Caching 自动减免 90%</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-emerald-400">✓</span>
                <span>高防抗封锁与多节点备援</span>
              </div>
            </div>
          </div>

          <!-- Right: 30秒真实极速接入终端 -->
          <div id="quickstart" class="lg:col-span-6 scroll-mt-24">
            <QuickstartTerminal />
          </div>
        </div>

        <!-- 核心性能指标 Pill Bar -->
        <div class="mb-16 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div class="rounded-xl border border-white/[0.07] bg-[#0E1118]/80 p-4 text-center backdrop-blur-md">
            <div class="text-2xl font-black font-mono text-primary-400">99.98%</div>
            <div class="mt-1 text-xs text-zinc-400">企业级服务可用性 SLA</div>
          </div>
          <div class="rounded-xl border border-white/[0.07] bg-[#0E1118]/80 p-4 text-center backdrop-blur-md">
            <div class="text-2xl font-black font-mono text-emerald-400">24ms</div>
            <div class="mt-1 text-xs text-zinc-400">亚太边缘节点 P50 延迟</div>
          </div>
          <div class="rounded-xl border border-white/[0.07] bg-[#0E1118]/80 p-4 text-center backdrop-blur-md">
            <div class="text-2xl font-black font-mono text-amber-300">100%</div>
            <div class="mt-1 text-xs text-zinc-400">官方原版直通 · 零降质掺假</div>
          </div>
          <div class="rounded-xl border border-white/[0.07] bg-[#0E1118]/80 p-4 text-center backdrop-blur-md">
            <div class="text-2xl font-black font-mono text-primary-400">内部特惠</div>
            <div class="mt-1 text-xs text-zinc-400">注册即可解锁专属通道</div>
          </div>
        </div>

        <!-- 全厂商大模型专属服务卡片矩阵 (用户核心要求) -->
        <VendorCards />

        <!-- 平台核心保障与特性 -->
        <div class="mt-16 mb-12">
          <div class="text-center mb-10">
            <h3 class="text-2xl font-bold text-white sm:text-3xl">为什么选择 ZeroX sub2api 网关</h3>
            <p class="mt-2 text-sm text-zinc-400">专为长时编码、高并发任务与专业生产环境设计的架构底座</p>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div class="rounded-2xl border border-white/[0.08] bg-[#0E1118]/70 p-6 backdrop-blur-md">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-primary-500/10 border border-primary-500/30 text-primary-400">
                <Icon name="sync" size="lg" />
              </div>
              <h4 class="text-base font-bold text-white mb-2">多上游并发与智能故障自愈</h4>
              <p class="text-xs text-zinc-400 leading-relaxed">
                单模型支持绑定多个底层号池与服务商。当遇到 429 限流或 500 异常时，毫秒级无感热切备用节点，确保编码智能体不中断。
              </p>
            </div>

            <div class="rounded-2xl border border-white/[0.08] bg-[#0E1118]/70 p-6 backdrop-blur-md">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-400">
                <Icon name="link" size="lg" />
              </div>
              <h4 class="text-base font-bold text-white mb-2">Session 粘性保活与缓存优化</h4>
              <p class="text-xs text-zinc-400 leading-relaxed">
                针对同一编程工程与上下文请求保持节点粘性，完整激活 Claude / OpenAI 原生 Prompt Caching，大幅降低多轮交互延迟与费用。
              </p>
            </div>

            <div class="rounded-2xl border border-white/[0.08] bg-[#0E1118]/70 p-6 backdrop-blur-md">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-400">
                <Icon name="chart" size="lg" />
              </div>
              <h4 class="text-base font-bold text-white mb-2">Token 级透明计量与多端适配</h4>
              <p class="text-xs text-zinc-400 leading-relaxed">
                按上行 Prompt / 补全 Completion 独立费率精准核算，提供详细账单流水。无缝支持 OpenAI SDK、Claude Code、Cursor、Dify 等全生态客户端。
              </p>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Footer -->
    <footer class="relative z-10 border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
      <div
        class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left"
      >
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >
            {{ t('home.docs') }}
          </a>
          <a
            :href="githubUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >
            GitHub
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import VendorCards from '@/components/home/VendorCards.vue'
import QuickstartTerminal from '@/components/home/QuickstartTerminal.vue'
import CosmicBackground from '@/components/common/CosmicBackground.vue'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))

// GitHub URL
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const modelPlazaRequiresAuth = computed(
  () => appStore.cachedPublicSettings?.model_plaza_require_auth === true,
)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
/* Terminal Container */
.terminal-container {
  position: relative;
  display: inline-block;
}

/* Terminal Window */
.terminal-window {
  width: 420px;
  background: linear-gradient(145deg, #1e293b 0%, #0f172a 100%);
  border-radius: 14px;
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.4),
    0 0 0 1px rgba(255, 255, 255, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  overflow: hidden;
  transform: perspective(1000px) rotateX(2deg) rotateY(-2deg);
  transition: transform 0.3s ease;
}

.terminal-window:hover {
  transform: perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(-4px);
}

/* Terminal Header */
.terminal-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: rgba(30, 41, 59, 0.8);
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}
.btn-minimize {
  background: #eab308;
}
.btn-maximize {
  background: #22c55e;
}

.terminal-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #64748b;
  margin-right: 52px;
}

/* Terminal Body */
.terminal-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}
.line-2 {
  animation-delay: 1s;
}
.line-3 {
  animation-delay: 1.8s;
}
.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}
.code-cmd {
  color: #f59e0b;
}
.code-flag {
  color: #fbbf24;
}
.code-url {
  color: #fcd34d;
}
.code-comment {
  color: #64748b;
  font-style: italic;
}
.code-success {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.code-response {
  color: #fbbf24;
}

/* Blinking Cursor */
.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

/* Dark mode adjustments */
:deep(.dark) .terminal-window {
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.6),
    0 0 0 1px rgba(20, 184, 166, 0.2),
    0 0 40px rgba(20, 184, 166, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
</style>
