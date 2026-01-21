import { useState, useMemo, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Zap, Download, ArrowRight, ChevronUp, ChevronDown, Search, X, MapPin } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { fetchLatencyComparison, fetchLatencyHistory } from '@/lib/api'
import type { LatencyComparison } from '@/lib/api'
import { ErrorState } from '@/components/ui/error-state'
import { cn } from '@/lib/utils'
import { useTheme } from '@/hooks/use-theme'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'

// Parse metro filters from URL
function parseMetroFilters(searchParam: string): string[] {
  if (!searchParam) return []
  return searchParam.split(',').map(f => f.trim()).filter(Boolean)
}

type SortField = 'route' | 'dz' | 'internet' | 'improvement' | 'dzJitter' | 'internetJitter'
type SortDirection = 'asc' | 'desc'

// Get improvement color class based on percentage
function getImprovementColor(pct: number): string {
  if (pct > 0) return 'text-green-600 dark:text-green-400'
  if (pct >= -10) return 'text-yellow-600 dark:text-yellow-400'
  return 'text-red-600 dark:text-red-400'
}

function getImprovementBg(pct: number): string {
  if (pct > 0) return 'bg-green-100 dark:bg-green-900/40'
  if (pct >= -10) return 'bg-yellow-100 dark:bg-yellow-900/40'
  return 'bg-red-100 dark:bg-red-900/40'
}

