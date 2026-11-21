<template>
  <div class="my-4">
    <v-alert type="info" variant="tonal" density="compact" class="mb-3">{{ t('network.scope') }}</v-alert>
    <v-select data-testid="proxy-mode" :model-value="value.proxyMode || 'legacy'" :items="proxyModes" :label="t('network.proxyMode')" variant="outlined" density="compact" @update:model-value="update({ proxyMode: $event })" />
    <v-select data-testid="egress-interface" :model-value="value.interfaceName || ''" :items="interfaceItems" :label="t('network.interface')" variant="outlined" density="compact" :loading="loading" @update:model-value="selectInterface" />
    <v-btn size="small" variant="text" :loading="loading" @click="loadInterfaces">{{ t('network.refresh') }}</v-btn>
    <v-select :model-value="value.addressFamily || 'auto'" :items="['auto', 'ipv4', 'ipv6']" :label="t('network.family')" variant="outlined" density="compact" class="mt-3" @update:model-value="update({ addressFamily: $event })" />
    <v-select :model-value="value.dns?.mode || 'system'" :items="['system', 'bound']" :label="t('network.dnsMode')" :disabled="!!value.interfaceName" variant="outlined" density="compact" @update:model-value="update({ dns: { mode: $event, servers: $event === 'bound' ? value.dns?.servers || [] : [] } })" />
    <v-textarea v-if="value.dns?.mode === 'bound'" :model-value="(value.dns.servers || []).join('\n')" :label="t('network.dnsServers')" :hint="t('network.dnsHint')" persistent-hint rows="2" variant="outlined" @update:model-value="update({ dns: { mode: 'bound', servers: String($event || '').split(/\s+/).filter(Boolean) } })" />
    <v-btn class="mt-3" size="small" :loading="diagnosing" :disabled="!baseUrl" @click="diagnose">{{ t('network.diagnose') }}</v-btn>
    <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
    <v-alert v-for="(result, i) in results" :key="i" :type="result.success ? 'success' : 'warning'" variant="tonal" class="mt-3">
      {{ result.branch }} · {{ result.stage }} · {{ result.localAddress || '—' }} → {{ result.remoteAddress || '—' }}
      <div>{{ t('network.binding') }}: {{ result.bindingApplied ? t('network.applied') : t('network.notApplied') }}</div>
      <div v-if="result.error">{{ result.error }}</div>
    </v-alert>
    <p class="text-caption text-medium-emphasis mt-2">{{ t('network.boundary') }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from '../i18n'
import { api } from '../services/api'
import type { NetworkDiagnosis, NetworkInterface, OutboundNetworkConfig } from '../services/types/network'
const props = defineProps<{ modelValue?: OutboundNetworkConfig | null; baseUrl?: string; proxyUrl?: string; proxyPreferDirect?: boolean; insecureSkipVerify?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [OutboundNetworkConfig] }>()
const { t } = useI18n()
const value = computed(() => props.modelValue || {})
const interfaces = ref<NetworkInterface[]>([])
const loading = ref(false)
const diagnosing = ref(false)
const error = ref('')
const results = ref<NetworkDiagnosis[]>([])
const proxyModes = computed(() => ['legacy', 'direct', 'proxy', 'direct_first'].map(mode => ({ title: t(`network.mode.${mode}`), value: mode })))
const interfaceItems = computed(() => {
  const items = [{ title: t('network.systemRoute'), value: '' }, ...interfaces.value.map(i => ({ title: `${i.name} · ${i.addresses.join(', ')}${i.up && i.bindingSupported ? '' : ` · ${t('network.unavailable')}`}`, value: i.name }))]
  if (value.value.interfaceName && !items.some(i => i.value === value.value.interfaceName)) items.push({ title: `${value.value.interfaceName} · ${t('network.unavailable')}`, value: value.value.interfaceName })
  return items
})
function update(patch: Partial<OutboundNetworkConfig>) { results.value = []; emit('update:modelValue', { ...value.value, ...patch }) }
function selectInterface(name: string) {
  update({ interfaceName: name, dns: name ? { mode: 'bound', servers: value.value.dns?.servers || [] } : value.value.dns })
}
async function loadInterfaces() {
  loading.value = true; error.value = ''
  try { interfaces.value = (await api.getNetworkInterfaces()).interfaces } catch (e) { error.value = e instanceof Error ? e.message : String(e) } finally { loading.value = false }
}
async function diagnose() {
  diagnosing.value = true; error.value = ''; results.value = []
  try { results.value = (await api.diagnoseNetwork({ baseUrl: props.baseUrl, proxyUrl: props.proxyUrl, proxyPreferDirect: props.proxyPreferDirect, insecureSkipVerify: props.insecureSkipVerify, outboundNetwork: props.modelValue })).results } catch (e) { error.value = e instanceof Error ? e.message : String(e) } finally { diagnosing.value = false }
}
</script>
