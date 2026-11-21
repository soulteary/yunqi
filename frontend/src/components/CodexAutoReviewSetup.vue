<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import { api } from '@/services/api'
import { useAuthStore } from '@/stores/auth'
import type { CodexAutoReviewMapping } from '@/services/api'

const props = defineProps<{ model: string }>()
const { locale } = useI18n()
const zh = computed(() => locale.value === 'zh-CN')
const auth = useAuthStore()
const open = ref(false)
const loading = ref(false)
const saving = ref(false)
const loaded = ref<CodexAutoReviewMapping>()
const target = ref('')
const error = ref('')
const notice = ref('')
const valid = computed(() => {
  const model = target.value.trim()
  const hasControl = [...model].some(character => {
    const code = character.charCodeAt(0)
    return code < 32 || (code >= 127 && code <= 159)
  })
  return (
    !!model &&
    new Blob([model]).size <= 256 &&
    model.toLowerCase() !== 'codex-auto-review' &&
    !hasControl
  )
})
let generation = 0
onBeforeUnmount(() => {
  generation++
})
watch(
  () => auth.apiKey,
  () => {
    generation++
    open.value = false
    loading.value = false
    saving.value = false
    loaded.value = undefined
    target.value = ''
    error.value = ''
    notice.value = ''
  }
)
async function show() {
  if (!auth.apiKey || loading.value || saving.value) return
  const version = ++generation
  open.value = true
  loading.value = true
  error.value = ''
  notice.value = ''
  loaded.value = undefined
  try {
    const mapping = await api.getCodexAutoReviewMapping()
    if (version !== generation) return
    loaded.value = mapping
    target.value = mapping.targetModel || (props.model.trim() !== 'codex-auto-review' ? props.model.trim() : '')
  } catch (e) {
    if (version === generation) error.value = e instanceof Error ? e.message : String(e)
  } finally {
    if (version === generation) loading.value = false
  }
}
function close() {
  generation++
  open.value = false
  loading.value = false
}
async function save(enabled: boolean) {
  if (!auth.apiKey || !loaded.value || saving.value || (enabled && !valid.value)) return
  const version = generation
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const mapping = await api.setCodexAutoReviewMapping({
      enabled,
      targetModel: enabled ? target.value.trim() : loaded.value.targetModel
    })
    if (version !== generation) return
    loaded.value = mapping
    target.value = mapping.targetModel
    notice.value = zh.value
      ? enabled
        ? '映射已启用并保存'
        : '映射已停用'
      : enabled
        ? 'Mapping enabled and saved'
        : 'Mapping disabled'
  } catch (e) {
    if (version === generation) error.value = e instanceof Error ? e.message : String(e)
  } finally {
    if (version === generation) saving.value = false
  }
}
</script>

<template>
  <v-card class="mt-4 pa-3" variant="outlined">
    <div class="d-flex flex-wrap align-center justify-space-between ga-2">
      <span class="text-subtitle-2">{{ zh ? 'Codex 自动审批映射' : 'Codex auto-review mapping' }}</span>
      <v-btn data-test="review-open" size="small" variant="tonal" :disabled="loading || saving" @click="show">
        {{ zh ? '快速配置' : 'Quick setup' }}
      </v-btn>
    </div>
    <div class="text-caption mt-2">
      {{
        zh
          ? '全局用于此网关的 Codex Responses 审查请求，按目标模型现有路由处理；不会改变 Codex 的审批设置。'
          : 'Applies globally to Codex Responses review requests on this gateway, using the target model’s normal routing. Codex approval settings stay unchanged.'
      }}
    </div>
    <template v-if="open">
      <p v-if="loading" class="mt-3">{{ zh ? '读取中…' : 'Loading…' }}</p>
      <v-alert v-if="error" type="error" class="mt-3" density="compact">{{ error }}</v-alert>
      <template v-if="loaded">
        <p class="text-caption my-3">
          {{
            loaded.enabled
              ? (zh ? '当前已启用：' : 'Enabled: ') + loaded.targetModel
              : zh
                ? '当前未启用，沿用自动路由'
                : 'Disabled; using automatic routing'
          }}
        </p>
        <v-text-field
          v-model="target"
          data-test="review-target"
          :disabled="saving"
          :label="zh ? '目标请求模型' : 'Target request model'"
          :hint="
            zh ? '填入已可用的模型名，默认带入上方当前模型。' : 'Use an available model; defaults to the model above.'
          "
          persistent-hint
        />
        <v-alert v-if="target && !valid" type="warning" density="compact" class="mb-3">{{
          zh
            ? '请填写有效目标模型，不能映射到 codex-auto-review 自身。'
            : 'Enter a valid target other than codex-auto-review itself.'
        }}</v-alert>
        <div class="d-flex flex-wrap ga-2">
          <v-btn
            data-test="review-save"
            color="primary"
            :loading="saving"
            :disabled="!valid || saving"
            @click="save(true)"
            >{{ zh ? '启用并保存' : 'Enable and save' }}</v-btn
          >
          <v-btn
            v-if="loaded.enabled"
            data-test="review-disable"
            variant="tonal"
            :disabled="saving"
            @click="save(false)"
            >{{ zh ? '停用映射' : 'Disable mapping' }}</v-btn
          >
        </div>
      </template>
      <v-alert v-if="notice" type="success" density="compact" class="mt-3">{{ notice }}</v-alert>
      <v-btn data-test="review-close" variant="text" size="small" class="mt-2" :disabled="saving" @click="close">{{
        zh ? '关闭' : 'Close'
      }}</v-btn>
    </template>
  </v-card>
</template>
