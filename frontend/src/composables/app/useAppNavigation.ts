import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
import { useChannelStore } from '../../stores/channel'
import { usePreferencesStore } from '../../stores/preferences'
import { useDialogStore } from '../../stores/dialog'
import { useSystemStore } from '../../stores/system'
import { useI18n, type SupportedLocale } from '../../i18n'
import type { Channel, ChannelKind } from '../../services/api'

// 导航展示、语言和新用户指引；渠道类型仍由 ChannelStore 跟随路由。
export function useAppNavigation() {
  const route = useRoute()
  const authStore = useAuthStore()
  const channelStore = useChannelStore()
  const preferencesStore = usePreferencesStore()
  const dialogStore = useDialogStore()
  const systemStore = useSystemStore()
  const { locale, t, setLocale } = useI18n()

  const languageOptions: Array<{ value: SupportedLocale; label: string; shortLabel: string }> = [
    { value: 'en', label: 'English', shortLabel: 'EN' },
    { value: 'id', label: 'Bahasa Indonesia', shortLabel: 'ID' },
    { value: 'zh-CN', label: '简体中文', shortLabel: '中' }
  ]

  const currentLocale = computed(() => locale.value)
  const currentLanguageShortLabel = computed(() => {
    return (
      languageOptions.find(option => option.value === currentLocale.value)?.shortLabel ??
      currentLocale.value.slice(0, 2).toUpperCase()
    )
  })

  // API 类型 Tab 选项（移动端下拉菜单使用）
  const apiTabOptions = [
    { value: 'messages', labelKey: 'app.tabs.channels', route: '/channels/messages', icon: 'mdi-server-network' },
    { value: 'images', labelKey: 'app.tabs.images', route: '/channels/images', icon: 'mdi-image-outline' },
    { value: 'vectors', labelKey: 'app.tabs.vectors', route: '/channels/vectors', icon: 'mdi-vector-point' },
    { value: 'conversations', labelKey: 'app.tabs.conversations', route: '/conversations', icon: 'mdi-radar' },
    { value: 'health', labelKey: 'app.tabs.healthCenter', route: '/health', icon: 'mdi-stethoscope' },
    { value: 'subscriptions', labelKey: 'app.tabs.subscriptions', route: '/subscriptions', icon: 'mdi-cash-multiple' },
    { value: 'cockpit', labelKey: 'app.tabs.cockpitOverview', route: '/cockpit', icon: 'mdi-view-dashboard-outline' },
    { value: 'autopilot', labelKey: 'app.tabs.autopilot', route: '/autopilot', icon: 'mdi-steering' },
    { value: 'console', labelKey: 'app.tabs.console', route: '/console', icon: 'mdi-console' },
    { value: 'cost-report', labelKey: 'app.tabs.costReport', route: '/cost-report', icon: 'mdi-finance' }
  ] as const

  const translatedApiTabOptions = computed(() => {
    return apiTabOptions.map(tab => ({
      ...tab,
      label: t(tab.labelKey)
    }))
  })

  const currentTabLabel = computed(() => {
    return (
      translatedApiTabOptions.value.find(tab => tab.value === channelStore.activeTab)?.label || channelStore.activeTab
    )
  })

  const activeTrafficTitle = computed(() => t('app.stats.trafficTitle', { tab: currentTabLabel.value }))
  const editingChannelType = computed<ChannelKind>(() => {
    return dialogStore.editingChannel?.routeKind ?? channelStore.activeTab
  })

  const getChannelRouteKind = (channel?: Channel | null): ChannelKind => {
    return channel?.routeKind ?? channelStore.activeTab
  }

  const getChannelRouteIndex = (channel: Channel): number => {
    return channel.routeIndex ?? channel.index
  }

  const systemStatusText = computed(() => {
    switch (systemStore.systemStatus) {
      case 'running':
        return t('system.running')
      case 'error':
        return t('system.error')
      case 'connecting':
        return t('system.connecting')
      default:
        return t('system.unknown')
    }
  })

  const systemStatusDesc = computed(() => {
    switch (systemStore.systemStatus) {
      case 'running':
        return t('system.runningDesc')
      case 'error':
        return t('system.errorDesc')
      case 'connecting':
        return t('system.connectingDesc')
      default:
        return ''
    }
  })

  // 新用户指引
  const showGuide = ref(false)

  function openGuide() {
    showGuide.value = true
  }

  // 指引关闭后标记已看过，避免下次自动弹出
  watch(showGuide, open => {
    if (!open) preferencesStore.markGuideSeen()
  })

  // 首次认证成功后自动弹出一次指引
  // 直接 watch authStore（在前文已定义），避免引用尚未声明的 isAuthenticated computed
  watch(
    () => authStore.isAuthenticated,
    authed => {
      if (authed && !preferencesStore.hasSeenGuide) {
        showGuide.value = true
      }
    }
  )

  // 监听 Tab 切换，刷新对应数据
  watch(
    () => channelStore.activeTab,
    async () => {
      if (authStore.isAuthenticated) {
        try {
          await channelStore.refreshChannels()
        } catch (error) {
          console.error('切换 Tab 刷新失败:', error)
        }
      }
    }
  )

  return {
    route,
    t,
    setLocale,
    languageOptions,
    currentLocale,
    currentLanguageShortLabel,
    translatedApiTabOptions,
    editingChannelType,
    activeTrafficTitle,
    systemStatusText,
    systemStatusDesc,
    getChannelRouteKind,
    getChannelRouteIndex,
    showGuide,
    openGuide
  }
}
