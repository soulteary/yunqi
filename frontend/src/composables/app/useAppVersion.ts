import { onMounted, onUnmounted } from 'vue'
import { fetchHealth } from '../../services/api'
import { versionService } from '../../services/version'
import { useSystemStore } from '../../stores/system'

// 版本请求、并发检查保护和窗口事件由同一组件作用域管理。
export function useAppVersion() {
  const systemStore = useSystemStore()

  // 版本检查
  const checkVersion = async () => {
    if (systemStore.isCheckingVersion) return

    systemStore.setCheckingVersion(true)
    try {
      // 直接通过 health 接口获取当前版本，再从 GitHub 检查是否有新版本
      const health = await fetchHealth()
      const currentVersion = health.version?.version || ''

      if (currentVersion) {
        versionService.setCurrentVersion(currentVersion)
        systemStore.setCurrentVersion(currentVersion)

        const result = await versionService.checkForUpdates()
        systemStore.setVersionInfo(result)
      } else {
        systemStore.setVersionInfo({
          currentVersion: systemStore.versionInfo.currentVersion,
          latestVersion: null,
          isLatest: false,
          hasUpdate: false,
          releaseUrl: null,
          lastCheckTime: 0,
          status: 'error'
        })
      }
    } catch (error) {
      console.warn('Version check failed:', error)
      systemStore.setVersionInfo({
        currentVersion: systemStore.versionInfo.currentVersion,
        latestVersion: null,
        isLatest: false,
        hasUpdate: false,
        releaseUrl: null,
        lastCheckTime: 0,
        status: 'error'
      })
    } finally {
      systemStore.setCheckingVersion(false)
    }
  }

  // 版本点击处理
  const handleVersionClick = () => {
    systemStore.setUpdateDialogOpen(true)
  }

  const handleVersionCheck = () => {
    void checkVersion()
  }

  onMounted(() => {
    void checkVersion()
    window.addEventListener('yunqi-check-version', handleVersionCheck)
  })

  onUnmounted(() => window.removeEventListener('yunqi-check-version', handleVersionCheck))

  return { handleVersionClick }
}
