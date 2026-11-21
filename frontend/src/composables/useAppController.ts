// 根组件的兼容入口：组合应用职责，保留渠道操作与熔断器表单。
import { useAppAuth } from './app/useAppAuth'
import { useAppNavigation } from './app/useAppNavigation'
import { useAppThemeController } from './app/useAppTheme'
import { useAppVersion } from './app/useAppVersion'
import { ref, reactive, computed } from 'vue'
import { api, ApiError, type Channel, type ChannelPlacement } from '../services/api'
import { useAuthStore } from '../stores/auth'
import { useChannelStore } from '../stores/channel'
import { usePreferencesStore } from '../stores/preferences'
import { useDialogStore } from '../stores/dialog'
import { useSystemStore } from '../stores/system'
import { useDialogHotkeys } from './useDialogHotkeys'
import { useToasts } from './useToasts'
import { streamTimeoutPresets as sharedStreamPresets } from '../utils/streamTimeoutPresets'
import { isAutoManagedAccountChannel } from '../utils/providerDisplay'

export function useAppController() {
  const authStore = useAuthStore()

  // 渠道 Store
  const channelStore = useChannelStore()

  // 偏好设置 Store
  const preferencesStore = usePreferencesStore()

  // 对话框 Store
  const dialogStore = useDialogStore()

  // 系统状态 Store
  const systemStore = useSystemStore()

  const { toasts, getToastColor, getToastIcon, showToast, showErrorToast, showSuccessToast } = useToasts()

  const { theme, toggleDarkMode } = useAppThemeController()
  const { handleVersionClick } = useAppVersion()
  const {
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
  } = useAppNavigation()
  const {
    isAuthenticated,
    MAX_AUTH_ATTEMPTS,
    showAuthDialog,
    handleAuthSubmit,
    handleLogout,
    handleAuthError,
    refreshChannels
  } = useAppAuth({ t, showToast })

  const saveChannel = async (
    channel: Omit<Channel, 'index' | 'latency' | 'status'>,
    options?: { isQuickAdd?: boolean; placement?: ChannelPlacement; skipVerify?: boolean },
    onComplete?: () => void
  ) => {
    try {
      const editingChannel = dialogStore.editingChannel as Channel | null
      const result = await channelStore.saveChannel(
        channel,
        editingChannel ? getChannelRouteIndex(editingChannel) : null,
        {
          ...options,
          channelType: getChannelRouteKind(editingChannel),
          autoManaged: isAutoManagedAccountChannel(editingChannel),
          accountUid: editingChannel?.accountUid,
          originalChannel: editingChannel ?? undefined
        }
      )
      showToast(result.message, 'success')
      if (result.quickAddMessage) {
        showToast(result.quickAddMessage, 'info')
      }
      // 新增 Key 探测被降级放行时展示警告（非鉴权类失败，key 已保存但连通性未确认）
      if (result.warnings) {
        for (const warning of result.warnings) {
          showToast(warning, 'warning')
        }
      }
      dialogStore.closeAddChannelModal()
      dialogStore.closeEditChannelModal()
      // 写路径全程持锁（变更→RebuildLogicalChannels→落盘→提交内存→响应），
      // 响应到达时数据已一致，立即刷新即可；逻辑卡分裂的根因在重建归组（见后端
      // logical_channel.go 第 5 步吸收逻辑），延迟刷新无法修复。
      await refreshChannels()

      return result
    } catch (error) {
      // 新增 Key 验证失败（如鉴权失败 401/403）：提供"仍要保存"兜底入口，确认后跳过验证重试。
      // onComplete 由外层 finally 统一执行，递归重试时不再透传，避免重复触发。
      if (
        !options?.skipVerify &&
        error instanceof ApiError &&
        (error.details as { verifyFailure?: unknown } | null)?.verifyFailure === true
      ) {
        const saveAnyway = await dialogStore.confirm({
          message: t('toast.verifyFailureSaveAnyway', { error: error.message }),
          confirmText: t('app.actions.saveAnyway'),
          color: 'warning'
        })
        if (saveAnyway) {
          return await saveChannel(channel, { ...options, skipVerify: true })
        }
        return undefined
      }
      handleAuthError(error)
      return undefined
    } finally {
      onComplete?.()
    }
  }

  const handleAutoAddedChannel = async (_channelId: number) => {
    dialogStore.closeAddChannelModal()
    showToast(t('store.channel.added'), 'success')
    await refreshChannels()
  }

  const editChannel = (channel: Channel) => {
    dialogStore.openEditChannelModal(channel)
  }

  const deleteChannel = async (target: number | Channel) => {
    const ok = await dialogStore.confirm({
      message: t('toast.confirmDeleteChannel'),
      confirmText: t('app.actions.delete')
    })
    if (!ok) return

    try {
      const channelId = typeof target === 'number' ? target : getChannelRouteIndex(target)
      const channelType = typeof target === 'number' ? channelStore.activeTab : getChannelRouteKind(target)
      const accountUid =
        typeof target === 'number' || !isAutoManagedAccountChannel(target) ? undefined : target.accountUid
      const logicalChannelUid = typeof target !== 'number' ? target.logicalChannelUid : undefined
      const result = await channelStore.deleteChannel(channelId, channelType, accountUid, logicalChannelUid)
      showToast(result.message, 'success')
    } catch (error) {
      handleAuthError(error)
    }
  }

  const openAddChannelModal = () => {
    dialogStore.openAddChannelModal()
  }

  const _openAddKeyModal = (channelId: number) => {
    dialogStore.openAddKeyModal(channelId)
  }

  const addApiKey = async () => {
    if (!dialogStore.newApiKey.trim()) return

    try {
      if (channelStore.activeTab === 'chat') {
        await api.addChatApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      } else if (channelStore.activeTab === 'vectors') {
        await api.addVectorsApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      } else if (channelStore.activeTab === 'images') {
        await api.addImagesApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      } else if (channelStore.activeTab === 'gemini') {
        await api.addGeminiApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      } else if (channelStore.activeTab === 'responses') {
        await api.addResponsesApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      } else {
        await api.addApiKey(dialogStore.selectedChannelForKey, dialogStore.newApiKey.trim())
      }
      showToast(t('toast.apiKeyAdded'), 'success')
      dialogStore.closeAddKeyModal()
      await refreshChannels()
    } catch (error) {
      showToast(
        t('toast.apiKeyAddFailed', { message: error instanceof Error ? error.message : t('system.unknown') }),
        'error'
      )
    }
  }

  const _removeApiKey = async (channelId: number, apiKey: string) => {
    const ok = await dialogStore.confirm({
      message: t('toast.confirmDeleteApiKey'),
      confirmText: t('app.actions.delete')
    })
    if (!ok) return

    try {
      if (channelStore.activeTab === 'chat') {
        await api.removeChatApiKey(channelId, apiKey)
      } else if (channelStore.activeTab === 'vectors') {
        await api.removeVectorsApiKey(channelId, apiKey)
      } else if (channelStore.activeTab === 'images') {
        await api.removeImagesApiKey(channelId, apiKey)
      } else if (channelStore.activeTab === 'gemini') {
        await api.removeGeminiApiKey(channelId, apiKey)
      } else if (channelStore.activeTab === 'responses') {
        await api.removeResponsesApiKey(channelId, apiKey)
      } else {
        await api.removeApiKey(channelId, apiKey)
      }
      showToast(t('toast.apiKeyDeleted'), 'success')
      await refreshChannels()
    } catch (error) {
      showToast(
        t('toast.apiKeyDeleteFailed', { message: error instanceof Error ? error.message : t('system.unknown') }),
        'error'
      )
    }
  }

  // 熔断器配置
  const circuitBreakerDialogOpen = ref(false)
  const cbSaving = ref(false)
  const activePreset = ref('balanced')
  const cbForm = reactive({
    windowSize: 20,
    failureThreshold: 0.7,
    consecutiveFailuresThreshold: 5,
    requestTimeoutMs: 300000,
    responseHeaderTimeoutMs: 120000,
    streamFirstContentTimeoutMs: 90000,
    streamInactivityTimeoutMs: 90000,
    streamToolCallIdleTimeoutMs: 300000
  })

  // 预设向更宽松方向平移一档：原温和→均衡、原均衡→激进、原激进移除（需手动自定义）
  // 新增更宽松的温和策略，降低新用户误熔断概率
  const cbPresets = [
    {
      key: 'gentle',
      labelKey: 'dialog.circuitBreaker.presetGentle' as const,
      windowSize: 50,
      failureThreshold: 0.85,
      consecutiveFailuresThreshold: 10,
      requestTimeoutMs: 300000,
      responseHeaderTimeoutMs: 300000,
      streamFirstContentTimeoutMs: sharedStreamPresets.gentle.firstContentMs,
      streamInactivityTimeoutMs: sharedStreamPresets.gentle.inactivityMs,
      streamToolCallIdleTimeoutMs: sharedStreamPresets.gentle.toolCallIdleMs
    },
    {
      key: 'balanced',
      labelKey: 'dialog.circuitBreaker.presetBalanced' as const,
      windowSize: 20,
      failureThreshold: 0.7,
      consecutiveFailuresThreshold: 5,
      requestTimeoutMs: 300000,
      responseHeaderTimeoutMs: 120000,
      streamFirstContentTimeoutMs: sharedStreamPresets.balanced.firstContentMs,
      streamInactivityTimeoutMs: sharedStreamPresets.balanced.inactivityMs,
      streamToolCallIdleTimeoutMs: sharedStreamPresets.balanced.toolCallIdleMs
    },
    {
      key: 'aggressive',
      labelKey: 'dialog.circuitBreaker.presetAggressive' as const,
      windowSize: 10,
      failureThreshold: 0.5,
      consecutiveFailuresThreshold: 3,
      requestTimeoutMs: 120000,
      responseHeaderTimeoutMs: 60000,
      streamFirstContentTimeoutMs: sharedStreamPresets.aggressive.firstContentMs,
      streamInactivityTimeoutMs: sharedStreamPresets.aggressive.inactivityMs,
      streamToolCallIdleTimeoutMs: sharedStreamPresets.aggressive.toolCallIdleMs
    },
    {
      key: 'custom',
      labelKey: 'dialog.circuitBreaker.presetCustom' as const,
      windowSize: 20,
      failureThreshold: 0.7,
      consecutiveFailuresThreshold: 5,
      requestTimeoutMs: 300000,
      responseHeaderTimeoutMs: 120000,
      streamFirstContentTimeoutMs: sharedStreamPresets.balanced.firstContentMs,
      streamInactivityTimeoutMs: sharedStreamPresets.balanced.inactivityMs,
      streamToolCallIdleTimeoutMs: sharedStreamPresets.balanced.toolCallIdleMs
    }
  ]

  const matchPreset = () => {
    for (const p of cbPresets) {
      if (p.key === 'custom') continue
      if (
        cbForm.windowSize === p.windowSize &&
        cbForm.failureThreshold === p.failureThreshold &&
        cbForm.consecutiveFailuresThreshold === p.consecutiveFailuresThreshold &&
        cbForm.requestTimeoutMs === p.requestTimeoutMs &&
        cbForm.responseHeaderTimeoutMs === p.responseHeaderTimeoutMs &&
        cbForm.streamFirstContentTimeoutMs === p.streamFirstContentTimeoutMs &&
        cbForm.streamInactivityTimeoutMs === p.streamInactivityTimeoutMs &&
        cbForm.streamToolCallIdleTimeoutMs === p.streamToolCallIdleTimeoutMs
      ) {
        activePreset.value = p.key
        return
      }
    }
    activePreset.value = 'custom'
  }

  const applyPreset = (preset: (typeof cbPresets)[number]) => {
    if (preset.key === 'custom') return
    cbForm.windowSize = preset.windowSize
    cbForm.failureThreshold = preset.failureThreshold
    cbForm.consecutiveFailuresThreshold = preset.consecutiveFailuresThreshold
    cbForm.requestTimeoutMs = preset.requestTimeoutMs
    cbForm.responseHeaderTimeoutMs = preset.responseHeaderTimeoutMs
    cbForm.streamFirstContentTimeoutMs = preset.streamFirstContentTimeoutMs
    cbForm.streamInactivityTimeoutMs = preset.streamInactivityTimeoutMs
    cbForm.streamToolCallIdleTimeoutMs = preset.streamToolCallIdleTimeoutMs
    activePreset.value = preset.key
  }

  const onSliderChange = (field: string, event: Event) => {
    const target = event.target
    if (!(target instanceof window.HTMLInputElement)) return
    const val = Number(target.value)
    if (field === 'failureThreshold') {
      cbForm.failureThreshold = Math.round(val * 100) / 100
    } else if (field === 'windowSize') {
      cbForm.windowSize = val
    } else if (field === 'consecutiveFailuresThreshold') {
      cbForm.consecutiveFailuresThreshold = val
    } else if (field === 'requestTimeoutMs') {
      cbForm.requestTimeoutMs = val
    } else if (field === 'responseHeaderTimeoutMs') {
      cbForm.responseHeaderTimeoutMs = val
    } else if (field === 'streamFirstContentTimeoutMs') {
      cbForm.streamFirstContentTimeoutMs = val
    } else if (field === 'streamInactivityTimeoutMs') {
      cbForm.streamInactivityTimeoutMs = val
    } else if (field === 'streamToolCallIdleTimeoutMs') {
      cbForm.streamToolCallIdleTimeoutMs = val
    }
    matchPreset()
  }

  const openCircuitBreakerDialog = async () => {
    try {
      const params = await api.getCircuitBreaker()
      cbForm.windowSize = params.windowSize
      cbForm.failureThreshold = params.failureThreshold
      cbForm.consecutiveFailuresThreshold = params.consecutiveFailuresThreshold
      cbForm.requestTimeoutMs =
        params.requestTimeoutMs && params.requestTimeoutMs >= 1000 ? params.requestTimeoutMs : 300000
      cbForm.responseHeaderTimeoutMs =
        params.responseHeaderTimeoutMs && params.responseHeaderTimeoutMs >= 1000
          ? params.responseHeaderTimeoutMs
          : 120000
      cbForm.streamFirstContentTimeoutMs =
        params.streamFirstContentTimeoutMs && params.streamFirstContentTimeoutMs >= 5000
          ? params.streamFirstContentTimeoutMs
          : 90000
      cbForm.streamInactivityTimeoutMs =
        params.streamInactivityTimeoutMs && params.streamInactivityTimeoutMs >= 1000
          ? params.streamInactivityTimeoutMs
          : 90000
      cbForm.streamToolCallIdleTimeoutMs =
        params.streamToolCallIdleTimeoutMs && params.streamToolCallIdleTimeoutMs >= 30000
          ? params.streamToolCallIdleTimeoutMs
          : 300000
      matchPreset()
    } catch (e) {
      console.error('Failed to load circuit breaker config:', e)
    }
    circuitBreakerDialogOpen.value = true
  }

  const saveCircuitBreaker = async () => {
    cbSaving.value = true
    try {
      await api.setCircuitBreaker({
        windowSize: cbForm.windowSize,
        failureThreshold: cbForm.failureThreshold,
        consecutiveFailuresThreshold: cbForm.consecutiveFailuresThreshold,
        requestTimeoutMs: cbForm.requestTimeoutMs,
        responseHeaderTimeoutMs: cbForm.responseHeaderTimeoutMs,
        streamFirstContentTimeoutMs: cbForm.streamFirstContentTimeoutMs,
        streamInactivityTimeoutMs: cbForm.streamInactivityTimeoutMs,
        streamToolCallIdleTimeoutMs: cbForm.streamToolCallIdleTimeoutMs
      })
      circuitBreakerDialogOpen.value = false
      showToast(t('toast.circuitBreakerSaved'), 'success')
    } catch (e) {
      showToast(
        t('toast.circuitBreakerFailed', { message: e instanceof Error ? e.message : t('system.unknown') }),
        'error'
      )
    } finally {
      cbSaving.value = false
    }
  }

  // 平台检测
  const isMac = computed(() => typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform))

  // 调校台弹窗键盘快捷键（经全局对话框快捷键栈，仅栈顶时生效）
  useDialogHotkeys(circuitBreakerDialogOpen, {
    esc: () => {
      circuitBreakerDialogOpen.value = false
    },
    confirm: () => saveCircuitBreaker()
  })

  // 添加API密钥弹窗键盘快捷键（Enter 确认添加）
  useDialogHotkeys(() => dialogStore.showAddKeyModal, {
    esc: () => dialogStore.closeAddKeyModal(),
    plainEnter: () => addApiKey()
  })

  // 通用确认弹窗键盘快捷键
  useDialogHotkeys(() => dialogStore.showConfirmDialog, {
    esc: () => dialogStore.resolveConfirm(false),
    confirm: () => dialogStore.resolveConfirm(true)
  })

  return {
    route,
    theme,
    authStore,
    channelStore,
    preferencesStore,
    dialogStore,
    systemStore,
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
    toasts,
    getToastColor,
    getToastIcon,
    showToast,
    showErrorToast,
    showSuccessToast,
    refreshChannels,
    saveChannel,
    handleAutoAddedChannel,
    editChannel,
    deleteChannel,
    openAddChannelModal,
    addApiKey,
    showGuide,
    openGuide,
    circuitBreakerDialogOpen,
    cbSaving,
    activePreset,
    cbForm,
    cbPresets,
    applyPreset,
    onSliderChange,
    openCircuitBreakerDialog,
    saveCircuitBreaker,
    isMac,
    toggleDarkMode,
    isAuthenticated,
    MAX_AUTH_ATTEMPTS,
    showAuthDialog,
    handleAuthSubmit,
    handleLogout,
    handleVersionClick
  }
}
