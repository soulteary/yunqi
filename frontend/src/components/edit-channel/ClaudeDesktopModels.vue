<template>
  <section v-if="messageRoutes.length" class="mt-4" data-testid="desktop-model-tiers">
    <div class="text-subtitle-2 mb-2">{{ t('channelEditor.desktopModels.title') }}</div>
    <p class="text-caption text-medium-emphasis mb-3">{{ t('channelEditor.desktopModels.hint') }}</p>
    <div v-for="route in messageRoutes" :key="route.channelUid || route.index" class="mb-4" :data-route-uid="route.channelUid">
      <div class="text-body-2 mb-2">{{ route.name }}</div>
      <p v-if="ruleSummary(route)" class="text-caption text-medium-emphasis mb-2">
        {{ t('channelEditor.desktopModels.inheritedRules', { rules: ruleSummary(route) }) }}
      </p>
      <v-select
        v-for="model in modelsFor(route)"
        :key="model"
        :data-model-id="model"
        :label="model"
        :model-value="selectedDesktopTier(route, model)"
        :items="tierOptions"
        :disabled="disabled || !route.channelUid"
        variant="outlined"
        density="compact"
        class="mb-2"
        @update:model-value="setTier(route, model, $event)"
        @update:menu="$emit('menu-update', $event)"
      />
      <div class="d-flex ga-2 align-start">
        <v-text-field
          :model-value="newModels[keyFor(route)] || ''"
          :label="t('channelEditor.desktopModels.modelId')"
          :disabled="disabled || !route.channelUid"
          variant="outlined"
          density="compact"
          hide-details
          @update:model-value="newModels[keyFor(route)] = $event || ''"
          @keydown.enter.prevent="addModel(route)"
        />
        <v-btn :disabled="disabled || !route.channelUid || !newModels[keyFor(route)]?.trim()" variant="tonal" @click="addModel(route)">
          {{ t('channelEditor.desktopModels.add') }}
        </v-btn>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import type { ChannelProtocolRoute } from '../../services/api'
import { useI18n } from '../../i18n'
import { DESKTOP_TIERS, selectedDesktopTier } from '../../utils/desktopModelTiers'

const props = defineProps<{ modelValue: ChannelProtocolRoute[]; inventoryRoutes?: ChannelProtocolRoute[]; disabled?: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [routes: ChannelProtocolRoute[]]
  'menu-update': [open: boolean]
}>()
const { t } = useI18n()
const messageRoutes = computed(() => props.modelValue.filter(route => route.kind === 'messages'))
const tierOptions = computed(() => [
  { title: t('channelEditor.desktopModels.none'), value: '' },
  ...DESKTOP_TIERS.map(tier => ({ title: tier.charAt(0).toUpperCase() + tier.slice(1), value: tier })),
])
const newModels = reactive<Record<string, string>>({})
const addedModels = reactive<Record<string, string[]>>({})
const keyFor = (route: ChannelProtocolRoute) => route.channelUid || `messages:${route.index}`
const ruleSummary = (route: ChannelProtocolRoute) => Object.entries(route.claudeDesktopTierRules ?? {}).filter(([pattern]) => pattern.includes('*') || pattern.startsWith('!')).map(([pattern, tier]) => `${pattern} → ${tier || t('channelEditor.desktopModels.none')}`).join(', ')
function modelsFor(route: ChannelProtocolRoute) {
  const inventory = props.inventoryRoutes?.find(item => item.channelUid === route.channelUid && item.kind === route.kind) ?? route
  return [...new Set([
    ...Object.keys(route.claudeDesktopModelTiers ?? {}),
    ...(inventory.discoveredModels ?? []),
    ...(inventory.modelBindings?.flatMap(binding => binding.models) ?? []),
    ...(route.supportedModels ?? []).filter(model => !model.includes('*') && !model.startsWith('!')),
    ...(addedModels[keyFor(route)] ?? []),
  ])].filter(Boolean).sort()
}
function addModel(route: ChannelProtocolRoute) {
  const model = newModels[keyFor(route)]?.trim()
  if (!model || model.includes('*') || model.startsWith('!') || Array.from(model).length > 256 || /\p{Cc}/u.test(model)) return
  addedModels[keyFor(route)] = [...(addedModels[keyFor(route)] ?? []), model]
  newModels[keyFor(route)] = ''
}
function setTier(route: ChannelProtocolRoute, model: string, value: unknown) {
  if (props.disabled || !route.channelUid || typeof value !== 'string' || (value !== '' && !DESKTOP_TIERS.some(tier => tier === value))) return
  emit('update:modelValue', props.modelValue.map(item => item === route
    ? { ...item, claudeDesktopModelTiers: { ...(item.claudeDesktopModelTiers ?? {}), [model]: value } }
    : item))
}
</script>
