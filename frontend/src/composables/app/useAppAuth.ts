import { computed, watch, onMounted, onUnmounted } from 'vue'
import { api, ApiError } from '../../services/api'
import { useAuthStore } from '../../stores/auth'
import { useChannelStore } from '../../stores/channel'
import { useSystemStore } from '../../stores/system'
import type { useI18n } from '../../i18n'
import type { useToasts } from '../useToasts'

interface AuthOptions {
  t: ReturnType<typeof useI18n>['t']
  showToast: ReturnType<typeof useToasts>['showToast']
}

// 认证、渠道连接状态与自动刷新共同随根组件生命周期启停。
export function useAppAuth({ t, showToast }: AuthOptions) {
  const authStore = useAuthStore()
  const channelStore = useChannelStore()
  const systemStore = useSystemStore()

  // 主要功能函数 - 使用 ChannelStore
  const refreshChannels = async () => {
    try {
      await channelStore.refreshChannels()
    } catch (error) {
      handleAuthError(error)
    }
  }

  // 认证状态管理（使用 AuthStore）
  const isAuthenticated = computed(() => authStore.isAuthenticated)
  // 认证相关状态已迁移到 AuthStore

  // 认证尝试限制
  const MAX_AUTH_ATTEMPTS = 5

  const getAuthLockoutRemainingSeconds = () => {
    const lockoutTime = authStore.authLockoutTime
    if (!lockoutTime) return 0

    const remainingSeconds = Math.ceil((lockoutTime - Date.now()) / 1000)
    if (remainingSeconds <= 0) {
      authStore.setAuthLockout(null)
      authStore.resetAuthAttempts()
      return 0
    }
    return remainingSeconds
  }

  // 控制认证对话框显示
  const showAuthDialog = computed({
    get: () => {
      // 只有在初始化完成后，且未认证，且不在自动认证中时，才显示对话框
      return authStore.isInitialized && !isAuthenticated.value && !authStore.isAutoAuthenticating
    },
    set: () => {} // 防止外部修改，认证状态只能通过内部逻辑控制
  })

  // 自动验证保存的密钥
  const autoAuthenticate = async () => {
    // 检查 AuthStore 中是否有保存的密钥
    if (!authStore.apiKey) {
      // 没有保存的密钥，显示登录对话框
      authStore.setAuthError(t('toast.enterAccessKeyContinue'))
      authStore.setAutoAuthenticating(false)
      authStore.setInitialized(true)
      return false
    }

    // 有保存的密钥，尝试自动认证
    try {
      // 尝试调用API验证密钥是否有效
      await api.getChannels()

      // 密钥有效，认证成功
      authStore.setAuthError('')
      return true
    } catch (error) {
      // 仅在明确 401 时视为密钥无效；其他错误（网络/5xx）不应清除密钥
      if (error instanceof ApiError && error.status === 401) {
        console.warn('自动认证失败: 认证失败(401)')
        authStore.clearAuth()
        authStore.setAuthError(t('toast.savedKeyInvalid'))
        return false
      }

      console.warn('自动认证暂时失败:', error)
      showToast(
        t('toast.cannotVerifyAccessKey', { message: error instanceof Error ? error.message : t('system.unknown') }),
        'warning'
      )
      // 非 401：保留密钥，继续尝试连接后端（后续刷新会更新系统状态）
      return true
    } finally {
      authStore.setAutoAuthenticating(false)
      authStore.setInitialized(true)
    }
  }

  // 手动设置密钥（用于重新认证）
  const setAuthKey = (key: string) => {
    authStore.setApiKey(key)
    authStore.setAuthError('')
  }

  // 处理认证提交
  const submitAuth = async () => {
    if (!authStore.authKeyInput.trim()) {
      authStore.setAuthError(t('toast.enterAccessKey'))
      return
    }

    // 检查是否被锁定；过期锁定会自动清理，避免显示负数倒计时
    const remainingSeconds = getAuthLockoutRemainingSeconds()
    if (remainingSeconds > 0) {
      authStore.setAuthError(t('toast.tooManyAttemptsSeconds', { seconds: remainingSeconds }))
      return
    }

    authStore.setAuthLoading(true)
    authStore.setAuthError('')

    try {
      // 设置密钥
      setAuthKey(authStore.authKeyInput.trim())

      // 测试API调用以验证密钥
      await api.getChannels()

      // 认证成功，重置计数器
      authStore.resetAuthAttempts()
      authStore.setAuthLockout(null)

      // 如果成功，加载数据
      await refreshChannels()
      // 手动登录成功后同步系统状态，避免状态卡停留在 Connecting
      systemStore.setSystemStatus(channelStore.lastRefreshSuccess ? 'running' : 'error')

      authStore.setAuthKeyInput('')

      // 记录认证成功(前端日志)
      if (import.meta.env.DEV) {
        console.info('✅ 认证成功 - 时间:', new Date().toISOString())
      }
    } catch (error) {
      // 仅在明确 401 时计入认证失败；网络/5xx 不计入失败次数，也不清除已保存密钥
      if (error instanceof ApiError && error.status === 401) {
        authStore.incrementAuthAttempts()

        // 记录认证失败(前端日志)
        console.warn('🔒 认证失败 - 尝试次数:', authStore.authAttempts, '时间:', new Date().toISOString())

        // 如果尝试次数过多，锁定5分钟
        if (authStore.authAttempts >= MAX_AUTH_ATTEMPTS) {
          authStore.setAuthLockout(new Date(Date.now() + 5 * 60 * 1000))
          authStore.setAuthError(t('toast.tooManyAttempts'))
        } else {
          authStore.setAuthError(
            t('toast.accessKeyInvalidRemaining', { remaining: MAX_AUTH_ATTEMPTS - authStore.authAttempts })
          )
        }

        authStore.clearAuth()
        return
      }

      showToast(
        t('toast.cannotVerifyAccessKey', { message: error instanceof Error ? error.message : t('system.unknown') }),
        'error'
      )
    } finally {
      authStore.setAuthLoading(false)
    }
  }

  const handleAuthSubmit = async () => {
    await submitAuth()
  }

  // 处理注销
  const handleLogout = () => {
    authStore.clearAuth()
    channelStore.clearChannels()
    authStore.setAuthError(t('toast.enterAccessKeyContinue'))
    showToast(t('toast.loggedOut'), 'info')
  }

  // 处理认证失败
  const handleAuthError = (error: unknown) => {
    if (error instanceof ApiError && error.status === 401) {
      authStore.setAuthError(t('toast.authInvalid'))
    } else {
      showToast(
        t('toast.operationFailed', { message: error instanceof Error ? error.message : t('system.unknown') }),
        'error'
      )
    }
  }

  // 启动自动刷新定时器
  const startAutoRefresh = () => {
    channelStore.startAutoRefresh()
  }

  // 停止自动刷新定时器
  const stopAutoRefresh = () => {
    channelStore.stopAutoRefresh()
  }

  // 监听认证状态变化
  watch(isAuthenticated, newValue => {
    if (newValue) {
      startAutoRefresh()
    } else {
      stopAutoRefresh()
    }
  })

  // 监听自动刷新状态，更新 systemStatus
  watch(
    () => channelStore.lastRefreshSuccess,
    success => {
      if (isAuthenticated.value) {
        systemStore.setSystemStatus(success ? 'running' : 'error')
      }
    }
  )

  onMounted(async () => {
    // 检查 AuthStore 中是否有保存的密钥
    if (authStore.apiKey) {
      // 有保存的密钥，开始自动认证
      authStore.setAutoAuthenticating(true)
      authStore.setInitialized(false)
    } else {
      // 没有保存的密钥，直接显示登录对话框
      authStore.setAutoAuthenticating(false)
      authStore.setInitialized(true)
    }

    // 尝试自动认证
    const authenticated = await autoAuthenticate()

    if (authenticated) {
      // 加载渠道数据
      await refreshChannels()
      // 启动自动刷新
      startAutoRefresh()
      // 初始化完成后根据最新刷新结果设置系统状态
      systemStore.setSystemStatus(channelStore.lastRefreshSuccess ? 'running' : 'error')
    }
  })

  onUnmounted(() => channelStore.stopAutoRefresh())

  return {
    isAuthenticated,
    MAX_AUTH_ATTEMPTS,
    showAuthDialog,
    handleAuthSubmit,
    handleLogout,
    handleAuthError,
    refreshChannels
  }
}
