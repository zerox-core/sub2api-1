<template>
  <header
    class="sticky top-0 z-30 border-b border-white/[0.08] bg-[#0A0C12]/80 backdrop-blur-xl"
  >
    <div class="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-3.5 sm:px-6">
      <!-- 左:站点折线几何 logo + 名称 -->
      <router-link to="/" class="flex min-w-0 items-center gap-3">
        <div class="relative flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-amber-500/25 to-orange-600/15 border border-primary-500/40 shadow-inner">
          <svg class="h-4.5 w-4.5 text-primary-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
          <div class="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-primary-400 animate-pulse"></div>
        </div>
        <div class="flex items-center gap-2">
          <span class="truncate text-base font-bold text-white tracking-tight">
            {{ siteName }}
          </span>
          <span class="rounded border border-primary-500/40 bg-primary-500/10 px-1.5 py-0.5 text-[10px] font-mono font-semibold text-primary-300">
            ZeroX Core
          </span>
        </div>
      </router-link>

      <!-- 右:登录 / 回到后台 -->
      <div class="flex items-center gap-3">
        <RouterLink
          to="/"
          class="hidden sm:inline-flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-1.5 text-xs text-zinc-300 transition-colors hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300"
        >
          返回首页
        </RouterLink>

        <RouterLink
          v-if="isAuthenticated"
          :to="backTarget"
          class="inline-flex flex-shrink-0 items-center justify-center gap-1.5 rounded-xl border border-primary-500/50 bg-gradient-to-r from-primary-500 to-amber-600 px-4 py-1.5 text-xs font-bold text-black shadow-md hover:brightness-110 active:scale-95 transition-all"
        >
          {{ t('modelPlaza.nav.backToDashboard') }}
        </RouterLink>
        <RouterLink
          v-else
          :to="{ path: '/login', query: { redirect: '/model-plaza' } }"
          class="inline-flex flex-shrink-0 items-center justify-center gap-1.5 rounded-xl border border-primary-500/50 bg-gradient-to-r from-primary-500 to-amber-600 px-4 py-1.5 text-xs font-bold text-black shadow-md hover:brightness-110 active:scale-95 transition-all"
        >
          {{ t('modelPlaza.nav.login') }}
        </RouterLink>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name || 'sub2api')
const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
</script>
