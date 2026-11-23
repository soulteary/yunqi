<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { api } from '@/services/api'
import { useAuthStore } from '@/stores/auth'
import type { ConsoleStatus } from '@/services/console-types'
import { clientConfig, type ClientKind } from '@/utils/client-setup'
import CodexAutoReviewSetup from '@/components/CodexAutoReviewSetup.vue'
import ClientModelMappings from '@/components/ClientModelMappings.vue'
const { locale } = useI18n()
const zh = computed(() => locale.value === 'zh-CN')
const auth = useAuthStore()
const status = ref<ConsoleStatus>()
const error = ref('')
const loading = ref(false)
const notice = ref('')
const address = ref(
  import.meta.env.DEV
    ? import.meta.env.VITE_BACKEND_URL || import.meta.env.VITE_PROXY_TARGET || window.location.origin
    : window.location.origin
)
const model = ref('')
const kind = ref<ClientKind>('claude')
const codexContextWindow = ref<string | null>('')
const config = computed(() => {
  try {
    const context = String(codexContextWindow.value ?? '').trim()
    return {
      ...clientConfig(
        kind.value,
        address.value,
        model.value,
        kind.value === 'codex' && context ? Number(context) : undefined
      ),
      error: ''
    }
  } catch (e) {
    return { filename: '', content: '', error: String(e) }
  }
})
let requestVersion = 0
async function refresh() {
  if (!auth.apiKey) return
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  try {
    const nextStatus = await api.getConsoleStatus()
    if (version === requestVersion) status.value = nextStatus
  } catch (e) {
    if (version === requestVersion) {
      status.value = undefined
      error.value = e instanceof Error ? e.message : String(e)
    }
  } finally {
    if (version === requestVersion) loading.value = false
  }
}
watch(
  () => auth.apiKey,
  key => {
    ++requestVersion
    status.value = undefined
    error.value = ''
    notice.value = ''
    loading.value = false
    if (key) void refresh()
  },
  { immediate: true }
)
function download(content: string, filename: string) {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
async function copy() {
  try {
    await navigator.clipboard.writeText(config.value.content)
    notice.value = zh.value ? '已复制' : 'Copied'
  } catch {
    notice.value = zh.value ? '复制失败，请手动选择文本复制。' : 'Select and copy the text manually.'
  }
}
function diagnostics() {
  if (!status.value) return
  download(
    JSON.stringify({ exportedAt: new Date().toISOString(), service: status.value }, null, 2),
    'yunqi-diagnostics.json'
  )
}
</script>

<template>
  <section>
    <div class="d-flex align-center justify-space-between mb-4">
      <h1 class="text-h5">{{ zh ? '服务与客户端接入' : 'Service & client setup' }}</h1>
      <v-btn :loading="loading" @click="refresh">{{ zh ? '刷新状态' : 'Refresh' }}</v-btn>
    </div>
    <v-alert v-if="error" type="error" class="mb-4">{{ error }}</v-alert>
    <v-alert v-if="notice" type="info" closable class="mb-4" @click:close="notice = ''">{{ notice }}</v-alert>
    <v-card v-if="status" class="pa-4 mb-6" variant="outlined">
      <h2 class="text-h6 mb-3">{{ zh ? '运行状态' : 'Runtime status' }}</h2>
      <p>{{ status.version.version }} · {{ status.version.gitCommit }} · {{ status.mode }}</p>
      <p>{{ zh ? '构建时间' : 'Build time' }}: {{ status.version.buildTime }}</p>
      <p>{{ zh ? '运行时间（分钟）' : 'Uptime (minutes)' }}: {{ Math.floor(status.uptimeSeconds / 60) }}</p>
      <p>{{ zh ? '监听地址' : 'Listening on' }}: {{ status.settings.bindHost || '*' }}:{{ status.settings.port }}</p>
      <p class="my-2">
        {{
          Object.entries(status.channels)
            .map(([name, count]) => `${name}: ${count}`)
            .join(' · ')
        }}
      </p>
      <v-btn :disabled="loading" @click="diagnostics">{{
        zh ? '导出无密钥诊断信息' : 'Export diagnostics without keys'
      }}</v-btn>
      <v-btn to="/health" variant="text">{{ zh ? '健康与排障' : 'Health & diagnostics' }}</v-btn>
      <v-btn to="/conversations" variant="text">{{ zh ? '请求与调度记录' : 'Requests & routing' }}</v-btn>
    </v-card>
    <ClientModelMappings />
    <h2 class="text-h6 mb-3">{{ zh ? '接入向导' : 'Connect a client' }}</h2>
    <p class="mb-3">
      {{
        zh
          ? '1. 添加上游并完成渠道能力测试；2. 生成客户端配置；3. 在客户端发送请求，在会话页面确认结果。'
          : '1. Add and test an upstream. 2. Generate client configuration. 3. Send a client request and inspect Conversations.'
      }}
    </p>
    <v-btn to="/channels/messages" class="mb-4">{{ zh ? '添加或测试渠道' : 'Add or test channels' }}</v-btn>
    <v-row>
      <v-col cols="12" md="5">
        <v-select
          v-model="kind"
          :items="[
            { title: 'Claude Code', value: 'claude' },
            { title: 'Codex', value: 'codex' },
            { title: 'OpenCode', value: 'opencode' }
          ]"
          :label="zh ? '客户端' : 'Client'"
        />
        <v-text-field
          v-model="address"
          :label="zh ? '客户端可访问的 Yunqi 地址' : 'Yunqi address reachable by the client'"
          hint="https://gateway.example/team"
          persistent-hint
        />
        <v-text-field
          v-model="model"
          data-test="client-model"
          :label="zh ? '已配置的请求模型名' : 'Configured request model'"
        />
        <v-text-field
          v-if="kind === 'codex'"
          v-model="codexContextWindow"
          data-test="codex-context-window"
          type="number"
          min="1"
          step="1"
          clearable
          :label="zh ? 'Codex 上下文窗口（tokens，可选）' : 'Codex context window (tokens, optional)'"
          :hint="
            zh
              ? '留空沿用客户端默认值。仅影响 Codex 本地配置，不能扩展上游模型的实际容量。'
              : 'Leave blank for client defaults. This only sets local Codex configuration, not upstream capacity.'
          "
          persistent-hint
        />
        <CodexAutoReviewSetup v-if="kind === 'codex'" :model="model" />
        <v-alert type="info" variant="tonal">{{
          zh
            ? '远程访问时请填写客户端能连接的网关地址。代理密钥与管理密钥可能不同，配置中只使用 PROXY_ACCESS_KEY。'
            : 'For remote deployments, enter the reachable gateway address. Use PROXY_ACCESS_KEY, which may differ from the administrator key.'
        }}</v-alert>
      </v-col>
      <v-col cols="12" md="7">
        <v-alert v-if="config.error" type="warning">{{
          zh
            ? '请输入有效的 HTTP(S) 网关地址和模型名；Codex 上下文窗口如填写，须为正整数。'
            : 'Enter a valid HTTP(S) gateway URL and model. An optional Codex context window must be a positive integer.'
        }}</v-alert>
        <template v-else>
          <p class="mb-2">{{ config.filename }}</p>
          <pre class="config-preview pa-4 mb-3">{{ config.content }}</pre>
          <v-btn @click="copy">{{ zh ? '复制配置' : 'Copy configuration' }}</v-btn>
          <v-btn variant="text" @click="download(config.content, config.filename)">{{
            zh ? '下载配置' : 'Download'
          }}</v-btn>
          <p class="mt-3">
            {{
              zh
                ? '合并到已有客户端配置，不要直接覆盖原文件。'
                : 'Merge into existing client configuration; do not overwrite it.'
            }}
          </p>
          <p v-if="kind === 'claude'">
            {{
              zh
                ? '将 env 字段合并到 ~/.claude/settings.json，并将 YOUR_YUNQI_PROXY_KEY 替换为 Yunqi 的 PROXY_ACCESS_KEY。'
                : 'Merge env into ~/.claude/settings.json and replace YOUR_YUNQI_PROXY_KEY with Yunqi’s PROXY_ACCESS_KEY.'
            }}
          </p>
          <p v-else>
            {{
              zh
                ? '设置环境变量 YUNQI_PROXY_KEY。Codex 合并到 ~/.codex/config.toml；OpenCode 合并到 ~/.config/opencode/opencode.json。'
                : 'Set YUNQI_PROXY_KEY in the client environment. Merge Codex into ~/.codex/config.toml or OpenCode into ~/.config/opencode/opencode.json.'
            }}
          </p>
        </template>
      </v-col>
    </v-row>
    <v-divider class="my-6" />
    <h2 class="text-h6 mb-3">{{ zh ? '配置与服务维护' : 'Configuration & maintenance' }}</h2>
    <p>
      {{
        zh
          ? '渠道配置保存在 config.json，支持热重载。端口、监听地址和访问密钥来自部署环境，修改后由进程管理器重启服务。'
          : 'Channel settings in config.json support hot reload. Port, bind address and access keys come from the deployment environment and require a service restart.'
      }}
    </p>
    <p>
      {{
        zh
          ? '使用 Docker Compose、systemd 或 launchd 管理启动、停止与自动运行。控制台不操作访问者本机文件，也不提供远程命令执行。'
          : 'Use Docker Compose, systemd or launchd for service lifecycle management. The console does not edit client-machine files or execute remote shell commands.'
      }}
    </p>
    <p>
      {{
        zh
          ? '备份时保留配置、环境文件与状态目录；数据库使用一致性备份，或先由管理员停止写入。导入渠道前先在渠道页导出备份。'
          : 'Back up configuration, environment files and the state directory. Use a consistent database backup or stop writes first. Export channels before importing replacements.'
      }}
    </p>
  </section>
</template>
<style scoped>
.config-preview {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: rgb(var(--v-theme-surface-variant));
  border-radius: 4px;
}
</style>
