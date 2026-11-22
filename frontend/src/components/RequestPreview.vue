<template>
  <v-expansion-panels class="my-4">
    <v-expansion-panel :title="t('requestPreview.title')">
      <v-expansion-panel-text>
        <v-alert type="info" variant="tonal" density="compact" class="mb-3">{{ t('requestPreview.scope') }}</v-alert>
        <v-select v-model="protocol" :items="['messages', 'chat', 'responses', 'gemini']" :label="t('requestPreview.protocol')" density="compact" />
        <v-text-field v-model="model" :label="t('requestPreview.model')" density="compact" />
        <v-textarea v-model="sample" :label="t('requestPreview.sample')" rows="5" variant="outlined" />
        <v-btn :loading="loading" :disabled="!channelUid" @click="preview">{{ t('requestPreview.build') }}</v-btn>
        <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
        <template v-if="result">
          <p class="mt-3">{{ t(`requestPreview.mode.${result.mode}`) }} · {{ result.sourceProtocol }} → {{ result.targetProtocol }}</p>
          <v-alert v-if="result.blocked" type="error" variant="tonal">{{ t('requestPreview.blocked') }}</v-alert>
          <p>{{ t('requestPreview.conversionQuality') }}: {{ result.conversion.quality }}</p>
          <p class="text-caption">{{ t('requestPreview.coverage') }}: {{ result.conversion.coverage.join(', ') }}</p>
          <v-list density="compact">
            <v-list-item v-for="(diagnostic, index) in result.conversion.diagnostics" :key="`diagnostic-${index}`" :title="diagnostic.field" :subtitle="`${diagnostic.action} · ${diagnostic.code}`" />
            <v-list-item v-for="effect in result.effects" :key="effect.field" :title="effect.field" :subtitle="t(`requestPreview.effect.${effect.status}`)" />
          </v-list>
          <p class="text-caption">{{ t('requestPreview.sampleBoundary') }}</p>
          <p class="text-caption">{{ t('requestPreview.runtimeBoundary') }}</p>
          <p v-if="result.bodyOmitted" class="text-warning">{{ t('requestPreview.bodyOmitted') }}</p>
          <pre class="request-preview-output">{{ JSON.stringify({ method: result.method, url: result.url, headers: result.headers, body: result.body }, null, 2) }}</pre>
        </template>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from '../i18n'
import { api } from '../services/api'
import type { RequestPreviewResult } from '../services/types/request-preview'
import { buildRequestPreviewDraft, requestPreviewSample } from '../utils/requestPreview'
const props = defineProps<{ channelUid?: string; channelType: string; draft: object }>()
const { t } = useI18n()
const protocol = ref(['messages', 'chat', 'responses', 'gemini'].includes(props.channelType) ? props.channelType : 'chat')
const model = ref('preview-model')
const sample = ref(requestPreviewSample(protocol.value, model.value))
const result = ref<RequestPreviewResult | null>(null)
const error = ref('')
const loading = ref(false)
let generation = 0
watch(protocol, value => { sample.value = requestPreviewSample(value, model.value) })
watch(() => [props.draft, props.channelUid, protocol.value, model.value, sample.value], () => { generation++; result.value = null; error.value = '' }, { deep: true, flush: 'sync' })
async function preview() {
  if (!props.channelUid) return
  const current = ++generation
  loading.value = true; result.value = null; error.value = ''
  try {
    const body: unknown = JSON.parse(sample.value)
    const data = await api.previewChannelRequest(props.channelUid, { protocol: protocol.value, model: model.value, body, draft: buildRequestPreviewDraft(props.draft) })
    if (current === generation) result.value = data
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : String(e) }
  finally { loading.value = false }
}
</script>
<style scoped>
.request-preview-output { max-height: 24rem; overflow: auto; white-space: pre-wrap; word-break: break-word; font-size: 0.8rem; margin-top: 1rem; }
</style>
