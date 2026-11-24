<template>
  <div class="subscription-provider-grid">
    <div class="d-flex flex-wrap ga-4">
      <template v-for="card in allCards" :key="card.id">
        <div class="provider-block">
          <v-card
            class="provider-card pa-4 d-flex flex-column"
            :class="{
              'provider-card--active': expandedProviderId === card.id
            }"
            variant="outlined"
          >
            <div class="d-flex align-center ga-3 mb-2">
              <v-icon size="32" :color="card.iconColor || 'secondary'">
                {{ card.icon || 'mdi-domain' }}
              </v-icon>
              <div class="text-subtitle-1 font-weight-bold">{{ card.displayName }}</div>
            </div>
            <div class="text-caption text-medium-emphasis mb-3 provider-card__desc">
              {{ card.description }}
            </div>
            <v-spacer />
            <div class="d-flex align-center ga-2 mt-auto flex-wrap">
              <v-btn size="small" color="primary" variant="flat" @click="handleAdd(card.id)">
                {{ t('subscription.addProvider') }}
              </v-btn>
              <v-btn
                v-if="providerConsoleLinks[card.id]"
                size="small"
                variant="text"
                append-icon="mdi-open-in-new"
                @click="openProviderConsole(card.id)"
              >
                {{ t('subscription.visitConsole') }}
              </v-btn>
            </div>
          </v-card>
        </div>

        <!-- 展开面板是兄弟 flex item 而非块内子元素：卡片保持原位填空行，面板独占下一行 -->
        <v-expand-transition>
          <div v-if="expandedProviderId === card.id" class="provider-expand">
            <slot name="expand" :provider-id="card.id" :card="card" />
          </div>
        </v-expand-transition>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '@/i18n'
import { getProviderTemplates, type ProviderTemplate } from '@/services/autopilot-api'
import { providerConsoleLinks, openProviderConsole } from '@/utils/provider-links'

const { t } = useI18n()
const emit = defineEmits<{
  add: [providerId: string]
}>()

const expandedProviderId = defineModel<string>('expandedProviderId', { default: '' })

const builtinProviders = ref<ProviderTemplate[]>([])

interface ProviderCard {
  id: string
  displayName: string
  description: string
  icon?: string
  iconColor?: string
}

const providerCards = computed<ProviderCard[]>(() =>
  builtinProviders.value.map(item => ({
    id: item.providerId,
    displayName: item.displayName,
    description: item.description || ''
  }))
)

const specialCards = computed<ProviderCard[]>(() => [
  {
    id: 'github-copilot',
    displayName: 'GitHub Copilot',
    description: t('subscription.copilotDescription'),
    icon: 'mdi-github',
    iconColor: 'primary'
  },
  {
    id: 'new-api',
    displayName: 'new-api',
    description: t('subscription.newApiDescription'),
    icon: 'mdi-server-network',
    iconColor: 'warning'
  }
])

const allCards = computed(() => [...providerCards.value, ...specialCards.value])

function handleAdd(providerId: string) {
  if (expandedProviderId.value === providerId) {
    expandedProviderId.value = ''
    return
  }
  expandedProviderId.value = providerId
  emit('add', providerId)
}

onMounted(async () => {
  try {
    builtinProviders.value = await getProviderTemplates()
  } catch (err) {
    console.error('[Subscription-Providers] 加载服务商失败:', err)
    builtinProviders.value = []
  }
})
</script>

<style scoped>
.provider-block {
  min-width: 240px;
  max-width: 300px;
  flex: 1 1 240px;
  display: flex;
  flex-direction: column;
}
.provider-card {
  width: 100%;
  min-height: 100%;
  transition:
    border-color 0.2s ease,
    background-color 0.2s ease,
    box-shadow 0.2s ease;
}
.provider-card:hover {
  border-color: rgb(var(--v-theme-primary));
  background-color: rgba(var(--v-theme-primary), 0.04);
}
.provider-card--active {
  border-color: rgb(var(--v-theme-primary));
  background-color: rgba(var(--v-theme-primary), 0.08);
}
.provider-card__desc {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.provider-expand {
  /* 独占一行的兄弟 flex item：展开面板全宽，卡片保持在原位 */
  flex: 1 1 100%;
  min-width: 100%;
  width: 100%;
}
</style>
