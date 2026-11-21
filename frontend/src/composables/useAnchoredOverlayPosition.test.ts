// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useAnchoredOverlayPosition } from './useAnchoredOverlayPosition'

let frames: Map<number, FrameRequestCallback>
let frameId: number
let observedResize: ResizeObserverCallback | undefined
const disconnect = vi.fn()

beforeEach(() => {
  frames = new Map()
  frameId = 0
  disconnect.mockReset()
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    frames.set(++frameId, callback)
    return frameId
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: ResizeObserverCallback) { observedResize = callback }
    observe() {}
    disconnect = disconnect
  })
  vi.stubGlobal('visualViewport', new EventTarget())
})
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const flushFrame = () => {
  const callbacks = [...frames.values()]
  frames.clear()
  callbacks.forEach(callback => callback(0))
}

async function setup(initiallyOpen = false) {
  const open = ref(initiallyOpen)
  const row = document.createElement('div')
  const anchor = document.createElement('button')
  const popup = document.createElement('div')
  row.append(anchor)
  document.body.append(row, popup)
  let top = 100
  vi.spyOn(anchor, 'getBoundingClientRect').mockImplementation(() => ({ bottom: top + 24 } as DOMRect))
  const updateLocation = vi.fn(() => { popup.style.top = `${anchor.getBoundingClientRect().bottom + 6}px` })
  const wrapper = mount(defineComponent({
    setup() {
      useAnchoredOverlayPosition(open, ref({ activatorEl: anchor, updateLocation }))
      return () => null
    },
  }))
  open.value = true
  await nextTick()
  await nextTick()
  flushFrame()
  updateLocation.mockClear()
  return { open, row, anchor, popup, wrapper, updateLocation, move: (value: number) => { top = value } }
}

it('在行的位移动画结束后按当前锚点重定位，不依赖一次性的打开延迟', async () => {
  const { row, popup, wrapper, move, updateLocation } = await setup()
  expect(popup.style.top).toBe('130px')
  move(175)
  row.dispatchEvent(new Event('transitionend', { bubbles: true }))
  flushFrame()
  expect(updateLocation).toHaveBeenCalledOnce()
  expect(popup.style.top).toBe('205px')
  expect(frames.size).toBe(0) // 稳定状态无持续轮询。
  wrapper.unmount()
})

it('嵌套容器滚动、视口缩放和尺寸变化合并到一帧，并忽略无关动画', async () => {
  const { row, popup, wrapper, move, updateLocation } = await setup(true)
  move(40)
  row.dispatchEvent(new Event('scroll')) // scroll 不冒泡，须捕获。
  window.dispatchEvent(new Event('resize'))
  window.visualViewport!.dispatchEvent(new Event('resize'))
  window.visualViewport!.dispatchEvent(new Event('scroll'))
  observedResize?.([], {} as ResizeObserver)
  expect(frames.size).toBe(1)
  flushFrame()
  expect(updateLocation).toHaveBeenCalledOnce()
  expect(popup.style.top).toBe('70px')
  const unrelated = document.createElement('div')
  document.body.append(unrelated)
  unrelated.dispatchEvent(new Event('animationend', { bubbles: true }))
  expect(frames.size).toBe(0)
  wrapper.unmount()
})

it.each(['close', 'unmount'])('%s 后取消排队工作并移除监听器', async action => {
  const { open, row, wrapper, updateLocation } = await setup()
  row.dispatchEvent(new Event('scroll'))
  expect(frames.size).toBe(1)
  if (action === 'close') {
    open.value = false
    await nextTick()
  } else wrapper.unmount()
  expect(frames.size).toBe(0)
  row.dispatchEvent(new Event('scroll'))
  row.dispatchEvent(new Event('transitionend', { bubbles: true }))
  window.dispatchEvent(new Event('resize'))
  window.visualViewport!.dispatchEvent(new Event('resize'))
  flushFrame()
  expect(updateLocation).not.toHaveBeenCalled()
  expect(disconnect).toHaveBeenCalledOnce()
  if (action === 'close') wrapper.unmount()
})

it('打开后立刻关闭时，nextTick 不会再创建观察器或动画帧', async () => {
  const open = ref(true)
  const updateLocation = vi.fn()
  const wrapper = mount(defineComponent({ setup() {
    useAnchoredOverlayPosition(open, ref({ updateLocation }))
    return () => null
  } }))
  wrapper.unmount()
  await nextTick()
  flushFrame()
  expect(updateLocation).not.toHaveBeenCalled()
  expect(frames.size).toBe(0)
})
