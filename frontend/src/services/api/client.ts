import { normalizeLocale } from '@/i18n/core'
import { translate } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { usePreferencesStore } from '@/stores/preferences'
import { API_BASE, ApiError } from '../api-helpers'

// Shared transport: dynamic credentials, locale, response parsing and errors.
export class ApiClient {
  historyQuery(duration: string, interval?: string): string {
    const params = new URLSearchParams({ duration })
    if (interval) {
      params.set('interval', interval)
    }
    return params.toString()
  }

  private t(key: Parameters<typeof translate>[1], params?: Parameters<typeof translate>[2]): string {
    const preferencesStore = usePreferencesStore()
    return translate(normalizeLocale(preferencesStore.uiLanguage as unknown as string), key, params)
  }

  // 获取当前 API Key（从 AuthStore）
  private getApiKey(): string | null {
    const authStore = useAuthStore()
    return authStore.apiKey as unknown as string | null
  }

  private async parseResponseBody(response: Response): Promise<unknown> {
    const text = await response.text()
    if (!text) return null
    try {
      return JSON.parse(text)
    } catch {
      return text
    }
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  async request(url: string, options: RequestInit = {}, timeoutMs?: number): Promise<any> {
    if (timeoutMs === undefined) return this.sendRequest(url, options)

    const controller = new AbortController()
    const callerSignal = options.signal
    const abortFromCaller = () => controller.abort(callerSignal?.reason)
    if (callerSignal?.aborted) abortFromCaller()
    else callerSignal?.addEventListener('abort', abortFromCaller, { once: true })
    const timeout = setTimeout(() => {
      controller.abort(new DOMException(`Request timed out after ${timeoutMs}ms`, 'TimeoutError'))
    }, timeoutMs)

    try {
      // Await body parsing as well as response headers before releasing the deadline.
      return await this.sendRequest(url, { ...options, signal: controller.signal })
    } catch (error) {
      // Body readers may report AbortError even when the request deadline supplied a TimeoutError.
      if (controller.signal.aborted) throw controller.signal.reason
      throw error
    } finally {
      clearTimeout(timeout)
      callerSignal?.removeEventListener('abort', abortFromCaller)
    }
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private async sendRequest(url: string, options: RequestInit): Promise<any> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>)
    }

    // 从 AuthStore 获取 API 密钥并添加到请求头
    const apiKey = this.getApiKey()
    if (apiKey) {
      headers['x-api-key'] = apiKey
    }

    const response = await fetch(`${API_BASE}${url}`, {
      ...options,
      headers
    })

    if (!response.ok) {
      const errorBody = await this.parseResponseBody(response)
      const errorMessage =
        (typeof errorBody === 'object' &&
        errorBody &&
        'error' in errorBody &&
        typeof (errorBody as { error?: unknown }).error === 'string'
          ? (errorBody as { error: string }).error
          : typeof errorBody === 'object' &&
              errorBody &&
              'message' in errorBody &&
              typeof (errorBody as { message?: unknown }).message === 'string'
            ? (errorBody as { message: string }).message
            : typeof errorBody === 'string'
              ? errorBody
              : null) || `Request failed (${response.status})`

      // 如果是401错误，清除认证信息并提示用户重新登录
      if (response.status === 401) {
        const authStore = useAuthStore()
        authStore.clearAuth()
        // 记录认证失败(前端日志)
        if (import.meta.env.DEV) {
          console.warn('🔒 认证失败 - 时间:', new Date().toISOString())
        }
        throw new ApiError(this.t('service.authFailed'), response.status, errorBody)
      }

      throw new ApiError(errorMessage, response.status, errorBody)
    }

    if (response.status === 204) return null
    return this.parseResponseBody(response)
  }
}
