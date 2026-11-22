// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelImportPanel from './ModelImportPanel.vue'
const mocks = vi.hoisted(() => ({
  getModelImport: vi.fn(),
  removeModelImport: vi.fn(),
  setModelImportPolicy: vi.fn(),
  previewModelImport: vi.fn(),
  approveModelImport: vi.fn(),
  discover: vi.fn()
}))
vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('../services/api', () => ({
  api: mocks,
  ApiError: class ApiError extends Error {
    status = 409
  }
}))
vi.mock('../services/autopilot-api', () => ({ autoDiscoverChannel: mocks.discover }))
const container = { template: '<div><slot /></div>' }
const input = defineComponent({
  props: ['modelValue', 'label', 'items', 'disabled'],
  emits: ['update:modelValue'],
  template:
    '<input :aria-label="label" :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)" />'
})
const checkboxInput = defineComponent({
  props: ['modelValue', 'label', 'disabled'],
  emits: ['update:modelValue'],
  template:
    '<input type="checkbox" :aria-label="label" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\',$event.target.checked)" />'
})
const button = { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' }
function review(id = 'review-a') {
  return {
    id,
    baseRevision: 'rev-a',
    source: 'operator',
    observedAt: 'now',
    models: [{ id: 'raw', upstreamId: 'provider/raw', endpoints: [] }],
    diff: { added: ['raw'], removed: [], changed: [] }
  }
}
function view(uid: string, pending = review()) {
  return {
    configured: true,
    channelUid: uid,
    revision: `rev-${uid}`,
    state: { policy: { enabled: false }, approved: [], pending }
  }
}
function setup() {
  const channels = [
    { channelUid: 'a', name: 'A', apiKeys: ['must-not-submit'], remark: 'unsaved draft' },
    { channelUid: 'b', name: 'B' }
  ]
  return mount(ModelImportPanel, {
    props: {
      channels,
      channelType: 'chat'
    },
    global: {
      stubs: {
        VExpansionPanels: container,
        VExpansionPanel: container,
        VExpansionPanelText: container,
        VSelect: input,
        VBtn: button,
        VAlert: container,
        VCheckbox: checkboxInput,
        VTextarea: input,
        VTextField: input,
        VDivider: container,
        VTable: container,
        VPagination: input
      }
    }
  })
}
async function click(wrapper: ReturnType<typeof setup>, key: string) {
  const found = wrapper.findAll('button').find(button => button.text() === `modelImport.${key}`)
  expect(found).toBeDefined()
  await found!.trigger('click')
  await flushPromises()
}
beforeEach(() => vi.clearAllMocks())
describe('independent reviewed model catalog', () => {
  it('ignores late state from a previous UID and approves only the selected review', async () => {
    let resolveA!: (value: unknown) => void
    mocks.getModelImport.mockImplementation((uid: string) =>
      uid === 'a'
        ? new Promise(resolve => {
            resolveA = resolve
          })
        : Promise.resolve(view('b', review('review-b')))
    )
    mocks.approveModelImport.mockResolvedValue(view('b'))
    const wrapper = setup()
    expect(mocks.getModelImport).not.toHaveBeenCalled()
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('a')
    await click(wrapper, 'load')
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('b')
    await click(wrapper, 'load')
    resolveA(view('a'))
    await flushPromises()
    await click(wrapper, 'approve')
    expect(mocks.approveModelImport).toHaveBeenCalledExactlyOnceWith('b', 'chat', 'review-b', false, false)
  })
  it('saves only policy and prevents approval of an edited draft', async () => {
    mocks.getModelImport.mockResolvedValue(view('a'))
    mocks.setModelImportPolicy.mockResolvedValue(view('a'))
    const wrapper = setup()
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('a')
    await click(wrapper, 'load')
    await wrapper.find('input[aria-label="modelImport.stripPrefix"]').setValue('provider/')
    expect(
      wrapper
        .findAll('button')
        .find(button => button.text() === 'modelImport.approve')!
        .attributes('disabled')
    ).toBeDefined()
    await click(wrapper, 'savePolicy')
    expect(mocks.setModelImportPolicy).toHaveBeenCalledWith(
      'a',
      'chat',
      { enabled: false, include: [], exclude: [], stripPrefix: 'provider/', stripSuffix: '', case: '', rules: [] },
      'rev-a'
    )
    expect(JSON.stringify(mocks.setModelImportPolicy.mock.calls)).not.toContain('must-not-submit')
    await wrapper.find('input[aria-label="modelImport.manualModels"]').setValue('changed')
    expect(
      wrapper
        .findAll('button')
        .find(button => button.text() === 'modelImport.approve')!
        .attributes('disabled')
    ).toBeDefined()
    expect(mocks.approveModelImport).not.toHaveBeenCalled()
  })
  it('binds previews to revision and discards late results after protocol changes', async () => {
    mocks.getModelImport.mockResolvedValue(view('a'))
    let resolve!: (value: unknown) => void
    mocks.previewModelImport.mockImplementation(
      () =>
        new Promise(done => {
          resolve = done
        })
    )
    const wrapper = setup()
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('a')
    await click(wrapper, 'load')
    await wrapper.find('input[aria-label="modelImport.manualModels"]').setValue('provider/new')
    await click(wrapper, 'preview')
    expect(mocks.previewModelImport).toHaveBeenCalledExactlyOnceWith('a', 'chat', ['provider/new'], 'rev-a')
    await wrapper.setProps({ channelType: 'responses' })
    resolve(review('stale'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('modelImport.approve')
  })
  it('requires explicit acknowledgement before restoring unrestricted legacy behavior', async () => {
    mocks.getModelImport.mockResolvedValue({ ...view('a'), unrestrictedOnRemove: true })
    mocks.removeModelImport.mockResolvedValue(view('a'))
    const wrapper = setup()
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('a')
    await click(wrapper, 'load')
    expect(
      wrapper
        .findAll('button')
        .find(button => button.text() === 'modelImport.removePolicy')!
        .attributes('disabled')
    ).toBeDefined()
    // Vuetify checkbox emits the acknowledged boolean, rather than a text value.
    await wrapper.find('input[aria-label="modelImport.allowUnrestricted"]').setValue(true)
    await flushPromises()
    await click(wrapper, 'removePolicy')
    expect(mocks.removeModelImport).toHaveBeenCalledExactlyOnceWith('a', 'chat', 'rev-a', true)
    expect(wrapper.text()).not.toContain('modelImport.approve')
  })
  it('requires persisted governance before discovery on a channel without policy', async () => {
    mocks.getModelImport.mockResolvedValue({
      ...view('a'),
      configured: false,
      state: { policy: { enabled: false }, approved: [] }
    })
    const wrapper = setup()
    await wrapper.find('input[aria-label="modelImport.channel"]').setValue('a')
    await click(wrapper, 'load')
    expect(
      wrapper
        .findAll('button')
        .find(button => button.text() === 'modelImport.discover')!
        .attributes('disabled')
    ).toBeDefined()
    await click(wrapper, 'discover')
    expect(mocks.discover).not.toHaveBeenCalled()
  })
})
