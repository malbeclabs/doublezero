import { useState, useEffect, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '@/lib/api'
import { TopologyMap } from '@/components/topology-map'
import { TopologyGraph } from '@/components/topology-graph'
import { TopologyProvider } from '@/components/topology'
import { Globe } from 'lucide-react'

// Only show loading indicator after this delay to avoid flash on fast loads
const LOADING_DELAY_MS = 300

type ViewMode = 'map' | 'graph'

interface TopologyPageProps {
  view: ViewMode
}

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

export function TopologyPage({ view }: TopologyPageProps) {
  const [searchParams, setSearchParams] = useSearchParams()

  // Get selected device from URL (shared between views)
  const selectedDevicePK = searchParams.get('type') === 'device' ? searchParams.get('id') : null

  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    refetchInterval: 60000, // Refresh every minute
  })

  // Handle device selection from graph view
  const handleGraphDeviceSelect = useCallback((devicePK: string | null) => {
    setSearchParams(prev => {
      if (devicePK === null) {
        prev.delete('type')
        prev.delete('id')
      } else {
        prev.set('type', 'device')
        prev.set('id', devicePK)
      }
      return prev
    })
  }, [setSearchParams])

  // Delay showing loading indicator to avoid flash on fast loads
  const [showLoading, setShowLoading] = useState(false)
  useEffect(() => {
    if (isLoading) {
      const timer = setTimeout(() => setShowLoading(true), LOADING_DELAY_MS)
      return () => clearTimeout(timer)
    }
    setShowLoading(false)
  }, [isLoading])

  if (isLoading && view === 'map') {
    // Only show loading indicator after delay to avoid flash
    return showLoading ? <TopologyLoading /> : null
  }

  if (error && view === 'map') {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-destructive">
          Failed to load topology: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    )
  }

  return (
    <TopologyProvider view={view}>
      <div className="fixed inset-0 z-0 h-screen w-screen">
        {view === 'map' && data && (
          <TopologyMap
            metros={data.metros}
            devices={data.devices}
            links={data.links}
            validators={data.validators}
          />
        )}

        {view === 'graph' && (
          <TopologyGraph
            selectedDevicePK={selectedDevicePK}
            onDeviceSelect={handleGraphDeviceSelect}
          />
        )}
      </div>
    </TopologyProvider>
  )
}
