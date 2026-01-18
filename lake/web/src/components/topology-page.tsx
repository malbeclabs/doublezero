import { useState, useEffect, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '@/lib/api'
import { TopologyMap } from '@/components/topology-map'
import { TopologyGraph } from '@/components/topology-graph'
import { Globe, Network } from 'lucide-react'

// Only show loading indicator after this delay to avoid flash on fast loads
const LOADING_DELAY_MS = 300

type ViewMode = 'map' | 'graph'

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

function ViewToggle({ view, onViewChange }: { view: ViewMode; onViewChange: (v: ViewMode) => void }) {
  return (
    <div className="absolute top-4 left-1/2 -translate-x-1/2 z-[1001] flex bg-[var(--card)] border border-[var(--border)] rounded-md shadow-sm">
      <button
        onClick={() => onViewChange('map')}
        className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-l-md transition-colors ${
          view === 'map'
            ? 'bg-primary text-primary-foreground'
            : 'hover:bg-[var(--muted)] text-muted-foreground'
        }`}
        title="Geographic map view"
      >
        <Globe className="h-4 w-4" />
        <span>Map</span>
      </button>
      <button
        onClick={() => onViewChange('graph')}
        className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-r-md transition-colors ${
          view === 'graph'
            ? 'bg-primary text-primary-foreground'
            : 'hover:bg-[var(--muted)] text-muted-foreground'
        }`}
        title="ISIS topology graph view"
      >
        <Network className="h-4 w-4" />
        <span>Graph</span>
      </button>
    </div>
  )
}

export function TopologyPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [view, setView] = useState<ViewMode>(() => {
    const v = searchParams.get('view')
    return v === 'graph' ? 'graph' : 'map'
  })

  // Get selected device from URL (shared between views)
  const selectedDevicePK = searchParams.get('type') === 'device' ? searchParams.get('id') : null

  const { data, isLoading, error } = useQuery({
    queryKey: ['topology'],
    queryFn: fetchTopology,
    refetchInterval: 60000, // Refresh every minute
  })

  // Sync view to URL
  const handleViewChange = (newView: ViewMode) => {
    setView(newView)
    setSearchParams(prev => {
      if (newView === 'map') {
        prev.delete('view')
      } else {
        prev.set('view', newView)
      }
      return prev
    })
  }

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
    <div className="fixed inset-0 z-0 h-screen w-screen">
      <ViewToggle view={view} onViewChange={handleViewChange} />

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
  )
}
