import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '@/lib/api'
import { TopologyMap } from '@/components/topology-map'
import { Globe } from 'lucide-react'

// Only show loading indicator after this delay to avoid flash on fast loads
const LOADING_DELAY_MS = 300

function TopologyLoading() {
  return (
    <div className="fixed inset-0 z-0 h-screen w-screen flex items-center justify-center bg-background">
      <div className="flex flex-col items-center gap-3">
        <Globe className="h-10 w-10 text-muted-foreground animate-pulse" />
        <div className="text-sm text-muted-foreground">Loading network topology...</div>
      </div>
    </div>
  )
}

export function TopologyPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    refetchInterval: 60000, // Refresh every minute
  })

  // Delay showing loading indicator to avoid flash on fast loads
  const [showLoading, setShowLoading] = useState(false)
  useEffect(() => {
    if (isLoading) {
      const timer = setTimeout(() => setShowLoading(true), LOADING_DELAY_MS)
      return () => clearTimeout(timer)
    }
    setShowLoading(false)
  }, [isLoading])

  if (isLoading) {
    // Only show loading indicator after delay to avoid flash
    return showLoading ? <TopologyLoading /> : null
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-destructive">
          Failed to load topology: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-muted-foreground">No topology data available</div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-0 h-screen w-screen">
      <TopologyMap
        metros={data.metros}
        devices={data.devices}
        links={data.links}
      />
    </div>
  )
}
