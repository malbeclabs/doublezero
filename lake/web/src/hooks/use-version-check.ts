import { useState, useEffect, useCallback, useRef } from 'react'
import { fetchVersion } from '@/lib/api'

const CHECK_INTERVAL_MS = 5 * 60 * 1000 // 5 minutes
const SHOW_UPDATE_DELAY_MS = 120 * 1000 // 2 minutes grace period before showing update
const LOCAL_BUILD_TIMESTAMP = __BUILD_TIMESTAMP__

export function useVersionCheck() {
  const [updateAvailable, setUpdateAvailable] = useState(false)
  const mismatchDetectedAt = useRef<number | null>(null)
  const graceTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const checkVersion = useCallback(async () => {
    // Skip in development (timestamp will be regenerated on each HMR)
    if (LOCAL_BUILD_TIMESTAMP === 'dev' || import.meta.env.DEV) {
      return
    }

    const serverVersion = await fetchVersion()
    if (serverVersion && serverVersion.buildTimestamp !== 'dev') {
      const isOutdated = serverVersion.buildTimestamp !== LOCAL_BUILD_TIMESTAMP

      if (isOutdated) {
        // Track when we first detected the mismatch
        if (mismatchDetectedAt.current === null) {
          mismatchDetectedAt.current = Date.now()
          // Schedule a re-check after the grace period
          if (graceTimeoutRef.current) {
            clearTimeout(graceTimeoutRef.current)
          }
          graceTimeoutRef.current = setTimeout(() => {
            // Re-check version after grace period to confirm mismatch persists
            checkVersion()
          }, SHOW_UPDATE_DELAY_MS)
        }
        // Only show update notification after grace period
        const elapsed = Date.now() - mismatchDetectedAt.current
        if (elapsed >= SHOW_UPDATE_DELAY_MS) {
          setUpdateAvailable(true)
        }
      } else {
        // Version matches - reset tracking and cancel pending timeout
        mismatchDetectedAt.current = null
        setUpdateAvailable(false)
        if (graceTimeoutRef.current) {
          clearTimeout(graceTimeoutRef.current)
          graceTimeoutRef.current = null
        }
      }
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
      if (graceTimeoutRef.current) {
        clearTimeout(graceTimeoutRef.current)
      }
    }
  }, [checkVersion])

  const reload = useCallback(() => {
    window.location.reload()
  }, [])

  return { updateAvailable, reload }
}
