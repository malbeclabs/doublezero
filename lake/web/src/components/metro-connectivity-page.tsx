import { useState, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Loader2, Network, Download, ArrowRight } from 'lucide-react'
import { fetchMetroConnectivity, fetchMetroPaths } from '@/lib/api'
import type { MetroConnectivity, MetroPathsResponse } from '@/lib/api'
import { ErrorState } from '@/components/ui/error-state'

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

export function MetroConnectivityPage() {
  const [selectedCell, setSelectedCell] = useState<{ from: string; to: string } | null>(null)
  const queryClient = useQueryClient()

  const { data, isLoading, error, isFetching } = useQuery({
    queryKey: ['metro-connectivity'],
    queryFn: fetchMetroConnectivity,
    staleTime: 60000, // 1 minute
    retry: 2,
  })

  // Fetch metro paths when a cell is selected
  const { data: metroPathsData, isLoading: metroPathsLoading } = useQuery({
    queryKey: ['metro-paths', selectedCell?.from, selectedCell?.to],
    queryFn: () => {
      if (!selectedCell) return Promise.resolve(null)
      return fetchMetroPaths(selectedCell.from, selectedCell.to, 5)
    },
    staleTime: 60000,
    enabled: selectedCell !== null,
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

  // Get selected connectivity
  const selectedConnectivity = useMemo(() => {
    if (!selectedCell) return null
    return connectivityMap.get(`${selectedCell.from}:${selectedCell.to}`) ?? null
  }, [selectedCell, connectivityMap])

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
            <Network className="h-5 w-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">Metro Connectivity</h1>
          </div>
          <button
            onClick={handleExport}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
          >
            <Download className="h-4 w-4" />
            Export CSV
          </button>
        </div>

        <p className="mt-3 text-sm text-muted-foreground">
          Shows routing paths and bottleneck bandwidth between each metro pair. More paths means better redundancy.
        </p>

        {/* Summary stats */}
        {summary && (
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
                    const isSelected = selectedCell?.from === fromMetro.pk && selectedCell?.to === toMetro.pk

                    return (
                      <div
                        key={`cell-${fromMetro.pk}-${toMetro.pk}`}
                        className="bg-background"
                      >
                        <MatrixCell
                          connectivity={connectivity}
                          onClick={() => {
                            if (!isSame && connectivity) {
                              setSelectedCell(isSelected ? null : { from: fromMetro.pk, to: toMetro.pk })
                            }
                          }}
                          isSelected={isSelected}
                        />
                      </div>
                    )
                  })}
                </>
              ))}
            </div>
          </div>

          {/* Detail panel */}
          {selectedConnectivity && (
            <div className="w-80 flex-shrink-0">
              <ConnectivityDetail
                connectivity={selectedConnectivity}
                pathsData={metroPathsData ?? null}
                isLoadingPaths={metroPathsLoading}
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}
        </div>

        {/* Legend */}
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
      </div>
    </div>
  )
}
