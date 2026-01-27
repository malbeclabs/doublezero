import { useState, useEffect, useRef, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import { ChevronDown, GripVertical, Check, RefreshCw } from 'lucide-react'
import { fetchTrafficData, fetchTopology } from '@/lib/api'
import { TrafficChart } from '@/components/traffic-chart-uplot'
import { LoadingSplash } from '@/components/loading-splash'

export interface LinkLookupInfo {
  pk: string
  code: string
  bandwidth_bps: number
  side_a_pk: string
  side_a_code: string
  side_z_pk: string
  side_z_code: string
}

// Lazy chart wrapper that only renders when in viewport
function LazyChart({ children, height = 600 }: { children: React.ReactNode; height?: number }) {
  const [isVisible, setIsVisible] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setIsVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '100px' } // Start loading 100px before visible
    )

    if (ref.current) {
      observer.observe(ref.current)
    }

    return () => observer.disconnect()
  }, [])

  return (
    <div ref={ref} style={{ minHeight: height }}>
      {isVisible ? children : (
        <div className="animate-pulse bg-muted rounded h-full" />
      )}
    </div>
  )
}

type TimeRange = '1h' | '3h' | '6h' | '12h' | '24h' | '3d' | '7d'

const timeRangeLabels: Record<TimeRange, string> = {
  '1h': 'Last 1 Hour',
  '3h': 'Last 3 Hours',
  '6h': 'Last 6 Hours',
  '12h': 'Last 12 Hours',
  '24h': 'Last 24 Hours',
  '3d': 'Last 3 Days',
  '7d': 'Last 7 Days',
}

type BucketSize = 'none' | '2 SECOND' | '30 SECOND' | '1 MINUTE' | '5 MINUTE' | '10 MINUTE' | '15 MINUTE' | '30 MINUTE' | '1 HOUR' | 'auto'

const bucketLabels: Record<BucketSize, string> = {
  'none': 'None (raw data)',
  '2 SECOND': '2 seconds',
  '30 SECOND': '30 seconds',
  '1 MINUTE': '1 minute',
  '5 MINUTE': '5 minutes',
  '10 MINUTE': '10 minutes',
  '15 MINUTE': '15 minutes',
  '30 MINUTE': '30 minutes',
  '1 HOUR': '1 hour',
  'auto': 'Auto',
}

type AggMethod = 'max' | 'avg'

const aggLabels: Record<AggMethod, string> = {
  'max': 'Max',
  'avg': 'Average',
}

type ChartSection = 'non-tunnel-stacked' | 'non-tunnel' | 'tunnel-stacked' | 'tunnel'

const chartSectionLabels: Record<ChartSection, string> = {
  'non-tunnel-stacked': 'Non-Tunnel Traffic (stacked)',
  'non-tunnel': 'Non-Tunnel Traffic',
  'tunnel-stacked': 'Tunnel Traffic (stacked)',
  'tunnel': 'Tunnel Traffic',
}

type Layout = '1x4' | '2x2'

const layoutLabels: Record<Layout, string> = {
  '1x4': '1',
  '2x2': '2',
}

type RefreshInterval = 'never' | '30s' | '1m' | '10m' | '30m'

const refreshIntervalLabels: Record<RefreshInterval, string> = {
  'never': 'Never',
  '30s': '30 seconds',
  '1m': '1 minute',
  '10m': '10 minutes',
  '30m': '30 minutes',
}

const refreshIntervalMs: Record<RefreshInterval, number | false> = {
  'never': false,
  '30s': 30000,
  '1m': 60000,
  '10m': 600000,
  '30m': 1800000,
}


