import { useState, useEffect } from 'react'

/**
 * Hook to delay showing loading UI (e.g., skeletons) to avoid flash for fast loads.
 * Only shows the loading state if loading persists beyond the delay threshold.
 *
 * @param isLoading - Whether the data is currently loading
 * @param delay - Milliseconds to wait before showing loading UI (default: 150ms)
 * @returns Whether to show the loading UI
 */
export function useDelayedLoading(isLoading: boolean, delay = 150) {
  const [showSkeleton, setShowSkeleton] = useState(false)

  useEffect(() => {
    if (isLoading) {
      const timer = setTimeout(() => setShowSkeleton(true), delay)
      return () => clearTimeout(timer)
    } else {
      setShowSkeleton(false)
    }
  }, [isLoading, delay])

  return showSkeleton
}
