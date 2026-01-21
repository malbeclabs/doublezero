import { useState, useEffect, useCallback } from 'react'
import { fetchVersion } from '@/lib/api'

const CHECK_INTERVAL_MS = 5 * 60 * 1000 // 5 minutes
const LOCAL_BUILD_TIMESTAMP = __BUILD_TIMESTAMP__

export function useVersionCheck() {
  const [updateAvailable, setUpdateAvailable] = useState(false)

  const checkVersion = useCallback(async () => {
    // Skip in development (timestamp will be regenerated on each HMR)
    if (LOCAL_BUILD_TIMESTAMP === 'dev' || import.meta.env.DEV) {
      return
    }

    const serverVersion = await fetchVersion()
    if (serverVersion && serverVersion.buildTimestamp !== 'dev') {
      const isOutdated = serverVersion.buildTimestamp !== LOCAL_BUILD_TIMESTAMP
      setUpdateAvailable(isOutdated)
    }
  }, [])

  useEffect(() => {
    // Check on mount
    checkVersion()

    // Check periodically
    const interval = setInterval(checkVersion, CHECK_INTERVAL_MS)

    // Check on window focus (user returning to tab)
    const handleFocus = () => {
      checkVersion()
    }
    window.addEventListener('focus', handleFocus)

    return () => {
      clearInterval(interval)
      window.removeEventListener('focus', handleFocus)
    }
  }, [checkVersion])

  const reload = useCallback(() => {
    window.location.reload()
  }, [])

  return { updateAvailable, reload }
}