export function DzVsInternetPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [sortField, setSortField] = useState<SortField>('improvement')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')
  const queryClient = useQueryClient()
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  // Fetch latency comparison data
  const { data: latencyData, isLoading: latencyLoading, error: latencyError, isFetching: latencyFetching } = useQuery({
    queryKey: ['latency-comparison'],
    queryFn: fetchLatencyComparison,
    staleTime: 0,
    retry: 2,
  })

  // Delay showing loading spinner to avoid flash on fast loads
  const showLoading = useDelayedLoading(latencyLoading)

  // Get selected route from URL
  const selectedRoute = searchParams.get('route')

  // Get metro filters from URL
  const metroFilters = useMemo(() => {
    return parseMetroFilters(searchParams.get('metros') || '')
  }, [searchParams])

  // Find the selected comparison from the data based on URL param
  const selectedComparison = useMemo(() => {
    if (!selectedRoute || !latencyData) return null
    const [origin, target] = selectedRoute.split('-')
    return latencyData.comparisons.find(
      c => c.origin_metro_code === origin && c.target_metro_code === target
    ) ?? null
  }, [selectedRoute, latencyData])

  // Update URL when selection changes (preserve metros filter)
  const setSelectedComparison = useCallback((comp: LatencyComparison | null) => {
    setSearchParams(prev => {
      if (comp) {
        prev.set('route', `${comp.origin_metro_code}-${comp.target_metro_code}`)
      } else {
        prev.delete('route')
      }
      return prev
    })
  }, [setSearchParams])

  // Remove a metro filter
  const removeMetroFilter = useCallback((metro: string) => {
    setSearchParams(prev => {
      const current = parseMetroFilters(prev.get('metros') || '')
      const newFilters = current.filter(m => m !== metro)
      if (newFilters.length === 0) {
        prev.delete('metros')
      } else {
        prev.set('metros', newFilters.join(','))
      }
      return prev
    })
  }, [setSearchParams])

  // Clear all metro filters
  const clearAllFilters = useCallback(() => {
    setSearchParams(prev => {
      prev.delete('metros')
      return prev
    })
  }, [setSearchParams])

  // Open the search spotlight
  const openSearch = useCallback(() => {
    window.dispatchEvent(new CustomEvent('open-search'))
  }, [])

  // Fetch latency history for selected comparison
  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['latency-history', selectedComparison?.origin_metro_code, selectedComparison?.target_metro_code],
    queryFn: () => fetchLatencyHistory(
      selectedComparison!.origin_metro_code,
      selectedComparison!.target_metro_code
    ),
    enabled: !!selectedComparison,
    staleTime: 0,
  })

  // Prepare chart data from history
  const chartData = useMemo(() => {
    if (!historyData?.points) return []
    return historyData.points.map(p => ({
      time: new Date(p.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      dzRtt: p.dz_avg_rtt_ms,
      inetRtt: p.inet_avg_rtt_ms,
      dzJitter: p.dz_avg_jitter_ms,
      inetJitter: p.inet_avg_jitter_ms,
    }))
  }, [historyData])

  // Chart colors
  const dzColor = isDark ? '#22c55e' : '#16a34a'
  const inetColor = isDark ? '#94a3b8' : '#64748b'

  // Filter to only comparisons with internet data, apply metro filters, and sort
  const comparisons = useMemo(() => {
    if (!latencyData) return []

    let filtered = latencyData.comparisons.filter(c => c.internet_sample_count > 0)

    // Apply metro filters - show routes where origin OR target matches any filter
    if (metroFilters.length > 0) {
      filtered = filtered.filter(c =>
        metroFilters.some(m =>
          c.origin_metro_code.toLowerCase() === m.toLowerCase() ||
          c.target_metro_code.toLowerCase() === m.toLowerCase()
        )
      )
    }

    return filtered.sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'route':
          cmp = `${a.origin_metro_code}→${a.target_metro_code}`.localeCompare(
            `${b.origin_metro_code}→${b.target_metro_code}`
          )
          break
        case 'dz':
          cmp = a.dz_avg_rtt_ms - b.dz_avg_rtt_ms
          break
        case 'internet':
          cmp = a.internet_avg_rtt_ms - b.internet_avg_rtt_ms
          break
        case 'improvement':
          cmp = (a.rtt_improvement_pct ?? 0) - (b.rtt_improvement_pct ?? 0)
          break
        case 'dzJitter':
          cmp = (a.dz_avg_jitter_ms ?? 0) - (b.dz_avg_jitter_ms ?? 0)
          break
        case 'internetJitter':
          cmp = (a.internet_avg_jitter_ms ?? 0) - (b.internet_avg_jitter_ms ?? 0)
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
  }, [latencyData, sortField, sortDirection, metroFilters])

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortField(field)
      setSortDirection(field === 'improvement' ? 'desc' : 'asc')
    }
  }

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return null
    return sortDirection === 'asc' ? (
      <ChevronUp className="h-3 w-3" />
    ) : (
      <ChevronDown className="h-3 w-3" />
    )
  }

  // Export to CSV
  const handleExport = () => {
    if (!comparisons.length) return

    const headers = ['From Metro', 'To Metro', 'DZ Avg RTT (ms)', 'DZ P95 RTT (ms)', 'DZ Jitter (ms)', 'Internet Avg RTT (ms)', 'Internet P95 RTT (ms)', 'Internet Jitter (ms)', 'RTT Improvement (%)', 'Jitter Improvement (%)']
    const rows = comparisons.map(comp => [
      comp.origin_metro_code,
      comp.target_metro_code,
      comp.dz_avg_rtt_ms.toFixed(1),
      comp.dz_p95_rtt_ms.toFixed(1),
      comp.dz_avg_jitter_ms?.toFixed(2) ?? '-',
      comp.internet_avg_rtt_ms.toFixed(1),
      comp.internet_p95_rtt_ms.toFixed(1),
      comp.internet_avg_jitter_ms?.toFixed(2) ?? '-',
      comp.rtt_improvement_pct?.toFixed(1) ?? '-',
      comp.jitter_improvement_pct?.toFixed(1) ?? '-',
    ])

    const csv = [headers.join(','), ...rows.map(row => row.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'dz-vs-internet-latency.csv'
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

  if (latencyError) {
    const errorMessage = latencyError instanceof Error ? latencyError.message : 'Unknown error'
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <ErrorState
          title="Failed to load latency data"
          message={errorMessage}
          onRetry={() => queryClient.invalidateQueries({ queryKey: ['latency-comparison'] })}
          retrying={latencyFetching}
        />
      </div>
    )
  }

  if (!latencyData || comparisons.length === 0) {
    // Don't show "no data" message while still loading (before delay threshold)
    if (latencyLoading) {
      return <div className="flex-1 bg-background" />
    }
    return (
      <div className="flex-1 flex items-center justify-center bg-background">
        <div className="text-muted-foreground">No latency comparison data available</div>
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col bg-background overflow-auto">
      {/* Header */}
      <div className="px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Zap className="h-5 w-5 text-muted-foreground" />
            <h1 className="text-lg font-semibold">DZ vs Internet</h1>
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
          Compares measured latency on direct DZ links against public internet latency for the same metro pairs.
        </p>

        {/* Summary stats */}
        <div className="flex gap-6 mt-4 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Link Pairs:</span>
            <span className="font-medium">{comparisons.length}</span>
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

        {/* Filter bar */}
        <div className="flex items-center gap-2 flex-wrap mt-4">
          {/* Search button */}
          <button
            onClick={openSearch}
            className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground hover:text-foreground border border-border rounded-md bg-background hover:bg-muted transition-colors"
            title="Filter by metro (Cmd+K)"
          >
            <Search className="h-3 w-3" />
            <span>Filter</span>
            <kbd className="ml-0.5 font-mono text-[10px] text-muted-foreground/70">⌘K</kbd>
          </button>

          {/* Filter tags */}
          {metroFilters.map((metro) => (
            <button
              key={metro}
              onClick={() => removeMetroFilter(metro)}
              className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 hover:bg-blue-500/20 transition-colors"
            >
              <MapPin className="h-3 w-3" />
              {metro}
              <X className="h-3 w-3" />
            </button>
          ))}

          {/* Clear all */}
          {metroFilters.length > 1 && (
            <button
              onClick={clearAllFilters}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              Clear all
            </button>
          )}
        </div>
      </div>

      {/* Table */}
      <div className="px-6">
        <div className="flex border-x border-border">
          <table className="w-full">
            <thead className="sticky top-0 bg-muted z-10">
              <tr>
                <th
                  className="px-4 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground"
                  onClick={() => handleSort('route')}
                >
                  <div className="flex items-center gap-1">
                    Route
                    <SortIcon field="route" />
                  </div>
                </th>
                {/* RTT group */}
                <th
                  className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground border-l border-border"
                  onClick={() => handleSort('dz')}
                >
                  <div className="flex items-center justify-end gap-1">
                    DZ RTT
                    <SortIcon field="dz" />
                  </div>
                </th>
                <th
                  className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground"
                  onClick={() => handleSort('internet')}
                >
                  <div className="flex items-center justify-end gap-1">
                    Internet RTT
                    <SortIcon field="internet" />
                  </div>
                </th>
                <th
                  className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground"
                  onClick={() => handleSort('improvement')}
                >
                  <div className="flex items-center justify-end gap-1">
                    RTT Improvement
                    <SortIcon field="improvement" />
                  </div>
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  RTT Saved
                </th>
                {/* Jitter group */}
                <th
                  className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground border-l border-border"
                  onClick={() => handleSort('dzJitter')}
                >
                  <div className="flex items-center justify-end gap-1">
                    DZ Jitter
                    <SortIcon field="dzJitter" />
                  </div>
                </th>
                <th
                  className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider cursor-pointer hover:text-foreground"
                  onClick={() => handleSort('internetJitter')}
                >
                  <div className="flex items-center justify-end gap-1">
                    Internet Jitter
                    <SortIcon field="internetJitter" />
                  </div>
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Jitter Improvement
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-muted-foreground uppercase tracking-wider">
                  Jitter Saved
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {comparisons.map((comp) => {
                const rttImprovement = comp.rtt_improvement_pct ?? 0
                const rttSaved = comp.internet_avg_rtt_ms - comp.dz_avg_rtt_ms
                const jitterImprovement = comp.jitter_improvement_pct ?? 0
                const hasJitter = comp.dz_avg_jitter_ms != null && comp.internet_avg_jitter_ms != null
                const jitterSaved = hasJitter
                  ? (comp.internet_avg_jitter_ms! - comp.dz_avg_jitter_ms!)
                  : null
                const isSelected = selectedComparison?.origin_metro_pk === comp.origin_metro_pk &&
                  selectedComparison?.target_metro_pk === comp.target_metro_pk

                return (
                  <tr
                    key={`${comp.origin_metro_pk}-${comp.target_metro_pk}`}
                    className={cn(
                      'hover:bg-muted/50 cursor-pointer transition-colors',
                      isSelected && 'bg-muted/50'
                    )}
                    onClick={() => setSelectedComparison(isSelected ? null : comp)}
                  >
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2 font-medium">
                        <span>{comp.origin_metro_code}</span>
                        <ArrowRight className="h-3 w-3 text-muted-foreground" />
                        <span>{comp.target_metro_code}</span>
                      </div>
                    </td>
                    {/* RTT group */}
                    <td className="px-4 py-3 text-right tabular-nums border-l border-border">
                      {comp.dz_avg_rtt_ms > 0 ? `${comp.dz_avg_rtt_ms.toFixed(1)}ms` : '-'}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-muted-foreground">
                      {comp.internet_avg_rtt_ms.toFixed(1)}ms
                    </td>
                    <td className="px-4 py-3 text-right">
                      {comp.dz_avg_rtt_ms > 0 ? (
                        <span className={cn(
                          'inline-flex items-center px-2 py-0.5 rounded text-sm font-medium tabular-nums',
                          getImprovementBg(rttImprovement),
                          getImprovementColor(rttImprovement)
                        )}>
                          {rttImprovement > 0 ? '+' : ''}{rttImprovement.toFixed(1)}%
                        </span>
                      ) : (
                        <span className="text-muted-foreground">-</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-muted-foreground">
                      {comp.dz_avg_rtt_ms > 0
                        ? `${rttSaved > 0 ? '-' : '+'}${Math.abs(rttSaved).toFixed(1)}ms`
                        : '-'}
                    </td>
                    {/* Jitter group */}
                    <td className="px-4 py-3 text-right tabular-nums border-l border-border">
                      {comp.dz_avg_jitter_ms != null ? `${comp.dz_avg_jitter_ms.toFixed(2)}ms` : '-'}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-muted-foreground">
                      {comp.internet_avg_jitter_ms != null ? `${comp.internet_avg_jitter_ms.toFixed(2)}ms` : '-'}
                    </td>
                    <td className="px-4 py-3 text-right">
                      {hasJitter ? (
                        <span className={cn(
                          'inline-flex items-center px-2 py-0.5 rounded text-sm font-medium tabular-nums',
                          getImprovementBg(jitterImprovement),
                          getImprovementColor(jitterImprovement)
                        )}>
                          {jitterImprovement > 0 ? '+' : ''}{jitterImprovement.toFixed(1)}%
                        </span>
                      ) : (
                        <span className="text-muted-foreground">-</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-muted-foreground">
                      {jitterSaved != null
                        ? `${jitterSaved > 0 ? '-' : '+'}${Math.abs(jitterSaved).toFixed(2)}ms`
                        : '-'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>

          {/* Detail panel */}
          {selectedComparison && (
            <div className="w-96 flex-shrink-0 border-l border-border p-4 bg-card overflow-y-auto">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-medium flex items-center gap-2">
                  <span>{selectedComparison.origin_metro_code}</span>
                  <ArrowRight className="h-4 w-4 text-muted-foreground" />
                  <span>{selectedComparison.target_metro_code}</span>
                </h3>
                <button
                  onClick={() => setSelectedComparison(null)}
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
                    <div className="text-lg font-bold">
                      {selectedComparison.dz_avg_rtt_ms > 0 ? `${selectedComparison.dz_avg_rtt_ms.toFixed(1)}ms` : '-'}
                    </div>
                  </div>
                  <div className="rounded-lg p-2 bg-muted">
                    <div className="text-[10px] text-muted-foreground mb-0.5">P95 RTT</div>
                    <div className="text-lg font-bold">
                      {selectedComparison.dz_p95_rtt_ms > 0 ? `${selectedComparison.dz_p95_rtt_ms.toFixed(1)}ms` : '-'}
                    </div>
                  </div>
                  <div className="rounded-lg p-2 bg-muted">
                    <div className="text-[10px] text-muted-foreground mb-0.5">Jitter</div>
                    <div className="text-lg font-bold">
                      {selectedComparison.dz_avg_jitter_ms != null
                        ? `${selectedComparison.dz_avg_jitter_ms.toFixed(2)}ms`
                        : '-'}
                    </div>
                  </div>
                </div>
              </div>

              {/* Internet Latency */}
              <div className="mb-4">
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Public Internet</div>
                <div className="grid grid-cols-3 gap-2">
                  <div className="rounded-lg p-2 bg-muted">
                    <div className="text-[10px] text-muted-foreground mb-0.5">Avg RTT</div>
                    <div className="text-lg font-bold">{selectedComparison.internet_avg_rtt_ms.toFixed(1)}ms</div>
                  </div>
                  <div className="rounded-lg p-2 bg-muted">
                    <div className="text-[10px] text-muted-foreground mb-0.5">P95 RTT</div>
                    <div className="text-lg font-bold">{selectedComparison.internet_p95_rtt_ms.toFixed(1)}ms</div>
                  </div>
                  <div className="rounded-lg p-2 bg-muted">
                    <div className="text-[10px] text-muted-foreground mb-0.5">Jitter</div>
                    <div className="text-lg font-bold">
                      {selectedComparison.internet_avg_jitter_ms != null
                        ? `${selectedComparison.internet_avg_jitter_ms.toFixed(2)}ms`
                        : '-'}
                    </div>
                  </div>
                </div>
              </div>

              {/* Improvement */}
              <div className={cn(
                'rounded-lg p-3 mb-4',
                getImprovementBg(selectedComparison.rtt_improvement_pct ?? 0)
              )}>
                <div className="text-xs text-muted-foreground mb-1">DZ Advantage</div>
                <div className={cn(
                  'text-xl font-bold',
                  getImprovementColor(selectedComparison.rtt_improvement_pct ?? 0)
                )}>
                  {(selectedComparison.rtt_improvement_pct ?? 0) > 0 ? '+' : ''}
                  {selectedComparison.rtt_improvement_pct?.toFixed(1)}%
                </div>
                <div className="text-xs text-muted-foreground">
                  {(selectedComparison.internet_avg_rtt_ms - selectedComparison.dz_avg_rtt_ms).toFixed(1)}ms saved
                </div>
              </div>

              {/* RTT Time Series Chart */}
              <div className="mb-4">
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
                  RTT Over Time (24h)
                </div>
                {historyLoading ? (
                  <div className="h-32 flex items-center justify-center">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                ) : chartData.length > 0 ? (
                  <>
                    <div className="h-32">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={chartData}>
                          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
                          <XAxis
                            dataKey="time"
                            tick={{ fontSize: 9 }}
                            tickLine={false}
                            axisLine={false}
                            interval="preserveStartEnd"
                          />
                          <YAxis
                            tick={{ fontSize: 9 }}
                            tickLine={false}
                            axisLine={false}
                            tickFormatter={(v) => `${v}ms`}
                            width={45}
                          />
                          <RechartsTooltip
                            contentStyle={{
                              backgroundColor: 'var(--card)',
                              border: '1px solid var(--border)',
                              borderRadius: '6px',
                              fontSize: '11px',
                            }}
                            formatter={(value) => (value as number | null) != null ? `${(value as number).toFixed(1)}ms` : '-'}
                          />
                          <Line
                            type="monotone"
                            dataKey="dzRtt"
                            stroke={dzColor}
                            strokeWidth={1.5}
                            dot={false}
                            name="DZ"
                            connectNulls
                          />
                          <Line
                            type="monotone"
                            dataKey="inetRtt"
                            stroke={inetColor}
                            strokeWidth={1.5}
                            dot={false}
                            name="Internet"
                            connectNulls
                          />
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                    <div className="flex justify-center gap-4 text-xs mt-1">
                      <span className="flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: dzColor }} />
                        DZ
                      </span>
                      <span className="flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: inetColor }} />
                        Internet
                      </span>
                    </div>
                  </>
                ) : (
                  <div className="h-32 flex items-center justify-center text-xs text-muted-foreground">
                    No history data available
                  </div>
                )}
              </div>

              {/* Jitter Time Series Chart */}
              <div className="mb-4">
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
                  Jitter Over Time (24h)
                </div>
                {historyLoading ? (
                  <div className="h-32 flex items-center justify-center">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                ) : chartData.length > 0 ? (
                  <>
                    <div className="h-32">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={chartData}>
                          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
                          <XAxis
                            dataKey="time"
                            tick={{ fontSize: 9 }}
                            tickLine={false}
                            axisLine={false}
                            interval="preserveStartEnd"
                          />
                          <YAxis
                            tick={{ fontSize: 9 }}
                            tickLine={false}
                            axisLine={false}
                            tickFormatter={(v) => `${v}ms`}
                            width={45}
                          />
                          <RechartsTooltip
                            contentStyle={{
                              backgroundColor: 'var(--card)',
                              border: '1px solid var(--border)',
                              borderRadius: '6px',
                              fontSize: '11px',
                            }}
                            formatter={(value) => (value as number | null) != null ? `${(value as number).toFixed(2)}ms` : '-'}
                          />
                          <Line
                            type="monotone"
                            dataKey="dzJitter"
                            stroke={dzColor}
                            strokeWidth={1.5}
                            dot={false}
                            name="DZ"
                            connectNulls
                          />
                          <Line
                            type="monotone"
                            dataKey="inetJitter"
                            stroke={inetColor}
                            strokeWidth={1.5}
                            dot={false}
                            name="Internet"
                            connectNulls
                          />
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                    <div className="flex justify-center gap-4 text-xs mt-1">
                      <span className="flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: dzColor }} />
                        DZ
                      </span>
                      <span className="flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: inetColor }} />
                        Internet
                      </span>
                    </div>
                  </>
                ) : (
                  <div className="h-32 flex items-center justify-center text-xs text-muted-foreground">
                    No history data available
                  </div>
                )}
              </div>

              <div className="text-xs text-muted-foreground">
                DZ samples: {selectedComparison.dz_sample_count.toLocaleString()} •
                Internet samples: {selectedComparison.internet_sample_count.toLocaleString()}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Legend */}
      <div className="px-6 py-4 flex items-center gap-6 text-xs text-muted-foreground">
        <span className="font-medium">Legend:</span>
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
      </div>
    </div>
  )
}