function TimeRangeSelector({
  value,
  onChange,
}: {
  value: TimeRange
  onChange: (value: TimeRange) => void
}) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
      >
        {timeRangeLabels[value]}
        <ChevronDown className="h-4 w-4" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[160px]">
            {(['1h', '3h', '6h', '12h', '24h', '3d', '7d'] as TimeRange[]).map((range) => (
              <button
                key={range}
                onClick={() => {
                  onChange(range)
                  setIsOpen(false)
                }}
                className={`w-full px-3 py-1.5 text-left text-sm transition-colors ${
                  value === range
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {timeRangeLabels[range]}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function BucketSelector({
  bucketValue,
  aggValue,
  onBucketChange,
  onAggChange,
}: {
  bucketValue: BucketSize
  aggValue: AggMethod
  onBucketChange: (value: BucketSize) => void
  onAggChange: (value: AggMethod) => void
}) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
      >
        Coalesce: {bucketLabels[bucketValue]} ({aggLabels[aggValue]})
        <ChevronDown className="h-4 w-4" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[200px]">
            <div className="px-3 py-1.5 text-xs font-semibold text-muted-foreground border-b border-border">
              Bucket Size
            </div>
            {(['none', '2 SECOND', '30 SECOND', '1 MINUTE', '5 MINUTE', '10 MINUTE', '15 MINUTE', '30 MINUTE', '1 HOUR', 'auto'] as BucketSize[]).map((bucket) => (
              <button
                key={bucket}
                onClick={() => {
                  onBucketChange(bucket)
                }}
                className={`w-full px-3 py-1.5 text-left text-sm transition-colors ${
                  bucketValue === bucket
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {bucketLabels[bucket]}
              </button>
            ))}
            <div className="px-3 py-1.5 text-xs font-semibold text-muted-foreground border-t border-b border-border mt-1">
              Aggregation
            </div>
            {(['max', 'avg'] as AggMethod[]).map((agg) => (
              <button
                key={agg}
                onClick={() => {
                  onAggChange(agg)
                }}
                className={`w-full px-3 py-1.5 text-left text-sm transition-colors ${
                  aggValue === agg
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {aggLabels[agg]}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function ShowGraphsSelector({
  sections,
  visibleSections,
  onReorder,
  onToggle,
}: {
  sections: ChartSection[]
  visibleSections: Set<ChartSection>
  onReorder: (sections: ChartSection[]) => void
  onToggle: (section: ChartSection) => void
}) {
  const [isOpen, setIsOpen] = useState(false)
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)

  const handleDragStart = (e: React.DragEvent, index: number) => {
    setDraggedIndex(index)
    e.dataTransfer.effectAllowed = 'move'
  }

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault()
    if (draggedIndex === null || draggedIndex === index) return

    const newSections = [...sections]
    const draggedSection = newSections[draggedIndex]
    newSections.splice(draggedIndex, 1)
    newSections.splice(index, 0, draggedSection)

    onReorder(newSections)
    setDraggedIndex(index)
  }

  const handleDragEnd = () => {
    setDraggedIndex(null)
  }

  const visibleCount = Array.from(visibleSections).filter(s => sections.includes(s)).length

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
      >
        Show Graphs ({visibleCount}/{sections.length})
        <ChevronDown className="h-4 w-4" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[280px]">
            {sections.map((section, index) => (
              <div
                key={section}
                draggable
                onDragStart={(e) => handleDragStart(e, index)}
                onDragOver={(e) => handleDragOver(e, index)}
                onDragEnd={handleDragEnd}
                className={`flex items-center gap-2 px-3 py-1.5 text-sm transition-colors cursor-move ${
                  draggedIndex === index ? 'opacity-50' : ''
                }`}
              >
                <GripVertical className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                <button
                  onClick={() => onToggle(section)}
                  className="flex-1 flex items-center justify-between text-left hover:bg-muted/50 -m-1.5 p-1.5 rounded"
                >
                  <span className="text-foreground">{chartSectionLabels[section]}</span>
                  {visibleSections.has(section) && <Check className="h-4 w-4 flex-shrink-0" />}
                </button>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function LayoutSelector({
  value,
  onChange,
}: {
  value: Layout
  onChange: (value: Layout) => void
}) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
      >
        Columns: {layoutLabels[value]}
        <ChevronDown className="h-4 w-4" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[180px]">
            {(['1x4', '2x2'] as Layout[]).map((layout) => (
              <button
                key={layout}
                onClick={() => {
                  onChange(layout)
                  setIsOpen(false)
                }}
                className={`w-full px-3 py-1.5 text-left text-sm transition-colors ${
                  value === layout
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {layoutLabels[layout]}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function RefreshIntervalSelector({
  value,
  onChange,
}: {
  value: RefreshInterval
  onChange: (value: RefreshInterval) => void
}) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
      >
        Auto-refresh: {refreshIntervalLabels[value]}
        <ChevronDown className="h-4 w-4" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[180px]">
            {(['never', '30s', '1m', '10m', '30m'] as RefreshInterval[]).map((interval) => (
              <button
                key={interval}
                onClick={() => {
                  onChange(interval)
                  setIsOpen(false)
                }}
                className={`w-full px-3 py-1.5 text-left text-sm transition-colors ${
                  value === interval
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {refreshIntervalLabels[interval]}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

export function TrafficPage() {
  const [timeRange, setTimeRange] = useState<TimeRange>('6h')
  const [bucketSize, setBucketSize] = useState<BucketSize>('auto')
  const [aggMethod, setAggMethod] = useState<AggMethod>('max')
  const [chartSections, setChartSections] = useState<ChartSection[]>([
    'non-tunnel-stacked',
    'non-tunnel',
    'tunnel-stacked',
    'tunnel',
  ])
  const [visibleSections, setVisibleSections] = useState<Set<ChartSection>>(
    new Set(['non-tunnel-stacked', 'non-tunnel', 'tunnel-stacked', 'tunnel'])
  )
  const [layout, setLayout] = useState<Layout>('1x4')
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>('never')

  // Load time range from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('traffic-time-range')
    if (saved && saved in timeRangeLabels) {
      setTimeRange(saved as TimeRange)
    }
  }, [])

  // Load chart sections order from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('traffic-chart-sections')
    if (saved) {
      try {
        const sections = JSON.parse(saved) as ChartSection[]
        setChartSections(sections)
      } catch {
        // Ignore invalid data
      }
    }
  }, [])

  // Load visible sections from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('traffic-visible-sections')
    if (saved) {
      try {
        const sections = JSON.parse(saved) as ChartSection[]
        setVisibleSections(new Set(sections))
      } catch {
        // Ignore invalid data
      }
    }
  }, [])

  // Load layout from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('traffic-layout')
    if (saved && (saved === '1x4' || saved === '2x2')) {
      setLayout(saved as Layout)
    }
  }, [])

  // Load refresh interval from localStorage
  useEffect(() => {
    const saved = localStorage.getItem('traffic-refresh-interval')
    if (saved && saved in refreshIntervalLabels) {
      setRefreshInterval(saved as RefreshInterval)
    }
  }, [])

  // Save time range to localStorage
  const handleTimeRangeChange = (range: TimeRange) => {
    setTimeRange(range)
    localStorage.setItem('traffic-time-range', range)
  }

  // Update bucket size (not persisted)
  const handleBucketSizeChange = (bucket: BucketSize) => {
    setBucketSize(bucket)
  }

  // Update aggregation method (not persisted)
  const handleAggMethodChange = (agg: AggMethod) => {
    setAggMethod(agg)
  }

  // Save chart sections order to localStorage
  const handleSectionsReorder = (sections: ChartSection[]) => {
    setChartSections(sections)
    localStorage.setItem('traffic-chart-sections', JSON.stringify(sections))
  }

  // Toggle section visibility
  const handleSectionToggle = (section: ChartSection) => {
    const newVisible = new Set(visibleSections)
    if (newVisible.has(section)) {
      newVisible.delete(section)
    } else {
      newVisible.add(section)
    }
    setVisibleSections(newVisible)
    localStorage.setItem('traffic-visible-sections', JSON.stringify(Array.from(newVisible)))
  }

  // Save layout to localStorage
  const handleLayoutChange = (newLayout: Layout) => {
    setLayout(newLayout)
    localStorage.setItem('traffic-layout', newLayout)
  }

  // Save refresh interval to localStorage
  const handleRefreshIntervalChange = (interval: RefreshInterval) => {
    setRefreshInterval(interval)
    localStorage.setItem('traffic-refresh-interval', interval)
  }

  // Compute actual bucket size to send to API
  const actualBucketSize = useMemo(() => {
    if (bucketSize === 'auto') {
      // Auto-select based on time range
      switch (timeRange) {
        case '1h':
        case '3h':
        case '6h':
        case '12h':
          return '30 SECOND'
        case '24h':
          return '1 MINUTE'
        case '3d':
        case '7d':
          return '5 MINUTE'
        default:
          return '30 SECOND'
      }
    }
    return bucketSize
  }, [bucketSize, timeRange])

  // Get refetch interval based on selected refresh interval
  const refetchIntervalValue = useMemo(() => {
    return refreshIntervalMs[refreshInterval]
  }, [refreshInterval])

  // Fetch tunnel data
  const {
    data: tunnelData,
    isLoading: tunnelLoading,
    error: tunnelError,
    refetch: refetchTunnel,
  } = useQuery({
    queryKey: ['traffic', timeRange, true, actualBucketSize, aggMethod],
    queryFn: () => fetchTrafficData(timeRange, true, actualBucketSize, aggMethod),
    staleTime: 30000, // 30 seconds
    refetchInterval: refetchIntervalValue,
  })

  // Fetch non-tunnel data
  const {
    data: nonTunnelData,
    isLoading: nonTunnelLoading,
    error: nonTunnelError,
    refetch: refetchNonTunnel,
  } = useQuery({
    queryKey: ['traffic', timeRange, false, actualBucketSize, aggMethod],
    queryFn: () => fetchTrafficData(timeRange, false, actualBucketSize, aggMethod),
    staleTime: 30000, // 30 seconds
    refetchInterval: refetchIntervalValue,
  })

  // Fetch topology data for link metadata
  const {
    data: topologyData,
    isLoading: topologyLoading,
    refetch: refetchTopology,
  } = useQuery({
    queryKey: ['topology'],
    queryFn: () => fetchTopology(),
    staleTime: 60000, // 1 minute
    refetchInterval: refetchIntervalValue,
  })

  // Manual refresh handler
  const handleManualRefresh = () => {
    refetchTunnel()
    refetchNonTunnel()
    refetchTopology()
  }

  // Build link lookup: device_pk + interface -> link info
  const linkLookup = useMemo(() => {
    if (!topologyData?.links) return new Map<string, LinkLookupInfo>()

    const map = new Map<string, LinkLookupInfo>()
    for (const link of topologyData.links) {
      const linkInfo: LinkLookupInfo = {
        pk: link.pk,
        code: link.code,
        bandwidth_bps: link.bandwidth_bps,
        side_a_pk: link.side_a_pk,
        side_a_code: link.side_a_code,
        side_z_pk: link.side_z_pk,
        side_z_code: link.side_z_code,
      }

      // Map side A device+interface to link
      map.set(`${link.side_a_pk}:${link.side_a_iface_name}`, linkInfo)
      // Map side Z device+interface to link
      map.set(`${link.side_z_pk}:${link.side_z_iface_name}`, linkInfo)
    }

    return map
  }, [topologyData])

  const showLoading = useDelayedLoading(tunnelLoading || nonTunnelLoading || topologyLoading)

  if (showLoading) {
    return <LoadingSplash />
  }

  if (tunnelError || nonTunnelError) {
    return (
      <div className="flex-1 overflow-auto">
        <div className="max-w-7xl mx-auto px-4 sm:px-8 py-8">
          <div className="text-center py-12">
            <p className="text-lg text-muted-foreground">
              Error loading traffic data: {(tunnelError || nonTunnelError)?.toString()}
            </p>
          </div>
        </div>
      </div>
    )
  }

  if (!tunnelData || !nonTunnelData) {
    return (
      <div className="flex-1 overflow-auto">
        <div className="max-w-7xl mx-auto px-4 sm:px-8 py-8">
          <div className="text-center py-12">
            <p className="text-lg text-muted-foreground">No traffic data available</p>
          </div>
        </div>
      </div>
    )
  }

  // Render a chart section
  const renderChartSection = (section: ChartSection) => {
    if (!visibleSections.has(section)) return null

    let title = ''
    let data = nonTunnelData
    let stacked = false

    switch (section) {
      case 'non-tunnel-stacked':
        title = 'Non-Tunnel Traffic Per Device & Interface (stacked)'
        data = nonTunnelData
        stacked = true
        break
      case 'non-tunnel':
        title = 'Non-Tunnel Traffic Per Device & Interface'
        data = nonTunnelData
        stacked = false
        break
      case 'tunnel-stacked':
        title = 'Tunnel Traffic Per Device & Interface (stacked)'
        data = tunnelData
        stacked = true
        break
      case 'tunnel':
        title = 'Tunnel Traffic Per Device & Interface'
        data = tunnelData
        stacked = false
        break
    }

    return (
      <div key={section} className="border border-border rounded-lg p-4">
        <LazyChart key={`${section}-${layout}`}>
          <TrafficChart
            title={title}
            data={data.points}
            series={data.series}
            stacked={stacked}
            linkLookup={linkLookup}
          />
        </LazyChart>
      </div>
    )
  }

  const gridClass = layout === '2x2' ? 'grid grid-cols-1 lg:grid-cols-2 gap-6' : 'space-y-6'

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-7xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center justify-between gap-4 mb-6 flex-wrap">
          <h1 className="text-2xl font-bold">Traffic</h1>
          <div className="flex items-center gap-3 flex-wrap">
            <ShowGraphsSelector
              sections={chartSections}
              visibleSections={visibleSections}
              onReorder={handleSectionsReorder}
              onToggle={handleSectionToggle}
            />
            <LayoutSelector value={layout} onChange={handleLayoutChange} />
            <BucketSelector
              bucketValue={bucketSize}
              aggValue={aggMethod}
              onBucketChange={handleBucketSizeChange}
              onAggChange={handleAggMethodChange}
            />
            <TimeRangeSelector value={timeRange} onChange={handleTimeRangeChange} />
            <RefreshIntervalSelector value={refreshInterval} onChange={handleRefreshIntervalChange} />
            <button
              onClick={handleManualRefresh}
              className="px-3 py-1.5 text-sm border border-border rounded-md hover:bg-muted transition-colors inline-flex items-center gap-1.5"
              title="Refresh now"
            >
              <RefreshCw className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Charts */}
        <div className={gridClass}>
          {chartSections.map(section => renderChartSection(section))}
        </div>
      </div>
    </div>
  )
}
