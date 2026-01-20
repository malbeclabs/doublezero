import { useState, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { Loader2, Route, Download, ArrowRight, ChevronDown } from 'lucide-react'
import { fetchMetroConnectivity, fetchMetroPathLatency, fetchMetroPathDetail } from '@/lib/api'
import type { MetroPathLatency, MetroPathDetailResponse, PathOptimizeMode } from '@/lib/api'
import { ErrorState } from '@/components/ui/error-state'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'

// Color classes for improvement
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

// Get improvement color class based on percentage
// Positive = green (DZ is faster), slightly negative = yellow, very negative = red
function getImprovementColor(pct: number | null): { bg: string; text: string; hover: string } {
  if (pct === null) return STRENGTH_COLORS.none
  if (pct > 0) return STRENGTH_COLORS.strong    // Any positive = green
  if (pct >= -10) return STRENGTH_COLORS.medium // 0% to -10% = yellow
  return STRENGTH_COLORS.weak                    // < -10% = red
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

export function PathLatencyPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const optimizeParam = searchParams.get('optimize') as PathOptimizeMode | null
  const optimizeMode: PathOptimizeMode = optimizeParam || 'latency'

  const [selectedCell, setSelectedCell] = useState<{ from: string; to: string } | null>(null)
  const queryClient = useQueryClient()

  // Fetch metro connectivity for the matrix structure (metros list)
  const { data: connectivityData, isLoading: connectivityLoading, error: connectivityError, isFetching: connectivityFetching } = useQuery({
    queryKey: ['metro-connectivity'],
    queryFn: fetchMetroConnectivity,
    staleTime: 60000,
    retry: 2,
  })

  // Delay showing loading spinner to avoid flash on fast loads
  const showLoading = useDelayedLoading(connectivityLoading)

  // Fetch path latency data
  const { data: pathLatencyData, isLoading: pathLatencyLoading } = useQuery({
    queryKey: ['metro-path-latency', optimizeMode],
    queryFn: () => fetchMetroPathLatency(optimizeMode),
    staleTime: 60000,
  })

  // Fetch path detail when a cell is selected
  const { data: pathDetailData, isLoading: pathDetailLoading } = useQuery({
    queryKey: ['metro-path-detail', selectedCell?.from, selectedCell?.to, optimizeMode],
    queryFn: () => {
      if (!selectedCell || !connectivityData) return Promise.resolve(null)
      // Find the metro codes for the selected PKs
      const fromMetro = connectivityData.metros.find(m => m.pk === selectedCell.from)
      const toMetro = connectivityData.metros.find(m => m.pk === selectedCell.to)
      if (!fromMetro || !toMetro) return Promise.resolve(null)
      return fetchMetroPathDetail(fromMetro.code, toMetro.code, optimizeMode)
    },
    staleTime: 60000,
    enabled: selectedCell !== null,
  })

  // Build path latency lookup map (by metro PKs)
  const pathLatencyMap = useMemo(() => {
    if (!pathLatencyData) return new Map<string, MetroPathLatency>()
    const map = new Map<string, MetroPathLatency>()
    for (const pl of pathLatencyData.paths) {
      map.set(`${pl.fromMetroPK}:${pl.toMetroPK}`, pl)
    }
    return map
  }, [pathLatencyData])

  // Get selected path latency
  const selectedPathLatency = useMemo(() => {
    if (!selectedCell) return null
    return pathLatencyMap.get(`${selectedCell.from}:${selectedCell.to}`) ?? null
  }, [selectedCell, pathLatencyMap])

  // Export to CSV
  const handleExport = () => {
    if (!pathLatencyData) return

    const headers = ['From Metro', 'To Metro', 'Path Latency (ms)', 'Hop Count', 'Internet Latency (ms)', 'Improvement (%)', 'Bottleneck BW (Gbps)']
    const rows = pathLatencyData.paths.map(pl => [
      pl.fromMetroCode,
      pl.toMetroCode,
      pl.pathLatencyMs.toFixed(1),
      pl.hopCount.toString(),
      pl.internetLatencyMs > 0 ? pl.internetLatencyMs.toFixed(1) : '-',
      pl.improvementPct > 0 ? pl.improvementPct.toFixed(1) : '-',
      pl.bottleneckBwGbps > 0 ? pl.bottleneckBwGbps.toFixed(1) : '-',
    ])

    const csv = [headers.join(','), ...rows.map(row => row.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'path-latency.csv'
    a.click()
    URL.revokeObjectURL(url)
  }

  if (showLoading) {
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (connectivityError || connectivityData?.error) {
    const errorMessage = connectivityData?.error || (connectivityError instanceof Error ? connectivityError.message : 'Unknown error')
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <ErrorState
          title="Failed to load metro data"
          message={errorMessage}
          onRetry={() => queryClient.invalidateQueries({ queryKey: ['metro-connectivity'] })}
          retrying={connectivityFetching}
        />
      </div>
    )
  }

  if (!connectivityData || connectivityData.metros.length === 0) {
    // Don't show "no data" message while still loading (before delay threshold)
    if (connectivityLoading) {
      return <div className="flex-1 bg-background" />
    }
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-muted-foreground">No metros found</div>
      </div>
    )
  }

  const metros = connectivityData.metros

  return (
    <div className="flex-1 flex flex-col bg-background overflow-hidden">
      {/* Header */}
      <div className="border-b border-border px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Route className="h-5 w-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">Path Latency</h1>
          </div>
          <div className="flex items-center gap-3">
            <div className="relative">
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
            <button
              onClick={handleExport}
              className="flex items-center gap-2 px-3 py-1.5 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
            >
              <Download className="h-4 w-4" />
              Export CSV
            </button>
          </div>
        </div>

        <p className="mt-3 text-sm text-muted-foreground">
          Compares end-to-end path latency across the DZ network against public internet latency.
        </p>

        {/* Summary stats */}
        {pathLatencyData && (
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

        {/* Loading indicator */}
        {pathLatencyLoading && (
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
                gridTemplateColumns: `auto repeat(${metros.length}, minmax(48px, 60px))`,
                gridTemplateRows: `auto repeat(${metros.length}, minmax(40px, 48px))`,
              }}
            >
              {/* Top-left corner (empty) */}
              <div className="bg-background sticky top-0 left-0 z-20" />

              {/* Column headers */}
              {metros.map(metro => (
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
              {metros.map(fromMetro => (
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
                  {metros.map(toMetro => {
                    const isSame = fromMetro.pk === toMetro.pk
                    const pathLatency = isSame ? null : pathLatencyMap.get(`${fromMetro.pk}:${toMetro.pk}`) ?? null
                    const isSelected = selectedCell?.from === fromMetro.pk && selectedCell?.to === toMetro.pk

                    return (
                      <div
                        key={`cell-${fromMetro.pk}-${toMetro.pk}`}
                        className="bg-background"
                      >
                        <PathLatencyCell
                          pathLatency={pathLatency}
                          onClick={() => {
                            if (!isSame && pathLatency) {
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
          {selectedPathLatency && selectedCell && (
            <div className="w-80 flex-shrink-0">
              <PathLatencyDetail
                fromCode={connectivityData.metros.find(m => m.pk === selectedCell.from)?.code || ''}
                toCode={connectivityData.metros.find(m => m.pk === selectedCell.to)?.code || ''}
                pathLatency={selectedPathLatency}
                pathDetail={pathDetailData ?? null}
                isLoadingDetail={pathDetailLoading}
                onClose={() => setSelectedCell(null)}
              />
            </div>
          )}
        </div>

        {/* Legend */}
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
      </div>
    </div>
  )
}
