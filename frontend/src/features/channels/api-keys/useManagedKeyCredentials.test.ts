// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { maskApiKey } from '../../../utils/apiKeyMask'
import { useManagedKeyCredentials } from './useManagedKeyCredentials'

const apiMocks = vi.hoisted(() => ({ getManagedAccounts: vi.fn(), setMiMoConsoleCookie: vi.fn(), clearMiMoConsoleCookie: vi.fn() }))
vi.mock('../../../services/api', () => ({
  ApiError: class extends Error {},
  ApiService: class {
    getManagedAccounts = apiMocks.getManagedAccounts
    setMiMoConsoleCookie = apiMocks.setMiMoConsoleCookie
    clearMiMoConsoleCookie = apiMocks.clearMiMoConsoleCookie
  },
}))
vi.mock('../../../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const wrappers: Array<ReturnType<typeof mount>> = []
const setupCredentials = async (providerId = 'mimo') => {
  const props = reactive({ providerId, accountUid: 'account-a', apiKeys: ['sk-first-1234567890', 'sk-second-1234567890'] })
  const updateKeys = vi.fn()
  let credentials!: ReturnType<typeof useManagedKeyCredentials>
  const wrapper = mount(defineComponent({ setup() { credentials = useManagedKeyCredentials(props, updateKeys); return () => null } }))
  wrappers.push(wrapper)
  await flushPromises()
  return { props, credentials, updateKeys }
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.getManagedAccounts.mockResolvedValue({ accounts: [{ accountUid: 'account-a', credentials: [{ credentialUid: 'second', keyMask: maskApiKey('sk-second-1234567890') }] }] })
  apiMocks.setMiMoConsoleCookie.mockResolvedValue({ keyAdopted: true, adoptedApiKey: 'adopted-key', keyMask: 'adopted-mask', tokenPlan: { planName: 'Coding' } })
})
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  vi.restoreAllMocks()
})

describe('managed channel Key credentials', () => {
  it('preserves the matching Key when a MiMo binding adopts a different credential', async () => {
    const { credentials, updateKeys } = await setupCredentials()
    const credential = credentials.mimoCredentials.value[0]!
    const form = credentials.mimoForms.value.second!
    form.cookie = '  console-cookie  '
    await credentials.saveMiMoConsoleCookie(credential)
    expect(apiMocks.setMiMoConsoleCookie).toHaveBeenCalledWith('account-a', 'second', { cookie: 'console-cookie', adoptCookieKey: false })
    expect(updateKeys).toHaveBeenCalledWith(['sk-first-1234567890', 'adopted-key'])
    expect(credential).toMatchObject({ keyMask: 'adopted-mask', hasMiMoConsoleCookie: true, mimoTokenPlan: { planName: 'Coding' } })
    expect(form).toMatchObject({ cookie: '', saving: false, error: '' })
  })

  it('keeps failed cookie saves visible and clears the loading state without adopting a Key', async () => {
    apiMocks.setMiMoConsoleCookie.mockRejectedValue(new Error('provider rejected cookie'))
    const { credentials, updateKeys } = await setupCredentials()
    credentials.mimoForms.value.second!.cookie = 'console-cookie'
    await credentials.saveMiMoConsoleCookie(credentials.mimoCredentials.value[0]!)
    expect(credentials.mimoForms.value.second).toMatchObject({ cookie: 'console-cookie', saving: false, error: 'provider rejected cookie' })
    expect(updateKeys).not.toHaveBeenCalled()
  })

  it('reloads credentials when switching accounts and does not query for ordinary API Key channels', async () => {
    const { props, credentials } = await setupCredentials('chat')
    expect(apiMocks.getManagedAccounts).not.toHaveBeenCalled()
    props.providerId = 'mimo'
    await flushPromises()
    expect(credentials.mimoCredentials.value).toHaveLength(1)
    props.accountUid = 'missing-account'
    await flushPromises()
    expect(credentials.mimoCredentials.value).toEqual([])
    expect(credentials.mimoCredentialsError.value).toBe('mimoConsoleCookie.accountNotFound')
    expect(credentials.mimoCredentialsLoading.value).toBe(false)
  })
})
