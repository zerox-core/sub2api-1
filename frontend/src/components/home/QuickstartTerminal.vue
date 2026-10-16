<template>
  <div class="relative w-full overflow-hidden rounded-2xl border border-white/[0.09] bg-[#0A0C12]/95 shadow-2xl backdrop-blur-2xl">
    <!-- 终端顶部控制栏 -->
    <div class="flex items-center justify-between border-b border-white/[0.08] bg-[#0E1118]/80 px-4 py-3">
      <!-- 窗口三色按钮 -->
      <div class="flex items-center gap-2">
        <span class="h-3 w-3 rounded-full bg-[#EF4444]/80"></span>
        <span class="h-3 w-3 rounded-full bg-[#F59E0B]/80"></span>
        <span class="h-3 w-3 rounded-full bg-[#10B981]/80"></span>
        <span class="ml-2 font-mono text-xs text-zinc-500">developer-quickstart ~ bash</span>
      </div>

      <!-- 客户端切换 Tabs -->
      <div class="flex items-center gap-1 rounded-lg bg-black/40 p-1 border border-white/[0.06]">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          @click="activeTab = tab.id"
          :class="[
            'rounded-md px-2.5 py-1 text-xs font-mono transition-all',
            activeTab === tab.id
              ? 'bg-primary-500 text-black font-bold shadow-sm'
              : 'text-zinc-400 hover:text-zinc-200'
          ]"
        >
          {{ tab.name }}
        </button>
      </div>

      <!-- 复制按钮 -->
      <button
        @click="copyCode"
        class="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-2.5 py-1 text-xs font-mono text-zinc-300 transition-all hover:border-primary-500/40 hover:bg-primary-500/10 hover:text-primary-300"
      >
        <svg v-if="!copied" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
        </svg>
        <span v-if="copied" class="text-emerald-400 font-bold">✓ Copied</span>
        <span v-else>Copy</span>
      </button>
    </div>

    <!-- 终端代码内容区 -->
    <div class="p-5 font-mono text-xs leading-relaxed text-zinc-200 overflow-x-auto min-h-[220px]">
      <pre class="selection:bg-primary-500/30 selection:text-primary-200"><code>{{ currentSnippet }}</code></pre>
    </div>

    <!-- 终端底部状态条 -->
    <div class="flex items-center justify-between border-t border-white/[0.06] bg-black/40 px-4 py-2 text-[11px] font-mono text-zinc-500">
      <div class="flex items-center gap-2">
        <span class="inline-block h-2 w-2 rounded-full bg-emerald-400 animate-ping"></span>
        <span class="text-zinc-400">Endpoint: https://api.zeroxcore.tech</span>
      </div>
      <div class="flex items-center gap-4">
        <span>协议: OpenAI & Claude 双原生兼容</span>
        <span class="text-emerald-400">延迟: 24ms</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const activeTab = ref('curl')
const copied = ref(false)

const tabs = [
  { id: 'curl', name: 'cURL' },
  { id: 'python', name: 'Python' },
  { id: 'claude_code', name: 'Claude Code' },
  { id: 'cursor', name: 'Cursor' },
]

const snippets: Record<string, string> = {
  curl: `# 1. 直接请求 OpenAI 兼容接口
curl https://api.zeroxcore.tech/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer sk-sub2api-your-key" \\
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "9.11 与 9.8 谁更大？详细推理过程"}],
    "stream": true
  }'`,

  python: `from openai import OpenAI

# 2. Python 客户端开箱即用，原生支持流式输出
client = OpenAI(
    api_key="sk-sub2api-your-key",
    base_url="https://api.zeroxcore.tech/v1"
)

response = client.chat.completions.create(
    model="claude-3-7-sonnet",
    messages=[{"role": "user", "content": "设计一个高性能分布式网关架构"}],
    stream=True
)

for chunk in response:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)`,

  claude_code: `# 3. 在终端一行命令绑定 Claude Code 编程环境
export ANTHROPIC_BASE_URL="https://api.zeroxcore.tech"
export ANTHROPIC_AUTH_TOKEN="sk-sub2api-your-key"

# 启动 Claude Code 编程助手
claude`,

  cursor: `// 4. Cursor / Windsurf 自定义模型配置
// 打开 Settings -> Models -> OpenAI API Key
API Key: sk-sub2api-your-key
Base URL: https://api.zeroxcore.tech/v1

// 支持填写的推荐模型名：
// - claude-3-7-sonnet
// - deepseek-reasoner
// - o3-mini
// - gemini-2.5-flash`
}

const currentSnippet = computed(() => snippets[activeTab.value] || snippets.curl)

function copyCode() {
  navigator.clipboard.writeText(currentSnippet.value)
  copied.value = true
  setTimeout(() => {
    copied.value = false
  }, 2000)
}
</script>
