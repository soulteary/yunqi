<template>
        <v-alert
          v-if="serviceType === 'copilot'"
          class="mt-3"
          color="info"
          variant="tonal"
          density="comfortable"
        >
          <div class="d-flex flex-column ga-2">
            <div class="text-body-2">{{ t('copilotOAuth.description') }}</div>
            <v-text-field
              :model-value="proxyUrl || ''"
              :label="t('channelEditor.transport.proxyUrl.label')"
              :placeholder="t('channelEditor.transport.proxyUrl.placeholder')"
              :hint="t('channelEditor.transport.proxyUrl.hint')"
              prepend-inner-icon="mdi-shield-lock-outline"
              variant="outlined"
              density="compact"
              clearable
              persistent-hint
              @update:model-value="$emit('update:proxyUrl', String($event || ''))"
            />
            <div v-if="copilotUserCode" class="d-flex align-center flex-wrap ga-2">
              <span class="text-body-2">{{ t('copilotOAuth.userCode') }}</span>
              <code class="px-2 py-1 rounded bg-surface">{{ copilotUserCode }}</code>
              <v-btn
                size="small"
                color="primary"
                variant="text"
                :href="copilotVerificationUri"
                target="_blank"
                rel="noopener noreferrer"
              >
                <v-icon start size="small">mdi-open-in-new</v-icon>
                {{ t('copilotOAuth.openAuthorize') }}
              </v-btn>
            </div>
            <v-alert v-if="copilotOAuthSuccess" color="success" variant="tonal" density="compact">
              {{ t('copilotOAuth.success') }}
            </v-alert>
            <v-alert v-if="copilotOAuthError" color="error" variant="tonal" density="compact">
              {{ copilotOAuthError }}
            </v-alert>
            <div class="d-flex align-center ga-2">
              <v-btn
                color="primary"
                variant="tonal"
                :loading="copilotOAuthLoading"
                @click="startCopilotOAuth"
              >
                <v-icon start>mdi-code-braces</v-icon>
                {{ t('copilotOAuth.button') }}
              </v-btn>
              <span v-if="copilotPolling" class="text-caption text-medium-emphasis">
                {{ t('copilotOAuth.waiting') }}
              </span>
            </div>
            <div class="d-flex align-center ga-2 mt-1">
              <v-btn
                v-if="copilotPolling || copilotOAuthLoading"
                size="small"
                color="warning"
                variant="text"
                @click="cancelCopilotOAuth"
              >
                {{ t('copilotOAuth.cancel') }}
              </v-btn>
              <v-btn
                v-if="copilotOAuthError"
                size="small"
                color="primary"
                variant="text"
                @click="retryCopilotOAuth"
              >
                {{ t('copilotOAuth.retry') }}
              </v-btn>
            </div>
            <div v-if="serviceType === 'copilot' && isEditing && channelId !== undefined" class="mt-3 d-flex flex-column ga-2">
              <div class="d-flex align-center ga-2">
                <v-btn
                  color="secondary"
                  variant="tonal"
                  :loading="copilotDiagnoseLoading"
                  @click="diagnoseCopilotChannel"
                >
                  {{ t('copilotDiagnose.button') }}
                </v-btn>
                <span v-if="copilotDiagnoseLoading" class="text-caption text-medium-emphasis">
                  {{ t('copilotDiagnose.loading') }}
                </span>
              </div>
              <v-alert v-if="copilotDiagnoseError" color="error" variant="tonal" density="compact">
                {{ copilotDiagnoseError }}
              </v-alert>
              <v-alert v-if="copilotDiagnoseResult" color="info" variant="tonal" density="compact" class="text-caption">
                <div class="d-flex flex-column ga-2">
                  <div class="d-flex align-center ga-2">
                    <v-chip :color="copilotDiagnoseResult.githubUser ? 'success' : 'warning'" size="small" variant="tonal">
                      GitHub
                    </v-chip>
                    <span v-if="copilotDiagnoseResult.githubUser">{{ copilotDiagnoseResult.githubUser.login }}</span>
                    <span v-else>{{ copilotDiagnoseResult.githubUserError }}</span>
                  </div>
                  <div class="d-flex align-center ga-2">
                    <v-chip :color="copilotDiagnoseResult.tokenError ? 'error' : 'success'" size="small" variant="tonal">
                      Token
                    </v-chip>
                    <span v-if="copilotDiagnoseResult.tokenError">{{ copilotDiagnoseResult.tokenError }}</span>
                    <span v-else>{{ copilotDiagnoseResult.copilotBaseUrl }}</span>
                  </div>
                  <div class="d-flex align-center ga-2">
                    <v-chip :color="copilotDiagnoseResult.modelsError ? 'error' : (copilotDiagnoseResult.modelsStatus && copilotDiagnoseResult.modelsStatus < 400 ? 'success' : 'warning')" size="small" variant="tonal">
                      Models
                    </v-chip>
                    <span v-if="copilotDiagnoseResult.modelsError">{{ copilotDiagnoseResult.modelsError }}</span>
                    <span v-else>{{ copilotDiagnoseResult.modelsStatus }} {{ copilotDiagnoseResult.modelsUrl }}</span>
                  </div>
                </div>
              </v-alert>
            </div>
          </div>
        </v-alert>

