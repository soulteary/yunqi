import { afterEach, expect, it, vi } from 'vitest'
import { getRuntimeLocale } from './core'
import { translate } from './index'

afterEach(() => vi.unstubAllGlobals())

it('prefers the Yunqi runtime language while retaining legacy deployment input', () => {
  vi.stubGlobal('window', {
    __YUNQI_RUNTIME_CONFIG__: { uiLanguage: 'id' },
    __CCX_RUNTIME_CONFIG__: { uiLanguage: 'en' },
  })
  expect(getRuntimeLocale()).toBe('id')
  vi.stubGlobal('window', { __CCX_RUNTIME_CONFIG__: { uiLanguage: 'en' } })
  expect(getRuntimeLocale()).toBe('en')
  vi.stubGlobal('window', {})
  vi.stubGlobal('__APP_UI_LANGUAGE__', 'zh-CN')
  expect(getRuntimeLocale()).toBe('zh-CN')
})

it('uses the Yunqi translation instance and restores its previous locale', () => {
  const instance = { global: { locale: { value: 'zh-CN' }, t: vi.fn(() => 'translated') } }
  vi.stubGlobal('__YUNQI_I18N__', instance)
  expect(translate('en', 'channel.name')).toBe('translated')
  expect(instance.global.t).toHaveBeenCalledWith('channel.name', undefined)
  expect(instance.global.locale.value).toBe('zh-CN')
})
