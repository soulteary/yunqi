<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '@/services/api'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'
import type { ClientMappingTarget, ClientModelMappings, MappingClient } from '@/services/types/client-model-mappings'
import { clientModelAlias, clientProtocol, clientSlots, mappingClients } from '@/utils/client-model-mappings'

const { locale } = useI18n()
const zh = computed(() => locale.value === 'zh-CN')
const auth = useAuthStore()
const client = ref<MappingClient>('claude-desktop')
const mappings = ref<ClientModelMappings>({})
const targets = ref<ClientMappingTarget[]>([])
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const error = ref('')
const notice = ref('')
const snapshot = ref('{}')
const dirty = computed(() => JSON.stringify(mappings.value) !== snapshot.value)
let generation = 0
onBeforeUnmount(() => { generation++ })
const options = computed(() => {
  const active = targets.value.filter(target => target.kind === clientProtocol[client.value]).map(target => ({
    title: `${target.model} — ${target.channels.join(', ')}`, value: target.model, props: { disabled: false }
  }))
  const known = new Set(active.map(option => option.value))
  for (const model of Object.values(mappings.value[client.value] ?? {})) {
    if (!known.has(model)) {
      active.push({ title: `${model} (${zh.value ? '当前不可用' : 'currently unavailable'})`, value: model, props: { disabled: true } })
      known.add(model)
    }
  }
  return [{ title: zh.value ? '不映射' : 'Not mapped', value: '' }, ...active]
})
function select(slot: string, value: unknown) {
  if (!loaded.value || loading.value || saving.value || typeof value !== 'string') return
  const slots = { ...mappings.value[client.value] }
  if (value) slots[slot] = value
  else delete slots[slot]
  mappings.value = { ...mappings.value, [client.value]: slots }
  notice.value = ''
}
async function load() {
  if (!auth.apiKey || loading.value || saving.value) return
  const version = ++generation
  loading.value = true
  loaded.value = false
  error.value = ''
  notice.value = ''
  try {
    const state = await api.getClientModelMappings()
    if (generation !== version) return
    mappings.value = state.mappings ?? {}
    targets.value = state.targets
    snapshot.value = JSON.stringify(mappings.value)
    loaded.value = true
  } catch (e) {
    if (generation === version) error.value = e instanceof Error ? e.message : String(e)
  } finally {
    if (generation === version) loading.value = false
  }
}
async function save() {
  if (!loaded.value || !auth.apiKey || saving.value || loading.value) return
  const version = generation
  saving.value = true
  error.value = ''
  try {
    const state = await api.setClientModelMappings(mappings.value)
    if (generation !== version) return
    mappings.value = state.mappings ?? {}
    targets.value = state.targets
    snapshot.value = JSON.stringify(mappings.value)
    notice.value = zh.value ? '映射已保存' : 'Mappings saved'
  } catch (e) {
    if (generation === version) error.value = e instanceof Error ? e.message : String(e)
  } finally {
    if (generation === version) saving.value = false
  }
}
watch(() => auth.apiKey, () => {
  generation++
  loaded.value = false
  loading.value = false
  saving.value = false
  mappings.value = {}
  targets.value = []
  error.value = ''
  notice.value = ''
  if (auth.apiKey) void load()
}, { immediate: true })
</script>

<template>
  <v-card class="pa-4 mb-6" variant="outlined" data-testid="client-model-mappings">
    <h2 class="text-h6 mb-3">{{ zh ? '客户端模型映射' : 'Client model mappings' }}</h2>
    <p class="text-body-2 mb-4">{{ zh
      ? '从客户端视角选择目标模型。多个档位可以共用同一模型；请求仍由当前激活渠道正常调度，模型能力以实际目标为准。'
      : 'Choose target models for each client. Slots can share a model. Requests use normal active-channel scheduling and the target model’s actual capabilities.' }}</p>
    <v-alert v-if="error" type="error" class="mb-3">{{ error }}</v-alert>
    <v-alert v-if="notice" type="success" class="mb-3">{{ notice }}</v-alert>
    <v-select v-model="client" :items="mappingClients" :label="zh ? '客户端' : 'Client'" :disabled="saving" data-test="mapping-client" />
    <v-select
      v-for="slot in clientSlots[client]" :key="`${client}:${slot}`"
      :data-slot="slot" :label="slot.charAt(0).toUpperCase() + slot.slice(1)"
      :model-value="mappings[client]?.[slot] ?? ''" :items="options"
      :disabled="!loaded || loading || saving" variant="outlined" density="compact"
      :hint="mappings[client]?.[slot] ? clientModelAlias(client, slot) : ''" persistent-hint
      @update:model-value="select(slot, $event)"
    />
    <p class="text-caption mb-3">{{ zh
      ? '仅列出默认入口下激活渠道已声明或已批准的模型。前缀专用渠道保持独立。Claude Desktop 从 /v1/models 发现映射档位；其他客户端使用上方显示的 Yunqi 模型别名。'
      : 'Lists declared or approved models in active default-route channels. Prefix-only channels stay separate. Claude Desktop discovers mapped tiers at /v1/models; other clients use the Yunqi model aliases shown above.' }}</p>
    <v-btn data-test="mapping-save" :disabled="!loaded || !dirty || loading" :loading="saving" @click="save">{{ zh ? '保存映射' : 'Save mappings' }}</v-btn>
    <v-btn class="ml-2" variant="text" :disabled="loading || saving" @click="load">{{ zh ? '重新加载' : 'Reload' }}</v-btn>
  </v-card>
</template>
