// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionProviderGrid from './SubscriptionProviderGrid.vue'
import { getProviderTemplates, type ProviderTemplate } from '@/services/autopilot-api'

vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/services/autopilot-api', () => ({ getProviderTemplates: vi.fn() }))

const providers: ProviderTemplate[] = [
  {
    providerId: 'mimo',
    displayName: 'MiMo',
    description: 'Messages / Chat',
    channelKind: 'messages',
    serviceType: 'claude'
  },
  {
    providerId: 'compshare',
    displayName: 'Compshare',
    description: 'API subscription',
    channelKind: 'messages',
    serviceType: 'claude'
  },
  {
    providerId: 'volcengine',
    displayName: 'Volcengine',
    description: 'Agent Plan',
    channelKind: 'messages',
    serviceType: 'claude'
  }
]

function render() {
  const stubs = Object.fromEntries(
    ['VCard', 'VIcon', 'VSpacer', 'VExpandTransition'].map(name => [name, { template: '<div><slot /></div>' }])
  )
  return mount(SubscriptionProviderGrid, {
    global: { stubs: { ...stubs, VBtn: { template: '<button><slot /></button>' } } }
  })
}

beforeEach(() => {
  vi.mocked(getProviderTemplates).mockReset().mockResolvedValue(providers)
})

describe('subscription provider actions', () => {
  it('renders every supplied provider once in backend order, with equal cards and only management actions', async () => {
    const view = render()
    await flushPromises()
    const cards = view.findAll('.provider-block')
    expect(cards.map(card => card.find('.text-subtitle-1').text())).toEqual([
      'MiMo',
      'Compshare',
      'Volcengine',
      'GitHub Copilot',
      'new-api'
    ])
    expect(cards.map(card => card.attributes('class'))).toEqual(Array(5).fill('provider-block'))
    expect(view.findAll('img')).toHaveLength(0)
    expect(
      view
        .findAll('button')
        .every(button => ['subscription.addProvider', 'subscription.visitConsole'].includes(button.text()))
    ).toBe(true)
    expect(cards[1].text()).toContain('API subscription')
    view.unmount()
  })

  it('keeps adding, collapsing and opening the official console available', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const view = render()
    await flushPromises()
    const buttons = view.findAll('.provider-block')[1].findAll('button')
    await buttons[0].trigger('click')
    expect(view.emitted('add')).toEqual([['compshare']])
    expect(view.emitted('update:expandedProviderId')).toEqual([['compshare']])
    await buttons[0].trigger('click')
    expect(view.emitted('add')).toHaveLength(1)
    expect(view.emitted('update:expandedProviderId')).toEqual([['compshare'], ['']])
    await buttons[1].trigger('click')
    expect(open).toHaveBeenCalledWith(
      'https://console.compshare.cn/light-gpu/model-subscription',
      '_blank',
      'noopener,noreferrer'
    )
    view.unmount()
    open.mockRestore()
  })

  it('does not invent provider cards if the template request fails', async () => {
    vi.mocked(getProviderTemplates).mockRejectedValue(new Error('offline'))
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const view = render()
    await flushPromises()
    expect(view.findAll('.provider-block').map(card => card.find('.text-subtitle-1').text())).toEqual([
      'GitHub Copilot',
      'new-api'
    ])
    view.unmount()
    error.mockRestore()
  })
})
