import { onMounted, onUnmounted } from 'vue'
import { useTheme } from 'vuetify'
import { usePreferencesStore } from '../../stores/preferences'
import { useAppTheme } from '../useTheme'

// 外观状态与系统主题监听；复古字体继续复用原主题工具。
export function useAppThemeController() {
  const theme = useTheme()
  const preferencesStore = usePreferencesStore()
  const { init: initTheme } = useAppTheme()

  // 主题管理
  const toggleDarkMode = () => {
    const newMode = preferencesStore.darkModePreference === 'dark' ? 'light' : 'dark'
    setDarkMode(newMode)
  }

  const setDarkMode = (themeName: 'light' | 'dark' | 'auto') => {
    preferencesStore.setDarkMode(themeName)
    const apply = (isDark: boolean) => {
      // 使用 Vuetify 3.9+ 推荐的 theme.change() API
      theme.change(isDark ? 'dark' : 'light')
    }

    if (themeName === 'auto') {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      apply(prefersDark)
    } else {
      apply(themeName === 'dark')
    }
    // PreferencesStore 已通过 pinia-plugin-persistedstate 自动持久化，无需手动写入 localStorage
  }

  // 监听系统主题变化（setup 阶段注册，onUnmounted 清理，避免泄漏）
  // 守卫非浏览器环境（SSR / vitest 非 jsdom）：避免 ReferenceError: window is not defined
  const mediaQuery =
    typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-color-scheme: dark)')
      : null
  const handlePref = () => {
    if (preferencesStore.darkModePreference === 'auto') setDarkMode('auto')
  }
  mediaQuery?.addEventListener('change', handlePref)

  onMounted(() => {
    document.documentElement.dataset.theme = 'retro'
    initTheme()
    setDarkMode(preferencesStore.darkModePreference)
  })

  onUnmounted(() => mediaQuery?.removeEventListener('change', handlePref))

  return { theme, toggleDarkMode }
}
