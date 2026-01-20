import { useState, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { Loader2, Grid3X3, Download, ArrowRight, Zap, Network, Route, ChevronDown } from 'lucide-react'
import { fetchMetroConnectivity, fetchLatencyComparison, fetchMetroPathLatency, fetchMetroPathDetail, fetchMetroPaths } from '@/lib/api'
import type { MetroConnectivity, LatencyComparison, MetroPathLatency, MetroPathDetailResponse, MetroPathsResponse, PathOptimizeMode } from '@/lib/api'
import { ErrorState } from '@/components/ui/error-state'

type ViewMode = 'connectivity' | 'vs-internet' | 'path-latency'

// Connectivity strength classification
function getConnectivityStrength(pathCount: number): 'strong' | 'medium' | 'weak' | 'none' {
  if (pathCount >= 3) return 'strong'
  if (pathCount === 2) return 'medium'
  if (pathCount === 1) return 'weak'
  return 'none'
}

// Color classes for connectivity strength
const STRENGTH_COLORS = {
  strong: {
    bg: 'bg-green-100 dark:bg-green-900/40',
    text: 'text-green-700 dark:text-green-400',
    hover: 'hover:bg-green-200 dark:hover:bg-green-900/60',
  },
  medium: {
    bg: 'bg-yellow-100 dark:bg-yellow-900/40',
    text: 'text-yellow-700 dark:text-yellow-400',
    hover: 'hover:bg-yellow-200 dark:hover:bg-yellow-900/60',
  },
  weak: {
    bg: 'bg-red-100 dark:bg-red-900/40',
    text: 'text-red-700 dark:text-red-400',
    hover: 'hover:bg-red-200 dark:hover:bg-red-900/60',
  },
  none: {
    bg: 'bg-muted/50',
    text: 'text-muted-foreground',
    hover: 'hover:bg-muted',
  },
}

// Format metric as latency
function formatMetric(metric: number): string {
  if (metric === 0) return '-'
  return `${(metric / 1000).toFixed(1)}ms`
}

// Cell component for the matrix
function MatrixCell({
  connectivity,
  onClick,
  isSelected,
}: {
  connectivity: MetroConnectivity | null
  onClick: () => void
  isSelected: boolean
}) {
  if (!connectivity) {
    // Diagonal cell (same metro)
    return (
      <div className="w-full h-full flex items-center justify-center bg-muted/30">
        <span className="text-muted-foreground text-xs">-</span>
      </div>
    )
  }

  const strength = getConnectivityStrength(connectivity.pathCount)
  const colors = STRENGTH_COLORS[strength]

  const bwDisplay = connectivity.bottleneckBwGbps && connectivity.bottleneckBwGbps > 0
    ? `${connectivity.bottleneckBwGbps.toFixed(0)}G`
    : null

  return (
    <button
      onClick={onClick}
      className={`w-full h-full flex flex-col items-center justify-center p-1 transition-colors cursor-pointer ${colors.bg} ${colors.hover} ${isSelected ? 'ring-2 ring-accent ring-inset' : ''}`}
      title={`${connectivity.fromMetroCode} → ${connectivity.toMetroCode}: ${connectivity.pathCount} paths, ${connectivity.minHops} hops, ${formatMetric(connectivity.minMetric)}${bwDisplay ? `, ${bwDisplay} bottleneck` : ''}`}
    >
      <span className={`text-sm font-medium ${colors.text}`}>{connectivity.pathCount}</span>
      <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
        <span>{connectivity.minHops}h</span>
        {bwDisplay && <span className="text-primary/70">• {bwDisplay}</span>}
      </div>
    </button>
  )
}

// Detail panel for selected cell
function ConnectivityDetail({
  connectivity,
  pathsData,
  isLoadingPaths,
  onClose,
}: {
  connectivity: MetroConnectivity
  pathsData: MetroPathsResponse | null
  isLoadingPaths: boolean
  onClose: () => void
}) {
  const strength = getConnectivityStrength(connectivity.pathCount)
  const colors = STRENGTH_COLORS[strength]

  return (
    <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-medium flex items-center gap-2">
          <span>{connectivity.fromMetroCode}</span>
          <ArrowRight className="h-4 w-4 text-muted-foreground" />
          <span>{connectivity.toMetroCode}</span>
        </h3>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          Close
        </button>
      </div>

      <div className="grid grid-cols-4 gap-3 mb-4">
        <div className={`rounded-lg p-3 ${colors.bg}`}>
          <div className="text-xs text-muted-foreground mb-1">Paths</div>
          <div className={`text-xl font-bold ${colors.text}`}>{connectivity.pathCount}</div>
        </div>
        <div className="rounded-lg p-3 bg-muted">
          <div className="text-xs text-muted-foreground mb-1">Min Hops</div>
          <div className="text-xl font-bold">{connectivity.minHops}</div>
        </div>
        <div className="rounded-lg p-3 bg-muted">
          <div className="text-xs text-muted-foreground mb-1">Min Latency</div>
          <div className="text-xl font-bold">{formatMetric(connectivity.minMetric)}</div>
        </div>
        <div className="rounded-lg p-3 bg-muted">
          <div className="text-xs text-muted-foreground mb-1">Bottleneck</div>
          <div className="text-xl font-bold">
            {connectivity.bottleneckBwGbps && connectivity.bottleneckBwGbps > 0
              ? `${connectivity.bottleneckBwGbps.toFixed(0)} Gbps`
              : '-'}
          </div>
        </div>
      </div>

      {/* Paths breakdown */}
      <div className="mb-4">
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Available Paths</div>
        {isLoadingPaths ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground py-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading paths...
          </div>
        ) : pathsData && pathsData.paths.length > 0 ? (
          <div className="space-y-3 max-h-64 overflow-y-auto">
            {pathsData.paths.map((path, pathIdx) => (
              <div key={pathIdx} className="bg-muted/50 rounded-lg p-2">
                <div className="flex items-center justify-between text-xs text-muted-foreground mb-1.5">
                  <span>Path {pathIdx + 1}</span>
                  <span>{path.totalHops} hops • {path.latencyMs.toFixed(1)}ms</span>
                </div>
                <div className="flex items-center gap-1 flex-wrap text-xs">
                  {path.hops.map((hop, hopIdx) => (
                    <span key={hopIdx} className="flex items-center gap-1">
                      <span
                        className="px-1.5 py-0.5 bg-background rounded border border-border font-mono"
                        title={`${hop.deviceCode} (${hop.metroCode})`}
                      >
                        {hop.metroCode}
                      </span>
                      {hopIdx < path.hops.length - 1 && (
                        <ArrowRight className="h-3 w-3 text-muted-foreground" />
                      )}
                    </span>
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-sm text-muted-foreground">No path details available</div>
        )}
      </div>

      <div className="flex gap-2 text-sm">
        <Link
          to={pathsData?.paths[0]?.hops[0]?.devicePK
            ? `/topology/graph?type=device&id=${pathsData.paths[0].hops[0].devicePK}`
            : '/topology/graph'}
          className="text-accent hover:underline flex items-center gap-1"
        >
          View {connectivity.fromMetroCode} in Graph
        </Link>
        <span className="text-muted-foreground">|</span>
        <Link
          to={`/topology/map?type=metro&id=${connectivity.fromMetroPK}`}
          className="text-accent hover:underline flex items-center gap-1"
        >
          View in Map
        </Link>
      </div>
    </div>
  )
}

// Get improvement color class based on percentage
// Positive = green (DZ is faster), slightly negative = yellow, very negative = red
function getImprovementColor(pct: number | null): { bg: string; text: string; hover: string } {
  if (pct === null) return STRENGTH_COLORS.none
  if (pct > 0) return STRENGTH_COLORS.strong    // Any positive = green
  if (pct >= -10) return STRENGTH_COLORS.medium // 0% to -10% = yellow
  return STRENGTH_COLORS.weak                    // < -10% = red
}

// Latency cell component for the matrix
function LatencyCell({
  comparison,
  onClick,
  isSelected,
}: {
  comparison: LatencyComparison | null
  onClick: () => void
  isSelected: boolean
}) {
  if (!comparison) {
    // Diagonal cell or no data
    return (
      <div className="w-full h-full flex items-center justify-center bg-muted/30">
        <span className="text-muted-foreground text-xs">-</span>
      </div>
    )
  }

  const colors = getImprovementColor(comparison.rtt_improvement_pct)
  const hasInternet = comparison.internet_sample_count > 0 && comparison.rtt_improvement_pct !== null

  return (
    <button
      onClick={onClick}
      className={`w-full h-full flex flex-col items-center justify-center p-1 transition-colors cursor-pointer ${colors.bg} ${colors.hover} ${isSelected ? 'ring-2 ring-accent ring-inset' : ''}`}
      title={`${comparison.origin_metro_code} → ${comparison.target_metro_code}: DZ ${comparison.dz_avg_rtt_ms.toFixed(1)}ms${hasInternet ? ` vs Internet ${comparison.internet_avg_rtt_ms.toFixed(1)}ms (${comparison.rtt_improvement_pct?.toFixed(0)}% faster)` : ''}`}
    >
      <span className={`text-sm font-medium ${colors.text}`}>
        {comparison.dz_avg_rtt_ms.toFixed(1)}
      </span>
      {hasInternet && (
        <span className="text-[10px] text-green-600 dark:text-green-400">
          {comparison.rtt_improvement_pct?.toFixed(0)}%↑
        </span>
      )}
    </button>
  )
}

// Detail panel for latency comparison
function LatencyDetail({
  comparison,
  onClose,
}: {
  comparison: LatencyComparison
  onClose: () => void
}) {
  const colors = getImprovementColor(comparison.rtt_improvement_pct)
  const hasInternet = comparison.internet_sample_count > 0 && comparison.rtt_improvement_pct !== null

  return (
    <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-medium flex items-center gap-2">
          <span>{comparison.origin_metro_code}</span>
          <ArrowRight className="h-4 w-4 text-muted-foreground" />
          <span>{comparison.target_metro_code}</span>
        </h3>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          Close
        </button>
      </div>

      {/* DZ Latency */}
      <div className="mb-4">
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">DoubleZero</div>
        <div className="grid grid-cols-3 gap-2">
          <div className="rounded-lg p-2 bg-muted">
            <div className="text-[10px] text-muted-foreground mb-0.5">Avg RTT</div>
            <div className="text-lg font-bold">{comparison.dz_avg_rtt_ms.toFixed(1)}ms</div>
          </div>
          <div className="rounded-lg p-2 bg-muted">
            <div className="text-[10px] text-muted-foreground mb-0.5">P95 RTT</div>
            <div className="text-lg font-bold">{comparison.dz_p95_rtt_ms.toFixed(1)}ms</div>
          </div>
          <div className="rounded-lg p-2 bg-muted">
            <div className="text-[10px] text-muted-foreground mb-0.5">Loss</div>
            <div className="text-lg font-bold">{comparison.dz_loss_pct.toFixed(2)}%</div>
          </div>
        </div>
      </div>

      {/* Internet Latency */}
      {hasInternet && (
        <>
          <div className="mb-4">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Public Internet</div>
            <div className="grid grid-cols-2 gap-2">
              <div className="rounded-lg p-2 bg-muted">
                <div className="text-[10px] text-muted-foreground mb-0.5">Avg RTT</div>
                <div className="text-lg font-bold">{comparison.internet_avg_rtt_ms.toFixed(1)}ms</div>
              </div>
              <div className="rounded-lg p-2 bg-muted">
                <div className="text-[10px] text-muted-foreground mb-0.5">P95 RTT</div>
                <div className="text-lg font-bold">{comparison.internet_p95_rtt_ms.toFixed(1)}ms</div>
              </div>
            </div>
          </div>

          {/* Improvement */}
          <div className={`rounded-lg p-3 ${colors.bg} mb-4`}>
            <div className="text-xs text-muted-foreground mb-1">DZ Advantage</div>
            <div className={`text-xl font-bold ${colors.text}`}>
              {comparison.rtt_improvement_pct?.toFixed(1)}% faster
            </div>
            <div className="text-xs text-muted-foreground">
              {(comparison.internet_avg_rtt_ms - comparison.dz_avg_rtt_ms).toFixed(1)}ms saved
            </div>
          </div>
        </>
      )}

      <div className="text-xs text-muted-foreground">
        DZ samples: {comparison.dz_sample_count.toLocaleString()}
        {hasInternet && ` • Internet samples: ${comparison.internet_sample_count.toLocaleString()}`}
      </div>
    </div>
  )
}

// Path latency cell component for the matrix
function PathLatencyCell({
  pathLatency,
  onClick,
  isSelected,
}: {
  pathLatency: MetroPathLatency | null
  onClick: () => void
  isSelected: boolean
}) {
  if (!pathLatency) {
    // Diagonal cell or no data
    return (
      <div className="w-full h-full flex items-center justify-center bg-muted/30">
        <span className="text-muted-foreground text-xs">-</span>
      </div>
    )
  }

  const colors = getImprovementColor(pathLatency.improvementPct)
  const hasInternet = pathLatency.internetLatencyMs > 0

  return (
    <button
      onClick={onClick}
      className={`w-full h-full flex flex-col items-center justify-center p-1 transition-colors cursor-pointer ${colors.bg} ${colors.hover} ${isSelected ? 'ring-2 ring-accent ring-inset' : ''}`}
      title={`${pathLatency.fromMetroCode} → ${pathLatency.toMetroCode}: ${pathLatency.pathLatencyMs.toFixed(1)}ms (${pathLatency.hopCount} hops)${hasInternet ? ` vs Internet ${pathLatency.internetLatencyMs.toFixed(1)}ms` : ''}`}
    >
      <span className={`text-sm font-medium ${colors.text}`}>
        {pathLatency.pathLatencyMs.toFixed(1)}
      </span>
      <span className="text-[10px] text-muted-foreground">
        {pathLatency.hopCount}h
      </span>
    </button>
  )
}

// Detail panel for path latency with breakdown
function PathLatencyDetail({
  fromCode,
  toCode,
  pathLatency,
  pathDetail,
  isLoadingDetail,
  onClose,
}: {
  fromCode: string
  toCode: string
  pathLatency: MetroPathLatency
  pathDetail: MetroPathDetailResponse | null
  isLoadingDetail: boolean
  onClose: () => void
}) {
  const colors = getImprovementColor(pathLatency.improvementPct)
  const hasInternet = pathLatency.internetLatencyMs > 0

  return (
    <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-medium flex items-center gap-2">
          <span>{fromCode}</span>
          <ArrowRight className="h-4 w-4 text-muted-foreground" />
          <span>{toCode}</span>
        </h3>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          Close
        </button>
      </div>

      {/* Summary stats */}
      <div className="grid grid-cols-3 gap-2 mb-4">
        <div className="rounded-lg p-2 bg-muted">
          <div className="text-[10px] text-muted-foreground mb-0.5">DZ Latency</div>
          <div className="text-lg font-bold">{pathLatency.pathLatencyMs.toFixed(1)}ms</div>
        </div>
        <div className="rounded-lg p-2 bg-muted">
          <div className="text-[10px] text-muted-foreground mb-0.5">Hops</div>
          <div className="text-lg font-bold">{pathLatency.hopCount}</div>
        </div>
        {pathLatency.bottleneckBwGbps > 0 && (
          <div className="rounded-lg p-2 bg-muted">
            <div className="text-[10px] text-muted-foreground mb-0.5">Bottleneck</div>
            <div className="text-lg font-bold">{pathLatency.bottleneckBwGbps.toFixed(0)} Gbps</div>
          </div>
        )}
      </div>

      {/* Internet comparison */}
      {hasInternet && (
        <div className={`rounded-lg p-3 ${colors.bg} mb-4`}>
          <div className="text-xs text-muted-foreground mb-1">vs Internet ({pathLatency.internetLatencyMs.toFixed(1)}ms)</div>
          <div className={`text-xl font-bold ${colors.text}`}>
            {pathLatency.improvementPct > 0 ? '+' : ''}{pathLatency.improvementPct.toFixed(1)}% {pathLatency.improvementPct > 0 ? 'faster' : 'slower'}
          </div>
        </div>
      )}

      {/* Path breakdown */}
      <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Path Breakdown</div>
      {isLoadingDetail ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground py-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading path details...
        </div>
      ) : pathDetail && pathDetail.hops.length > 0 ? (
        <div className="space-y-1">
          {pathDetail.hops.map((hop, idx) => (
            <div key={idx} className="flex items-center gap-2 text-sm">
              <span className="font-mono text-xs text-muted-foreground w-8">{hop.metroCode}</span>
              <span className="font-medium">{hop.deviceCode}</span>
              {idx < pathDetail.hops.length - 1 && (
                <span className="text-muted-foreground ml-auto">
                  → {hop.linkLatency.toFixed(2)}ms
                </span>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div className="text-sm text-muted-foreground">No path details available</div>
      )}
    </div>
  )
}

export function MetroMatrixPage() {
  const { view } = useParams<{ view: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const optimizeParam = searchParams.get('optimize') as PathOptimizeMode | null
  const optimizeMode: PathOptimizeMode = optimizeParam || 'latency'

  const viewMode: ViewMode = view === 'vs-internet' ? 'vs-internet' : view === 'path-latency' ? 'path-latency' : 'connectivity'
  const [selectedCell, setSelectedCell] = useState<{ from: string; to: string } | null>(null)
  const queryClient = useQueryClient()

  const { data, isLoading, error, isFetching } = useQuery({
    queryKey: ['metro-connectivity'],
    queryFn: fetchMetroConnectivity,
    staleTime: 60000, // 1 minute
    retry: 2, // Built-in retry for transient errors
  })

  const { data: latencyData, isLoading: latencyLoading } = useQuery({
    queryKey: ['latency-comparison'],
    queryFn: fetchLatencyComparison,
    staleTime: 60000,
    enabled: viewMode === 'vs-internet',
  })

  const { data: pathLatencyData, isLoading: pathLatencyLoading } = useQuery({
    queryKey: ['metro-path-latency', optimizeMode],
    queryFn: () => fetchMetroPathLatency(optimizeMode),
    staleTime: 60000,
    enabled: viewMode === 'path-latency',
  })

  // Fetch path detail when a cell is selected in path-latency mode
  const { data: pathDetailData, isLoading: pathDetailLoading } = useQuery({
    queryKey: ['metro-path-detail', selectedCell?.from, selectedCell?.to, optimizeMode],
    queryFn: () => {
      if (!selectedCell) return Promise.resolve(null)
      // Find the metro codes for the selected PKs
      const fromMetro = data?.metros.find(m => m.pk === selectedCell.from)
      const toMetro = data?.metros.find(m => m.pk === selectedCell.to)
      if (!fromMetro || !toMetro) return Promise.resolve(null)
      return fetchMetroPathDetail(fromMetro.code, toMetro.code, optimizeMode)
    },
    staleTime: 60000,
    enabled: viewMode === 'path-latency' && selectedCell !== null,
  })

  // Fetch metro paths when a cell is selected in connectivity mode
  const { data: metroPathsData, isLoading: metroPathsLoading } = useQuery({
    queryKey: ['metro-paths', selectedCell?.from, selectedCell?.to],
    queryFn: () => {
      if (!selectedCell) return Promise.resolve(null)
      return fetchMetroPaths(selectedCell.from, selectedCell.to, 5)
    },
    staleTime: 60000,
    enabled: viewMode === 'connectivity' && selectedCell !== null,
  })

  // Build connectivity lookup map
  const connectivityMap = useMemo(() => {
    if (!data) return new Map<string, MetroConnectivity>()
    const map = new Map<string, MetroConnectivity>()
    for (const conn of data.connectivity) {
      map.set(`${conn.fromMetroPK}:${conn.toMetroPK}`, conn)
    }
    return map
  }, [data])

  // Build latency comparison lookup map (by metro PKs)
  const latencyMap = useMemo(() => {
    if (!latencyData) return new Map<string, LatencyComparison>()
    const map = new Map<string, LatencyComparison>()
    for (const comp of latencyData.comparisons) {
      map.set(`${comp.origin_metro_pk}:${comp.target_metro_pk}`, comp)
    }
    return map
  }, [latencyData])

  // Build path latency lookup map (by metro PKs)
  const pathLatencyMap = useMemo(() => {
    if (!pathLatencyData) return new Map<string, MetroPathLatency>()
    const map = new Map<string, MetroPathLatency>()
    for (const pl of pathLatencyData.paths) {
      map.set(`${pl.fromMetroPK}:${pl.toMetroPK}`, pl)
    }
    return map
  }, [pathLatencyData])

  // Get selected connectivity
  const selectedConnectivity = useMemo(() => {
    if (!selectedCell) return null
    return connectivityMap.get(`${selectedCell.from}:${selectedCell.to}`) ?? null
  }, [selectedCell, connectivityMap])

  // Get selected latency comparison
  const selectedLatency = useMemo(() => {
    if (!selectedCell) return null
    return latencyMap.get(`${selectedCell.from}:${selectedCell.to}`) ?? null
  }, [selectedCell, latencyMap])

  // Get selected path latency
  const selectedPathLatency = useMemo(() => {
    if (!selectedCell) return null
    return pathLatencyMap.get(`${selectedCell.from}:${selectedCell.to}`) ?? null
  }, [selectedCell, pathLatencyMap])

  // Export to CSV
  const handleExport = () => {
    if (!data) return

    const headers = ['From Metro', 'To Metro', 'Path Count', 'Min Hops', 'Min Latency (ms)', 'Bottleneck BW (Gbps)']
    const rows = data.connectivity.map(conn => [
      conn.fromMetroCode,
      conn.toMetroCode,
      conn.pathCount.toString(),
      conn.minHops.toString(),
      (conn.minMetric / 1000).toFixed(1),
      conn.bottleneckBwGbps && conn.bottleneckBwGbps > 0 ? conn.bottleneckBwGbps.toFixed(1) : '-',
    ])

    const csv = [headers.join(','), ...rows.map(row => row.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'metro-connectivity-matrix.csv'
    a.click()
    URL.revokeObjectURL(url)
  }

  // Summary stats
  const summary = useMemo(() => {
    if (!data) return null
    const connections = data.connectivity.filter(c =>
      // Dedupe by only counting fromPK < toPK
      c.fromMetroPK < c.toMetroPK
    )
    const strong = connections.filter(c => getConnectivityStrength(c.pathCount) === 'strong').length
    const medium = connections.filter(c => getConnectivityStrength(c.pathCount) === 'medium').length
    const weak = connections.filter(c => getConnectivityStrength(c.pathCount) === 'weak').length
    return { total: connections.length, strong, medium, weak }
  }, [data])

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || data?.error) {
    const errorMessage = data?.error || (error instanceof Error ? error.message : 'Unknown error')
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <ErrorState
          title="Failed to load metro connectivity"
          message={errorMessage}
          onRetry={() => queryClient.invalidateQueries({ queryKey: ['metro-connectivity'] })}
          retrying={isFetching}
        />
      </div>
    )
  }

  if (!data || data.metros.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-muted-foreground">No metros with ISIS connectivity found</div>
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col bg-background overflow-hidden">
      {/* Header */}
      <div className="border-b border-border px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Grid3X3 className="h-5 w-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">Metro Connectivity Matrix</h1>
          </div>
          <div className="flex items-center gap-3">
            {/* View mode toggle */}
            <div className="flex items-center bg-muted rounded-md p-0.5">
              <Link
                to="/topology/metro-matrix/connectivity"
                className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded transition-colors ${
                  viewMode === 'connectivity'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Network className="h-4 w-4" />
                Connectivity
              </Link>
              <Link
                to="/topology/metro-matrix/vs-internet"
                className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded transition-colors ${
                  viewMode === 'vs-internet'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Zap className="h-4 w-4" />
                DZ vs Internet
              </Link>
              <Link
                to={`/topology/metro-matrix/path-latency?optimize=${optimizeMode}`}
                className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded transition-colors ${
                  viewMode === 'path-latency'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Route className="h-4 w-4" />
                Path Latency
              </Link>
            </div>
            <button
              onClick={handleExport}
              className="flex items-center gap-2 px-3 py-1.5 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
            >
              <Download className="h-4 w-4" />
              Export CSV
            </button>
          </div>
        </div>

        {/* View descriptions */}
        {viewMode === 'connectivity' && (
          <p className="mt-3 text-sm text-muted-foreground">
            Shows routing paths and bottleneck bandwidth between each metro pair. More paths means better redundancy. Bandwidth shows the minimum link capacity along the best path.
          </p>
        )}
        {viewMode === 'vs-internet' && (
          <p className="mt-3 text-sm text-muted-foreground">
            Compares measured latency on direct DZ links against public internet latency for the same metro pairs. Only shows pairs with direct physical connections (not routed paths).
          </p>
        )}
        {viewMode === 'path-latency' && (
          <div className="mt-3 flex items-start justify-between gap-4">
            <p className="text-sm text-muted-foreground">
              Compares end-to-end path latency across the DZ network (summing ISIS link metrics along the shortest path) against public internet latency. Covers all reachable metro pairs, not just directly connected ones.
            </p>
            <div className="relative flex-shrink-0">
              <select
                value={optimizeMode}
                onChange={(e) => {
                  const newMode = e.target.value as PathOptimizeMode
                  setSearchParams({ optimize: newMode })
                }}
                className="appearance-none bg-muted hover:bg-muted/80 rounded-md px-3 py-1.5 pr-8 text-sm cursor-pointer transition-colors"
              >
                <option value="latency">Optimize: Latency</option>
                <option value="hops">Optimize: Hops</option>
                <option value="bandwidth">Optimize: Bandwidth</option>
              </select>
              <ChevronDown className="absolute right-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
            </div>
          </div>
        )}

        {/* Summary stats - connectivity mode */}
        {viewMode === 'connectivity' && summary && (
          <div className="flex gap-6 mt-4 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Metros:</span>
              <span className="font-medium">{data.metros.length}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Connections:</span>
              <span className="font-medium">{summary.total}</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded bg-green-500" />
              <span className="text-muted-foreground">Strong (3+):</span>
              <span className="font-medium text-green-600 dark:text-green-400">{summary.strong}</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded bg-yellow-500" />
              <span className="text-muted-foreground">Medium (2):</span>
              <span className="font-medium text-yellow-600 dark:text-yellow-400">{summary.medium}</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded bg-red-500" />
              <span className="text-muted-foreground">Weak (1):</span>
              <span className="font-medium text-red-600 dark:text-red-400">{summary.weak}</span>
            </div>
          </div>
        )}

        {/* Summary stats - vs-internet mode */}
        {viewMode === 'vs-internet' && latencyData && (
          <div className="flex gap-6 mt-4 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Metro Pairs:</span>
              <span className="font-medium">{latencyData.summary.total_pairs}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">With Internet Data:</span>
              <span className="font-medium">{latencyData.summary.pairs_with_data}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Avg Improvement:</span>
              <span className="font-medium text-green-600 dark:text-green-400">
                {latencyData.summary.avg_improvement_pct.toFixed(1)}%
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Max Improvement:</span>
              <span className="font-medium text-green-600 dark:text-green-400">
                {latencyData.summary.max_improvement_pct.toFixed(1)}%
              </span>
            </div>
          </div>
        )}

        {/* Loading indicator for vs-internet mode */}
        {viewMode === 'vs-internet' && latencyLoading && (
          <div className="flex items-center gap-2 mt-4 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading latency comparison data...
          </div>
        )}

        {/* Summary stats - path-latency mode */}
        {viewMode === 'path-latency' && pathLatencyData && (
          <div className="flex gap-6 mt-4 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Metro Pairs:</span>
              <span className="font-medium">{pathLatencyData.summary.totalPairs}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">With Internet Data:</span>
              <span className="font-medium">{pathLatencyData.summary.pairsWithInternet}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Avg Improvement:</span>
              <span className="font-medium text-green-600 dark:text-green-400">
                {pathLatencyData.summary.avgImprovementPct.toFixed(1)}%
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Max Improvement:</span>
              <span className="font-medium text-green-600 dark:text-green-400">
                {pathLatencyData.summary.maxImprovementPct.toFixed(1)}%
              </span>
            </div>
          </div>
        )}

        {/* Loading indicator for path-latency mode */}
        {viewMode === 'path-latency' && pathLatencyLoading && (
          <div className="flex items-center gap-2 mt-4 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading path latency data...
          </div>
        )}
      </div>

      {/* Matrix grid */}
      <div className="flex-1 overflow-auto p-6">
        <div className="flex gap-6">
          {/* Matrix */}
          <div className="overflow-auto">
            <div
              className="grid gap-px bg-border"
              style={{
                gridTemplateColumns: `auto repeat(${data.metros.length}, minmax(48px, 60px))`,
                gridTemplateRows: `auto repeat(${data.metros.length}, minmax(40px, 48px))`,
              }}
            >
              {/* Top-left corner (empty) */}
              <div className="bg-background sticky top-0 left-0 z-20" />

              {/* Column headers */}
              {data.metros.map(metro => (
                <div
                  key={`col-${metro.pk}`}
                  className="bg-muted px-1 py-2 text-xs font-medium text-center sticky top-0 z-10 flex items-end justify-center"
                  title={metro.name}
                >
                  <span className="writing-mode-vertical transform -rotate-45 origin-center whitespace-nowrap">
                    {metro.code}
                  </span>
                </div>
              ))}

              {/* Rows */}
              {data.metros.map(fromMetro => (
                <>
                  {/* Row header */}
                  <div
                    key={`row-${fromMetro.pk}`}
                    className="bg-muted px-2 py-1 text-xs font-medium flex items-center justify-end sticky left-0 z-10"
                    title={fromMetro.name}
                  >
                    {fromMetro.code}
                  </div>

                  {/* Cells */}
                  {data.metros.map(toMetro => {
                    const isSame = fromMetro.pk === toMetro.pk
                    const connectivity = isSame ? null : connectivityMap.get(`${fromMetro.pk}:${toMetro.pk}`) ?? null
                    const latency = isSame ? null : latencyMap.get(`${fromMetro.pk}:${toMetro.pk}`) ?? null
                    const pathLatency = isSame ? null : pathLatencyMap.get(`${fromMetro.pk}:${toMetro.pk}`) ?? null
                    const isSelected = selectedCell?.from === fromMetro.pk && selectedCell?.to === toMetro.pk

                    return (
                      <div
                        key={`cell-${fromMetro.pk}-${toMetro.pk}`}
                        className="bg-background"
                      >
                        {viewMode === 'connectivity' ? (
                          <MatrixCell
                            connectivity={connectivity}
                            onClick={() => {
                              if (!isSame && connectivity) {
                                setSelectedCell(isSelected ? null : { from: fromMetro.pk, to: toMetro.pk })
                              }
                            }}
                            isSelected={isSelected}
                          />
                        ) : viewMode === 'vs-internet' ? (
                          <LatencyCell
                            comparison={latency}
                            onClick={() => {
                              if (!isSame && latency) {
                                setSelectedCell(isSelected ? null : { from: fromMetro.pk, to: toMetro.pk })
                              }
                            }}
                            isSelected={isSelected}
                          />
                        ) : (
                          <PathLatencyCell
                            pathLatency={pathLatency}
                            onClick={() => {
                              if (!isSame && pathLatency) {
                                setSelectedCell(isSelected ? null : { from: fromMetro.pk, to: toMetro.pk })
                              }
                            }}
                            isSelected={isSelected}
                          />
                        )}
                      </div>
                    )
                  })}
                </>
              ))}
            </div>
          </div>

          {/* Detail panel - connectivity mode */}
          {viewMode === 'connectivity' && selectedConnectivity && (
            <div className="w-80 flex-shrink-0">
              <ConnectivityDetail
                connectivity={selectedConnectivity}
                pathsData={metroPathsData ?? null}
                isLoadingPaths={metroPathsLoading}
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}

          {/* Detail panel - vs-internet mode */}
          {viewMode === 'vs-internet' && selectedLatency && (
            <div className="w-80 flex-shrink-0">
              <LatencyDetail
                comparison={selectedLatency}
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}

          {/* Detail panel - path-latency mode */}
          {viewMode === 'path-latency' && selectedPathLatency && selectedCell && (
            <div className="w-80 flex-shrink-0">
              <PathLatencyDetail
                fromCode={data?.metros.find(m => m.pk === selectedCell.from)?.code || ''}
                toCode={data?.metros.find(m => m.pk === selectedCell.to)?.code || ''}
                pathLatency={selectedPathLatency}
                pathDetail={pathDetailData ?? null}
                isLoadingDetail={pathDetailLoading}
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}
        </div>

        {/* Legend - connectivity mode */}
        {viewMode === 'connectivity' && (
          <div className="mt-6 flex items-center gap-6 text-xs text-muted-foreground">
            <span className="font-medium">Legend:</span>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-green-100 dark:bg-green-900/40 border border-green-200 dark:border-green-800" />
              <span>Strong (3+ paths)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-yellow-100 dark:bg-yellow-900/40 border border-yellow-200 dark:border-yellow-800" />
              <span>Medium (2 paths)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-red-100 dark:bg-red-900/40 border border-red-200 dark:border-red-800" />
              <span>Weak (1 path)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-muted/50 border border-border" />
              <span>No connection</span>
            </div>
          </div>
        )}

        {/* Legend - vs-internet mode */}
        {viewMode === 'vs-internet' && (
          <div className="mt-6 flex items-center gap-6 text-xs text-muted-foreground">
            <span className="font-medium">Legend (DZ vs Internet):</span>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-green-100 dark:bg-green-900/40 border border-green-200 dark:border-green-800" />
              <span>DZ faster</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-yellow-100 dark:bg-yellow-900/40 border border-yellow-200 dark:border-yellow-800" />
              <span>Similar (0 to -10%)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-red-100 dark:bg-red-900/40 border border-red-200 dark:border-red-800" />
              <span>Internet faster (&lt;-10%)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-muted/50 border border-border" />
              <span>No internet data</span>
            </div>
          </div>
        )}

        {/* Legend - path-latency mode */}
        {viewMode === 'path-latency' && (
          <div className="mt-6 flex items-center gap-6 text-xs text-muted-foreground">
            <span className="font-medium">Legend (DZ Path vs Internet):</span>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-green-100 dark:bg-green-900/40 border border-green-200 dark:border-green-800" />
              <span>DZ faster</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-yellow-100 dark:bg-yellow-900/40 border border-yellow-200 dark:border-yellow-800" />
              <span>Similar (0 to -10%)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-red-100 dark:bg-red-900/40 border border-red-200 dark:border-red-800" />
              <span>Internet faster (&lt;-10%)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-muted/50 border border-border" />
              <span>No internet data</span>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
