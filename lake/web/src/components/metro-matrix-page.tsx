import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Loader2, Grid3X3, Download, ArrowRight, Zap, Network } from 'lucide-react'
import { fetchMetroConnectivity, fetchLatencyComparison } from '@/lib/api'
import type { MetroConnectivity, LatencyComparison } from '@/lib/api'

type ViewMode = 'connectivity' | 'latency'

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

  return (
    <button
      onClick={onClick}
      className={`w-full h-full flex flex-col items-center justify-center p-1 transition-colors cursor-pointer ${colors.bg} ${colors.hover} ${isSelected ? 'ring-2 ring-accent ring-inset' : ''}`}
      title={`${connectivity.fromMetroCode} → ${connectivity.toMetroCode}: ${connectivity.pathCount} paths, ${connectivity.minHops} hops, ${formatMetric(connectivity.minMetric)}`}
    >
      <span className={`text-sm font-medium ${colors.text}`}>{connectivity.pathCount}</span>
      <span className="text-[10px] text-muted-foreground">{connectivity.minHops}h</span>
    </button>
  )
}

// Detail panel for selected cell
function ConnectivityDetail({
  connectivity,
  onClose,
}: {
  connectivity: MetroConnectivity
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

      <div className="grid grid-cols-3 gap-4 mb-4">
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
      </div>

      <div className="flex gap-2 text-sm">
        <Link
          to={`/topology/graph?highlight-metro=${connectivity.fromMetroPK}`}
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
function getImprovementColor(pct: number | null): { bg: string; text: string; hover: string } {
  if (pct === null) return STRENGTH_COLORS.none
  if (pct >= 30) return STRENGTH_COLORS.strong
  if (pct >= 10) return STRENGTH_COLORS.medium
  return STRENGTH_COLORS.weak
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

export function MetroMatrixPage() {
  const [selectedCell, setSelectedCell] = useState<{ from: string; to: string } | null>(null)
  const [viewMode, setViewMode] = useState<ViewMode>('connectivity')

  const { data, isLoading, error } = useQuery({
    queryKey: ['metro-connectivity'],
    queryFn: fetchMetroConnectivity,
    staleTime: 60000, // 1 minute
  })

  const { data: latencyData, isLoading: latencyLoading } = useQuery({
    queryKey: ['latency-comparison'],
    queryFn: fetchLatencyComparison,
    staleTime: 60000,
    enabled: viewMode === 'latency',
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

  // Export to CSV
  const handleExport = () => {
    if (!data) return

    const headers = ['From Metro', 'To Metro', 'Path Count', 'Min Hops', 'Min Latency (ms)']
    const rows = data.connectivity.map(conn => [
      conn.fromMetroCode,
      conn.toMetroCode,
      conn.pathCount.toString(),
      conn.minHops.toString(),
      (conn.minMetric / 1000).toFixed(1),
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
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-destructive">
          Failed to load metro connectivity: {data?.error || (error instanceof Error ? error.message : 'Unknown error')}
        </div>
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
              <button
                onClick={() => setViewMode('connectivity')}
                className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded transition-colors ${
                  viewMode === 'connectivity'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Network className="h-4 w-4" />
                Connectivity
              </button>
              <button
                onClick={() => setViewMode('latency')}
                className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded transition-colors ${
                  viewMode === 'latency'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Zap className="h-4 w-4" />
                DZ vs Internet
              </button>
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

        {/* Summary stats - latency mode */}
        {viewMode === 'latency' && latencyData && (
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

        {/* Loading indicator for latency mode */}
        {viewMode === 'latency' && latencyLoading && (
          <div className="flex items-center gap-2 mt-4 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading latency comparison data...
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
                        ) : (
                          <LatencyCell
                            comparison={latency}
                            onClick={() => {
                              if (!isSame && latency) {
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
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}

          {/* Detail panel - latency mode */}
          {viewMode === 'latency' && selectedLatency && (
            <div className="w-80 flex-shrink-0">
              <LatencyDetail
                comparison={selectedLatency}
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

        {/* Legend - latency mode */}
        {viewMode === 'latency' && (
          <div className="mt-6 flex items-center gap-6 text-xs text-muted-foreground">
            <span className="font-medium">Legend (DZ Advantage):</span>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-green-100 dark:bg-green-900/40 border border-green-200 dark:border-green-800" />
              <span>30%+ faster</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-yellow-100 dark:bg-yellow-900/40 border border-yellow-200 dark:border-yellow-800" />
              <span>10-30% faster</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded bg-red-100 dark:bg-red-900/40 border border-red-200 dark:border-red-800" />
              <span>&lt;10% faster</span>
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
