import { ref, watch } from 'vue'
import { useI18n } from '../../../i18n'
import { ApiError, ApiService } from '../../../services/api'
import type { DeepSeekCredentialBalance, ManagedAccountCredential } from '../../../services/api-types'
import { maskApiKey } from '../../../utils/apiKeyMask'

interface ManagedKeyCredentialProps {
  providerId?: string
  accountUid?: string
  apiKeys: string[]
}

// 凭证编辑与套餐刷新按账户管理；Key 列表的排序、倍率和分组策略由编辑器负责。
export function useManagedKeyCredentials(
  props: Readonly<ManagedKeyCredentialProps>,
  updateApiKeys: (keys: string[]) => void,
) {
  const { t } = useI18n()
  const apiService = new ApiService()

  const deepseekBalances = ref<DeepSeekCredentialBalance[]>([])
  const deepseekBalancesLoading = ref(false)
  const deepseekBalancesError = ref('')
  interface VolcengineCredentialForm {
    accessKeyId: string
    secretAccessKey: string
    saving: boolean
    clearing: boolean
    error: string
  }

  const volcengineCredentials = ref<ManagedAccountCredential[]>([])
  const volcengineCredentialsLoading = ref(false)
  const volcengineCredentialsError = ref('')
  const volcengineForms = ref<Record<string, VolcengineCredentialForm>>({})
  const volcengineUsageRefreshing = ref<Record<string, boolean>>({})
  interface KimiCredentialForm {
    accessToken: string
    saving: boolean
    refreshing: boolean
    clearing: boolean
    error: string
  }

  const kimiCredentials = ref<ManagedAccountCredential[]>([])
  const kimiCredentialsLoading = ref(false)
  const kimiCredentialsError = ref('')
  const kimiForms = ref<Record<string, KimiCredentialForm>>({})
  interface MiMoCredentialForm {
    cookie: string
    saving: boolean
    refreshing: boolean
    clearing: boolean
    error: string
  }

  const mimoCredentials = ref<ManagedAccountCredential[]>([])
  const mimoCredentialsLoading = ref(false)
  const mimoCredentialsError = ref('')
  const mimoForms = ref<Record<string, MiMoCredentialForm>>({})
  interface CompshareCredentialForm {
    cookie: string
    saving: boolean
    refreshing: boolean
    clearing: boolean
    error: string
  }

  const compshareCredentials = ref<ManagedAccountCredential[]>([])
  const compshareCredentialsLoading = ref(false)
  const compshareCredentialsError = ref('')
  const compshareForms = ref<Record<string, CompshareCredentialForm>>({})

  const loadDeepSeekBalances = async () => {
    deepseekBalances.value = []
    deepseekBalancesError.value = ''
    if (props.providerId !== 'deepseek' || !props.accountUid) return
    deepseekBalancesLoading.value = true
    try {
      const response = await apiService.getDeepSeekAccountBalances(props.accountUid)
      deepseekBalances.value = response.balances
    } catch (err) {
      deepseekBalancesError.value = err instanceof Error ? err.message : String(err)
    } finally {
      deepseekBalancesLoading.value = false
    }
  }

  watch(
    () => [props.providerId, props.accountUid],
    () => { void loadDeepSeekBalances() },
    { immediate: true }
  )

  const loadVolcengineCredentials = async () => {
    volcengineCredentials.value = []
    volcengineCredentialsError.value = ''
    if (props.providerId !== 'volcengine' || !props.accountUid) return
    volcengineCredentialsLoading.value = true
    try {
      const response = await apiService.getManagedAccounts()
      const account = response.accounts.find(item => item.accountUid === props.accountUid)
      if (!account) {
        volcengineCredentialsError.value = t('volcengineAccessKey.accountNotFound')
        return
      }
      volcengineCredentials.value = account.credentials
      const nextForms: Record<string, VolcengineCredentialForm> = {}
      for (const credential of account.credentials) {
        nextForms[credential.credentialUid] = volcengineForms.value[credential.credentialUid] ?? {
          accessKeyId: '',
          secretAccessKey: '',
          saving: false,
          clearing: false,
          error: '',
        }
      }
      volcengineForms.value = nextForms
      // 打开编辑框时对已绑定 AK 的凭证自动刷新用量（后端 TTL 兜底避免频繁请求）。
      void Promise.allSettled(
        account.credentials
          .filter(credential => credential.hasVolcengineAccessKey)
          .map(credential => refreshVolcengineUsage(credential))
      )
    } catch (err) {
      volcengineCredentialsError.value = err instanceof Error ? err.message : String(err)
    } finally {
      volcengineCredentialsLoading.value = false
    }
  }

  watch(
    () => [props.providerId, props.accountUid],
    () => { void loadVolcengineCredentials() },
    { immediate: true }
  )

  const loadKimiCredentials = async () => {
    kimiCredentials.value = []
    kimiCredentialsError.value = ''
    if (props.providerId !== 'kimi' || !props.accountUid) return
    kimiCredentialsLoading.value = true
    try {
      const response = await apiService.getManagedAccounts()
      const account = response.accounts.find(item => item.accountUid === props.accountUid)
      if (!account) {
        kimiCredentialsError.value = t('kimiConsoleToken.accountNotFound')
        return
      }
      kimiCredentials.value = account.credentials
      const nextForms: Record<string, KimiCredentialForm> = {}
      for (const credential of account.credentials) {
        nextForms[credential.credentialUid] = kimiForms.value[credential.credentialUid] ?? {
          accessToken: '',
          saving: false,
          refreshing: false,
          clearing: false,
          error: '',
        }
      }
      kimiForms.value = nextForms
    } catch (err) {
      kimiCredentialsError.value = err instanceof Error ? err.message : String(err)
    } finally {
      kimiCredentialsLoading.value = false
    }
  }

  watch(
    () => [props.providerId, props.accountUid],
    () => { void loadKimiCredentials() },
    { immediate: true }
  )

  const loadMiMoCredentials = async () => {
    mimoCredentials.value = []
    mimoCredentialsError.value = ''
    if (props.providerId !== 'mimo' || !props.accountUid) return
    mimoCredentialsLoading.value = true
    try {
      const response = await apiService.getManagedAccounts()
      const account = response.accounts.find(item => item.accountUid === props.accountUid)
      if (!account) {
        mimoCredentialsError.value = t('mimoConsoleCookie.accountNotFound')
        return
      }
      mimoCredentials.value = account.credentials
      const nextForms: Record<string, MiMoCredentialForm> = {}
      for (const credential of account.credentials) {
        nextForms[credential.credentialUid] = mimoForms.value[credential.credentialUid] ?? {
          cookie: '', saving: false, refreshing: false, clearing: false, error: '',
        }
      }
      mimoForms.value = nextForms
    } catch (err) {
      mimoCredentialsError.value = err instanceof Error ? err.message : String(err)
    } finally {
      mimoCredentialsLoading.value = false
    }
  }

  watch(
    () => [props.providerId, props.accountUid],
    () => { void loadMiMoCredentials() },
    { immediate: true }
  )

  const loadCompshareCredentials = async () => {
    compshareCredentials.value = []
    compshareCredentialsError.value = ''
    if (props.providerId !== 'compshare' || !props.accountUid) return
    compshareCredentialsLoading.value = true
    try {
      const response = await apiService.getManagedAccounts()
      const account = response.accounts.find(item => item.accountUid === props.accountUid)
      if (!account) {
        compshareCredentialsError.value = t('compshareConsoleCookie.accountNotFound')
        return
      }
      compshareCredentials.value = account.credentials
      const nextForms: Record<string, CompshareCredentialForm> = {}
      for (const credential of account.credentials) {
        nextForms[credential.credentialUid] = compshareForms.value[credential.credentialUid] ?? {
          cookie: '',
          saving: false,
          refreshing: false,
          clearing: false,
          error: '',
        }
      }
      compshareForms.value = nextForms
    } catch (err) {
      compshareCredentialsError.value = err instanceof Error ? err.message : String(err)
    } finally {
      compshareCredentialsLoading.value = false
    }
  }

  watch(
    () => [props.providerId, props.accountUid],
    () => { void loadCompshareCredentials() },
    { immediate: true }
  )

  const saveCompshareCookie = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = compshareForms.value[credential.credentialUid]
    if (!form?.cookie.trim()) return
    form.saving = true
    form.error = ''
    try {
      const response = await apiService.setCompshareConsoleCookie(props.accountUid, credential.credentialUid, form.cookie.trim())
      credential.hasCompshareConsoleCookie = true
      credential.compsharePlan = response.plan
      form.cookie = ''
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.saving = false
    }
  }

  const refreshCompshareCookie = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = compshareForms.value[credential.credentialUid]
    if (!form) return
    form.refreshing = true
    form.error = ''
    try {
      const response = await apiService.refreshCompshareConsoleCookie(props.accountUid, credential.credentialUid)
      credential.compsharePlan = response.plan
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.refreshing = false
    }
  }

  const clearCompshareCookie = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid || !window.confirm(t('compshareConsoleCookie.clearConfirm'))) return
    const form = compshareForms.value[credential.credentialUid]
    if (!form) return
    form.clearing = true
    form.error = ''
    try {
      await apiService.clearCompshareConsoleCookie(props.accountUid, credential.credentialUid)
      credential.hasCompshareConsoleCookie = false
      credential.compsharePlan = undefined
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.clearing = false
    }
  }

  const applyMiMoCookieResponse = (credential: ManagedAccountCredential, response: Awaited<ReturnType<ApiService['setMiMoConsoleCookie']>>) => {
    if (response.keyAdopted && response.adoptedApiKey) {
      const keyIndex = props.apiKeys.findIndex(key => maskApiKey(key) === credential.keyMask)
      if (keyIndex >= 0) {
        const updated = [...props.apiKeys]
        updated[keyIndex] = response.adoptedApiKey
        updateApiKeys(updated)
      }
      credential.keyMask = response.keyMask
    }
    credential.hasMiMoConsoleCookie = true
    credential.mimoTokenPlan = response.tokenPlan
    mimoForms.value[credential.credentialUid].cookie = ''
  }

  const bindMiMoConsoleCookie = async (credential: ManagedAccountCredential, adoptCookieKey: boolean) => {
    if (!props.accountUid) return
    const form = mimoForms.value[credential.credentialUid]
    const response = await apiService.setMiMoConsoleCookie(props.accountUid, credential.credentialUid, {
      cookie: form.cookie.trim(),
      adoptCookieKey,
    })
    applyMiMoCookieResponse(credential, response)
  }

  const saveMiMoConsoleCookie = async (credential: ManagedAccountCredential) => {
    const form = mimoForms.value[credential.credentialUid]
    if (!form?.cookie.trim()) return
    form.saving = true
    form.error = ''
    try {
      await bindMiMoConsoleCookie(credential, false)
    } catch (err) {
      const details = err instanceof ApiError && typeof err.details === 'object' && err.details
        ? err.details as { code?: string; currentKeyMask?: string; cookieKeyMask?: string }
        : null
      if (err instanceof ApiError && err.status === 409 && details?.code === 'mimo_cookie_key_mismatch') {
        const confirmed = window.confirm(t('mimoConsoleCookie.keyMismatchConfirm', {
          currentKey: details.currentKeyMask ?? '-',
          cookieKey: details.cookieKeyMask ?? '-',
        }))
        if (confirmed) {
          try {
            await bindMiMoConsoleCookie(credential, true)
            return
          } catch (adoptError) {
            form.error = adoptError instanceof Error ? adoptError.message : String(adoptError)
            return
          }
        }
      }
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.saving = false
    }
  }

  const refreshMiMoConsoleCookie = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = mimoForms.value[credential.credentialUid]
    form.refreshing = true
    form.error = ''
    try {
      const response = await apiService.refreshMiMoConsoleCookie(props.accountUid, credential.credentialUid)
      credential.mimoTokenPlan = response.tokenPlan
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.refreshing = false
    }
  }

  const clearMiMoConsoleCookie = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid || !window.confirm(t('mimoConsoleCookie.clearConfirm'))) return
    const form = mimoForms.value[credential.credentialUid]
    form.clearing = true
    form.error = ''
    try {
      await apiService.clearMiMoConsoleCookie(props.accountUid, credential.credentialUid)
      credential.hasMiMoConsoleCookie = false
      credential.mimoTokenPlan = undefined
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.clearing = false
    }
  }

  const refreshVolcengineUsage = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid || !credential.hasVolcengineAccessKey) return
    volcengineUsageRefreshing.value[credential.credentialUid] = true
    try {
      const response = await apiService.refreshVolcenginePlanUsage(props.accountUid, credential.credentialUid)
      credential.volcenginePlanUsage = response.usage
    } catch (err) {
      // 用量刷新失败降级为快照 error，不打断编辑框其它操作。
      credential.volcenginePlanUsage = {
        fetchedAt: new Date().toISOString(),
        error: err instanceof Error ? err.message : String(err),
      }
    } finally {
      volcengineUsageRefreshing.value[credential.credentialUid] = false
    }
  }

  const canSaveVolcengineCredential = (credentialUid: string) => {
    const form = volcengineForms.value[credentialUid]
    return !!form?.accessKeyId.trim() && !!form?.secretAccessKey.trim() && !form.saving
  }

  const saveVolcengineAccessKey = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = volcengineForms.value[credential.credentialUid]
    if (!form || !canSaveVolcengineCredential(credential.credentialUid)) return
    form.saving = true
    form.error = ''
    try {
      const response = await apiService.setVolcengineAccessKey(props.accountUid, credential.credentialUid, {
        accessKeyId: form.accessKeyId.trim(),
        secretAccessKey: form.secretAccessKey.trim(),
      })
      credential.hasVolcengineAccessKey = true
      credential.volcengineAccessKeyIdMask = response.accessKeyIdMask
      credential.volcenginePlan = response.plan
      credential.volcenginePlanTier = response.planTier
      credential.volcenginePlanStatus = response.planStatus
      credential.volcenginePlanUsage = response.usage
      form.accessKeyId = ''
      form.secretAccessKey = ''
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.saving = false
    }
  }

  const clearVolcengineAccessKey = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid || !window.confirm(t('volcengineAccessKey.clearConfirm'))) return
    const form = volcengineForms.value[credential.credentialUid]
    if (!form) return
    form.clearing = true
    form.error = ''
    try {
      await apiService.clearVolcengineAccessKey(props.accountUid, credential.credentialUid)
      credential.hasVolcengineAccessKey = false
      credential.volcengineAccessKeyIdMask = undefined
      credential.volcenginePlan = undefined
      credential.volcenginePlanTier = undefined
      credential.volcenginePlanStatus = undefined
      credential.volcenginePlanUsage = undefined
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.clearing = false
    }
  }

  const saveKimiToken = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = kimiForms.value[credential.credentialUid]
    if (!form?.accessToken.trim()) return
    form.saving = true
    form.error = ''
    try {
      const response = await apiService.setKimiConsoleToken(props.accountUid, credential.credentialUid, form.accessToken.trim())
      credential.hasKimiConsoleToken = true
      credential.kimiCodeUsage = response.usage
      form.accessToken = ''
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.saving = false
    }
  }

  const refreshKimiToken = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid) return
    const form = kimiForms.value[credential.credentialUid]
    if (!form) return
    form.refreshing = true
    form.error = ''
    try {
      const response = await apiService.refreshKimiConsoleToken(props.accountUid, credential.credentialUid)
      credential.kimiCodeUsage = response.usage
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.refreshing = false
    }
  }

  const clearKimiToken = async (credential: ManagedAccountCredential) => {
    if (!props.accountUid || !window.confirm(t('kimiConsoleToken.clearConfirm'))) return
    const form = kimiForms.value[credential.credentialUid]
    if (!form) return
    form.clearing = true
    form.error = ''
    try {
      await apiService.clearKimiConsoleToken(props.accountUid, credential.credentialUid)
      credential.hasKimiConsoleToken = false
      credential.kimiCodeUsage = undefined
    } catch (err) {
      form.error = err instanceof Error ? err.message : String(err)
    } finally {
      form.clearing = false
    }
  }

  return {
    deepseekBalances,
    deepseekBalancesLoading,
    deepseekBalancesError,
    volcengineCredentials,
    volcengineCredentialsLoading,
    volcengineCredentialsError,
    volcengineForms,
    volcengineUsageRefreshing,
    kimiCredentials,
    kimiCredentialsLoading,
    kimiCredentialsError,
    kimiForms,
    mimoCredentials,
    mimoCredentialsLoading,
    mimoCredentialsError,
    mimoForms,
    compshareCredentials,
    compshareCredentialsLoading,
    compshareCredentialsError,
    compshareForms,
    loadDeepSeekBalances,
    saveCompshareCookie,
    refreshCompshareCookie,
    clearCompshareCookie,
    saveMiMoConsoleCookie,
    refreshMiMoConsoleCookie,
    clearMiMoConsoleCookie,
    refreshVolcengineUsage,
    canSaveVolcengineCredential,
    saveVolcengineAccessKey,
    clearVolcengineAccessKey,
    saveKimiToken,
    refreshKimiToken,
    clearKimiToken,
  }
}
