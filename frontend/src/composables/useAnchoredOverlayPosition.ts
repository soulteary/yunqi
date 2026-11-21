import { nextTick, watch, type Ref } from 'vue'

export interface AnchoredOverlay {
  activatorEl?: HTMLElement
  updateLocation?: (event: Event) => void
}

/** Follow position changes that do not resize the activator, such as row transforms. */
export function useAnchoredOverlayPosition(open: Ref<boolean>, overlay: Ref<AnchoredOverlay | null>) {
  watch(open, (isOpen, _previous, onCleanup) => {
    if (!isOpen) return

    let disposed = false
    let frame: number | undefined
    let resizeObserver: ResizeObserver | undefined
    const viewport = window.visualViewport
    const schedule = () => {
      if (disposed || frame !== undefined) return
      frame = window.requestAnimationFrame(() => {
        frame = undefined
        if (!disposed) overlay.value?.updateLocation?.(new Event('resize'))
      })
    }
    const onMotionEnd = (event: Event) => {
      const target = event.target
      const anchor = overlay.value?.activatorEl
      if (anchor && target instanceof Element && (target.contains(anchor) || anchor.contains(target))) schedule()
    }

    // Capture includes nested scroll containers; avoid Vuetify's idle-bound scroll updates.
    document.addEventListener('scroll', schedule, { passive: true, capture: true })
    document.addEventListener('transitionend', onMotionEnd, true)
    document.addEventListener('animationend', onMotionEnd, true)
    window.addEventListener('resize', schedule, { passive: true })
    viewport?.addEventListener('resize', schedule, { passive: true })
    viewport?.addEventListener('scroll', schedule, { passive: true })
    onCleanup(() => {
      disposed = true
      if (frame !== undefined) window.cancelAnimationFrame(frame)
      resizeObserver?.disconnect()
      document.removeEventListener('scroll', schedule, true)
      document.removeEventListener('transitionend', onMotionEnd, true)
      document.removeEventListener('animationend', onMotionEnd, true)
      window.removeEventListener('resize', schedule)
      viewport?.removeEventListener('resize', schedule)
      viewport?.removeEventListener('scroll', schedule)
    })
    void nextTick(() => {
      if (disposed) return
      const anchor = overlay.value?.activatorEl
      if (anchor && typeof ResizeObserver !== 'undefined') {
        resizeObserver = new ResizeObserver(schedule)
        resizeObserver.observe(anchor)
      }
      schedule()
    })
  }, { flush: 'post', immediate: true })
}
