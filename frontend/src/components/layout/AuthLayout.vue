<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden p-4 bg-[#07080C] text-zinc-100">
    <!-- 宇宙星空深色背景（纯白微光点，呼吸微闪，杜绝蓝紫色） -->
    <CosmicBackground />

    <!-- Content Container -->
    <div class="relative z-10 w-full max-w-md">
      <!-- Logo/Brand -->
      <div class="mb-8 text-center">
        <!-- Z-Prism 现代极客折线几何 Logo -->
        <div class="mb-4 inline-flex items-center justify-center">
          <div class="relative flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-amber-500/25 to-orange-600/15 border border-primary-500/40 shadow-inner">
            <svg class="h-7 w-7 text-primary-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
            <div class="absolute -top-1 -right-1 h-2.5 w-2.5 rounded-full bg-primary-400 animate-pulse"></div>
          </div>
        </div>

        <h1 class="text-2xl font-black tracking-tight text-white flex items-center justify-center gap-2">
          <span>sub2api</span>
          <span class="rounded border border-primary-500/40 bg-primary-500/10 px-1.5 py-0.5 text-xs font-mono font-semibold text-primary-300">
            ZeroX Core
          </span>
        </h1>
        <p class="mt-1 text-xs text-zinc-400 font-mono">
          {{ siteSubtitle }}
        </p>
      </div>

      <!-- Card Container - 黑曜石晶体微透卡片 -->
      <div class="rounded-2xl border border-white/[0.09] bg-[#0E1118]/85 p-7 shadow-2xl backdrop-blur-xl">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="mt-6 text-center text-sm text-zinc-400">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="mt-8 text-center text-xs text-zinc-500 font-mono">
        &copy; {{ currentYear }} sub2api. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import CosmicBackground from '@/components/common/CosmicBackground.vue'

const appStore = useAppStore()

const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway & Multiplexing Infrastructure')
const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>