</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from '../../../i18n'
import { ApiService } from '../../../services/api'

const props = defineProps<{
  apiKeys: string[]
  proxyUrl?: string
  serviceType?: string
  isEditing: boolean
  channelId?: number
}>()
const emit = defineEmits<{
  'update:apiKeys': [string[]]
  'update:proxyUrl': [string]
}>()
const { t } = useI18n()
const apiService = new ApiService()

interface CopilotDiagnoseResponse {
  githubUser?: {
    login?: string
    id?: number
  }
  githubUserError?: string
  copilotBaseUrl?: string
  tokenError?: string
  tokenErrorKind?: string
  modelsUrl?: string
  modelsStatus?: number
  modelsError?: string
  modelsBodyPrefix?: string
}

const copilotOAuthLoading = ref(false)
const copilotPolling = ref(false)
const copilotOAuthError = ref('')
const copilotOAuthSuccess = ref(false)
const copilotDeviceCode = ref('')
const copilotUserCode = ref('')
const copilotVerificationUri = ref('')
const copilotDiagnoseLoading = ref(false)
const copilotDiagnoseResult = ref<CopilotDiagnoseResponse | null>(null)
const copilotDiagnoseError = ref('')
let copilotPollTimer: number | null = null

const clearCopilotPollTimer = () => {
  if (copilotPollTimer !== null) {
    window.clearTimeout(copilotPollTimer)
    copilotPollTimer = null
  }
}

const appendOAuthKey = (accessToken: string) => {
  if (!props.apiKeys.includes(accessToken)) {
    emit('update:apiKeys', [...props.apiKeys, accessToken])
  }
}

const oauthProxyUrl = () => props.proxyUrl?.trim() || undefined

const pollCopilotAccessToken = async (intervalSeconds: number) => {
  if (!copilotDeviceCode.value) return
  copilotPolling.value = true
  try {
    const token = await apiService.pollCopilotAccessToken(copilotDeviceCode.value, oauthProxyUrl())
    if (token.accessToken) {
      appendOAuthKey(token.accessToken)
      copilotOAuthError.value = ''
      copilotOAuthSuccess.value = true
      copilotPolling.value = false
      copilotOAuthLoading.value = false
      clearCopilotPollTimer()
      return
    }
    if (token.error === 'expired_token') {
      copilotOAuthError.value = t('copilotOAuth.expired')
      copilotOAuthSuccess.value = false
      copilotPolling.value = false
      copilotOAuthLoading.value = false
      clearCopilotPollTimer()
      return
    }
    if (token.error && token.error !== 'authorization_pending') {
      copilotOAuthError.value = token.errorDescription || token.error
      copilotOAuthSuccess.value = false
      copilotPolling.value = false
      copilotOAuthLoading.value = false
      clearCopilotPollTimer()
      return
    }
  } catch (err) {
    copilotOAuthError.value = err instanceof Error ? err.message : String(err)
    copilotOAuthSuccess.value = false
    copilotPolling.value = false
    copilotOAuthLoading.value = false
    clearCopilotPollTimer()
    return
  }

  copilotPollTimer = window.setTimeout(() => {
    void pollCopilotAccessToken(intervalSeconds)
  }, Math.max(intervalSeconds, 5) * 1000)
}

const startCopilotOAuth = async () => {
  clearCopilotPollTimer()
  copilotOAuthLoading.value = true
  copilotOAuthError.value = ''
  copilotOAuthSuccess.value = false
  try {
    const device = await apiService.requestCopilotDeviceCode(oauthProxyUrl())
    copilotDeviceCode.value = device.deviceCode
    copilotUserCode.value = device.userCode
    copilotVerificationUri.value = device.verificationUri
    window.open(device.verificationUri, '_blank', 'noopener,noreferrer')
    await pollCopilotAccessToken(device.interval || 5)
  } catch (err) {
    copilotOAuthError.value = err instanceof Error ? err.message : String(err)
    copilotOAuthLoading.value = false
    copilotPolling.value = false
  }
}

onBeforeUnmount(() => {
  clearCopilotPollTimer()
})

const cancelCopilotOAuth = () => {
  clearCopilotPollTimer()
  copilotPolling.value = false
  copilotOAuthLoading.value = false
}

const retryCopilotOAuth = () => {
  void startCopilotOAuth()
}

const diagnoseCopilotChannel = async () => {
  if (!props.channelId) return
  copilotDiagnoseLoading.value = true
  copilotDiagnoseError.value = ''
  copilotDiagnoseResult.value = null
  try {
    const latestKey = props.apiKeys[0]
    copilotDiagnoseResult.value = await apiService.diagnoseCopilotChannel(props.channelId, latestKey) as unknown as CopilotDiagnoseResponse
  } catch (err) {
    copilotDiagnoseError.value = err instanceof Error ? err.message : String(err)
  } finally {
    copilotDiagnoseLoading.value = false
  }
}

</script>
