<template>
  <v-expansion-panels v-if="channelOptions.length" class="mb-4">
    <v-expansion-panel :title="t('modelImport.title')">
      <v-expansion-panel-text>
        <p class="text-caption mb-3">{{ t('modelImport.scope') }}</p>
        <v-select v-model="selectedUid" :items="channelOptions" :label="t('modelImport.channel')" density="compact" />
        <v-btn size="small" :disabled="!selectedUid || busy" @click="load">{{ t('modelImport.load') }}</v-btn>
        <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
        <template v-if="view">
          <p class="text-caption mt-2">{{ t('modelImport.lifecycle') }}</p>
          <v-checkbox
            v-if="view.unrestrictedOnRemove"
            v-model="allowUnrestricted"
            :disabled="busy"
            :label="t('modelImport.allowUnrestricted')"
            hide-details
          />
          <v-btn
            size="small"
            :disabled="busy || (!!view.unrestrictedOnRemove && !allowUnrestricted)"
            @click="removePolicy"
            >{{ t('modelImport.removePolicy') }}</v-btn
          >

          <v-checkbox v-model="policy.enabled" :disabled="busy" :label="t('modelImport.enabled')" hide-details />
          <v-alert v-if="policy.enabled && !view.state.approved?.length" type="info" variant="tonal" class="my-3">{{
            t('modelImport.emptyGate')
          }}</v-alert>
          <div class="model-import-grid">
            <v-textarea
              v-model="includeText"
              :disabled="busy"
              :label="t('modelImport.include')"
              rows="3"
              variant="outlined"
            />
            <v-textarea
              v-model="excludeText"
              :disabled="busy"
              :label="t('modelImport.exclude')"
              rows="3"
              variant="outlined"
            />
            <v-text-field
              v-model="policy.stripPrefix"
              :disabled="busy"
              :label="t('modelImport.stripPrefix')"
              variant="outlined"
              density="compact"
            />
            <v-text-field
              v-model="policy.stripSuffix"
              :disabled="busy"
              :label="t('modelImport.stripSuffix')"
              variant="outlined"
              density="compact"
            />
          </div>
          <v-select
            v-model="policy.case"
            :items="caseOptions"
            :disabled="busy"
            :label="t('modelImport.case')"
            density="compact"
          />
          <p class="text-caption mb-2">{{ t('modelImport.ruleOrder') }}</p>
          <div v-for="(rule, index) in policy.rules" :key="index" class="model-import-rule">
            <v-text-field v-model="rule.pattern" :disabled="busy" :label="t('modelImport.pattern')" density="compact" />
            <v-text-field
              v-model="rule.replacement"
              :disabled="busy"
              :label="t('modelImport.replacement')"
              density="compact"
            />
            <v-btn size="small" :disabled="busy" @click="policy.rules?.splice(index, 1)">{{
              t('modelImport.removeRule')
            }}</v-btn>
          </div>
          <v-btn
            size="small"
            :disabled="busy || (policy.rules?.length || 0) >= 16"
            @click="policy.rules?.push({ pattern: '', replacement: '' })"
            >{{ t('modelImport.addRule') }}</v-btn
          >
          <v-btn class="ml-2" size="small" :disabled="busy" @click="savePolicy">{{
            t('modelImport.savePolicy')
          }}</v-btn>
          <v-divider class="my-4" />
          <p class="text-caption mb-3">{{ t('modelImport.discoveryScope') }}</p>
          <v-btn size="small" :disabled="busy || dirtyPolicy" @click="discover">{{ t('modelImport.discover') }}</v-btn>
          <p v-if="discoveryStarted" class="text-caption mt-2">{{ t('modelImport.discoveryStarted') }}</p>
          <v-textarea
            v-model="modelText"
            :disabled="busy"
            :label="t('modelImport.manualModels')"
            rows="4"
            variant="outlined"
            class="mt-3"
          />
          <v-btn size="small" :disabled="busy || dirtyPolicy" @click="preview">{{ t('modelImport.preview') }}</v-btn>
          <p v-if="dirtyPolicy" class="text-caption mt-2">{{ t('modelImport.saveBeforePreview') }}</p>
          <template v-if="pending">
            <v-alert type="info" variant="tonal" class="my-3">{{
              t('modelImport.diff', {
                added: pending.diff.added.length,
                removed: pending.diff.removed.length,
                changed: pending.diff.changed.length
              })
            }}</v-alert>
            <p class="text-caption">
              {{ t(pending.source === 'discovery' ? 'modelImport.sourceDiscovery' : 'modelImport.sourceOperator') }} ·
              {{ pending.observedAt }}
            </p>
            <p v-if="pending.previousSupportedModels?.length" class="text-caption">
              {{ t('modelImport.previousRules') }}: {{ pending.previousSupportedModels.join(', ') }}
            </p>
            <p v-if="pending.diff.removed.length" class="text-caption">
              {{ t('modelImport.removed') }}: {{ pending.diff.removed.join(', ') }}
            </p>
          </template>
          <v-table v-if="catalog.length" density="compact" class="model-import-table mt-2">
            <thead>
              <tr>
                <th>{{ t('modelImport.publicId') }}</th>
                <th>{{ t('modelImport.upstreamId') }}</th>
                <th>{{ t('modelImport.endpoints') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in visibleCatalog" :key="item.id">
                <td>{{ item.id }}</td>
                <td>{{ item.upstreamId }}</td>
                <td>
                  <span v-for="ep in item.endpoints" :key="`${ep.type}-${ep.source}-${ep.verified}`" class="mr-2"
                    >{{ ep.type }} · {{ t(ep.verified ? 'modelImport.probed' : 'modelImport.declared') }} ({{
                      ep.source
                    }})</span
                  >
                </td>
              </tr>
            </tbody>
          </v-table>
          <v-pagination
            v-if="catalog.length > 20"
            v-model="page"
            :length="Math.ceil(catalog.length / 20)"
            density="compact"
          />
          <template v-if="pending">
            <v-checkbox
              v-if="!pending.models.length"
              v-model="allowEmpty"
              :label="t('modelImport.allowEmpty')"
              hide-details
            />
            <v-checkbox
              v-if="pending.diff.removed.length"
              v-model="allowRemovals"
              :label="t('modelImport.allowRemovals')"
              hide-details
            />
            <v-btn
              class="mt-3"
              color="primary"
              :disabled="
                busy ||
                dirtyPolicy ||
                (!pending.models.length && !allowEmpty) ||
                (!!pending.diff.removed.length && !allowRemovals) ||
                editedPreview
              "
              @click="approve"
              >{{ t('modelImport.approve') }}</v-btn
            >
          </template>
          <p class="text-caption mt-4">
            {{ t('modelImport.activeCount', { count: view.state.approved?.length || 0 }) }}
          </p>
        </template>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '../i18n'
import { api, ApiError } from '../services/api'
import { autoDiscoverChannel } from '../services/autopilot-api'
import type { ModelImportPolicy, ModelImportView } from '../services/types/model-import'
const props = defineProps<{ channels: Array<{ channelUid?: string; name?: string }>; channelType: string }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const selectedUid = ref('')
const busy = ref(false)
const error = ref('')
const view = ref<ModelImportView | null>(null)
const policy = ref<ModelImportPolicy>({ enabled: false, rules: [] })
const includeText = ref('')
const excludeText = ref('')
const modelText = ref('')
const allowUnrestricted = ref(false)
const allowEmpty = ref(false)
const allowRemovals = ref(false)
const discoveryStarted = ref(false)
const page = ref(1)
const previewText = ref('')
const editedPreview = computed(() => !!pending.value && modelText.value !== previewText.value)
let generation = 0
const channelOptions = computed(() =>
  props.channels
    .filter(channel => channel.channelUid)
    .map(channel => ({ title: channel.name || channel.channelUid!, value: channel.channelUid! }))
)
const caseOptions = computed(() => [
  { title: t('modelImport.keepCase'), value: '' },
  { title: t('modelImport.lowerCase'), value: 'lower' },
  { title: t('modelImport.upperCase'), value: 'upper' }
])
const pending = computed(() => view.value?.state.pending)
const catalog = computed(() => pending.value?.models || view.value?.state.approved || [])
const visibleCatalog = computed(() => catalog.value.slice((page.value - 1) * 20, page.value * 20))
const lines = (value: string) =>
  value
    .split(/\r?\n/)
    .map(item => item.trim())
    .filter(Boolean)
function currentPolicy(): ModelImportPolicy {
  return {
    enabled: policy.value.enabled,
    include: lines(includeText.value),
    exclude: lines(excludeText.value),
    stripPrefix: policy.value.stripPrefix || '',
    stripSuffix: policy.value.stripSuffix || '',
    case: policy.value.case || '',
    rules: policy.value.rules || []
  }
}
function normalizedPolicy(value: ModelImportPolicy) {
  return JSON.stringify({
    enabled: value.enabled,
    include: value.include || [],
    exclude: value.exclude || [],
    stripPrefix: value.stripPrefix || '',
    stripSuffix: value.stripSuffix || '',
    case: value.case || '',
    rules: value.rules || []
  })
}
watch(modelText, () => {
  allowEmpty.value = false
  allowRemovals.value = false
})
const dirtyPolicy = computed(
  () => !!view.value && normalizedPolicy(currentPolicy()) !== normalizedPolicy(view.value.state.policy)
)
function apply(value: ModelImportView) {
  allowUnrestricted.value = false
  view.value = value
  modelText.value =
    value.state.pending?.source === 'operator' ? value.state.pending.models.map(item => item.upstreamId).join('\n') : ''
  previewText.value = modelText.value
  policy.value = {
    ...value.state.policy,
    case: value.state.policy.case || '',
    rules: value.state.policy.rules?.map(rule => ({ ...rule })) || []
  }
  includeText.value = value.state.policy.include?.join('\n') || ''
  excludeText.value = value.state.policy.exclude?.join('\n') || ''
  allowEmpty.value = false
  allowRemovals.value = false
  page.value = 1
}
watch(
  () => [selectedUid.value, props.channelType],
  () => {
    allowUnrestricted.value = false
    generation++
    view.value = null
    error.value = ''
    busy.value = false
    modelText.value = ''
    discoveryStarted.value = false
    allowEmpty.value = false
    allowRemovals.value = false
  },
  { flush: 'sync' }
)
watch(channelOptions, options => {
  if (selectedUid.value && !options.some(item => item.value === selectedUid.value)) selectedUid.value = ''
})
async function run(action: (_uid: string, _protocol: string) => Promise<void>) {
  if (busy.value || !selectedUid.value) return
  const current = generation
  const uid = selectedUid.value
  const protocol = props.channelType
  busy.value = true
  error.value = ''
  try {
    await action(uid, protocol)
  } catch (cause) {
    if (current !== generation) return
    if (cause instanceof ApiError && cause.status === 409) {
      try {
        const fresh = await api.getModelImport(uid, protocol)
        if (current !== generation) return
        apply(fresh)
      } catch {
        if (current !== generation) return
        view.value = null
        allowEmpty.value = false
        allowRemovals.value = false
      }
    }
    if (current === generation) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === generation) busy.value = false
  }
}
async function load() {
  const current = generation
  await run(async (uid, protocol) => {
    const result = await api.getModelImport(uid, protocol)
    if (current === generation) apply(result)
  })
}
async function savePolicy() {
  const current = generation
  await run(async (uid, protocol) => {
    if (!view.value) return
    const result = await api.setModelImportPolicy(uid, protocol, currentPolicy(), view.value.revision)
    if (current === generation) {
      apply(result)
      emit('updated')
    }
  })
}
async function removePolicy() {
  const current = generation
  await run(async (uid, protocol) => {
    if (!view.value || (view.value.unrestrictedOnRemove && !allowUnrestricted.value)) return
    await api.removeModelImport(uid, protocol, view.value.revision, allowUnrestricted.value)
    if (current === generation) {
      view.value = null
      allowUnrestricted.value = false
      emit('updated')
    }
  })
}
async function preview() {
  const current = generation
  await run(async (uid, protocol) => {
    if (dirtyPolicy.value) return
    const submittedText = modelText.value
    const result = await api.previewModelImport(uid, protocol, lines(submittedText), view.value!.revision)
    if (current === generation && view.value) {
      view.value = { ...view.value, state: { ...view.value.state, pending: result } }
      previewText.value = submittedText
      allowEmpty.value = false
      allowRemovals.value = false
      page.value = 1
    }
  })
}
async function approve() {
  const current = generation
  await run(async (uid, protocol) => {
    if (
      !pending.value ||
      editedPreview.value ||
      dirtyPolicy.value ||
      (!pending.value.models.length && !allowEmpty.value) ||
      (pending.value.diff.removed.length && !allowRemovals.value)
    )
      return
    const result = await api.approveModelImport(uid, protocol, pending.value.id, allowEmpty.value, allowRemovals.value)
    if (current === generation) {
      apply(result)
      emit('updated')
    }
  })
}
async function discover() {
  const current = generation
  await run(async (uid, protocol) => {
    if (dirtyPolicy.value) return
    await autoDiscoverChannel(protocol, uid)
    if (current === generation) discoveryStarted.value = true
  })
}
</script>
<style scoped>
.model-import-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}
.model-import-rule {
  display: flex;
  align-items: start;
  gap: 0.75rem;
}
.model-import-rule > :first-child,
.model-import-rule > :nth-child(2) {
  min-width: 0;
  flex: 1;
}
.model-import-table {
  overflow: auto;
}
.model-import-table td {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .model-import-grid {
    grid-template-columns: 1fr;
  }
  .model-import-rule {
    flex-wrap: wrap;
  }
}
</style>
