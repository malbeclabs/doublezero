import { useQuery } from '@tanstack/react-query'
import { useState, useMemo } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { AlertTriangle, Download, ExternalLink } from 'lucide-react'
import { fetchLinkOutages, type LinkOutage, type OutageTimeRange, type OutageThreshold } from '@/lib/api'
import { StatCard } from '@/components/stat-card'
import { StatusFilters, useStatusFilters } from '@/components/status-search-bar'

const timeRanges: { value: OutageTimeRange; label: string }[] = [
  { value: '3h', label: '3h' },
  { value: '6h', label: '6h' },
  { value: '12h', label: '12h' },
  { value: '24h', label: '24h' },
  { value: '3d', label: '3d' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

const thresholds: { value: OutageThreshold; label: string }[] = [
  { value: 1, label: '1%' },
  { value: 10, label: '10%' },
]

const outageTypes: { value: 'all' | 'status' | 'loss' | 'no_data'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'status', label: 'Status' },
  { value: 'loss', label: 'Packet Loss' },
  { value: 'no_data', label: 'No Data' },
]

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

function SummaryCard({ label, value, highlight }: { label: string; value: number; highlight?: boolean }) {
  return (
    <div className={`text-center p-4 rounded-lg border ${highlight ? 'border-red-500/50 bg-red-500/5' : 'border-border'}`}>
      <div className="text-4xl font-medium tabular-nums tracking-tight mb-1">
        {value.toLocaleString()}
      </div>
      <div className="text-sm text-muted-foreground">{label}</div>
    </div>
  )
}

function OutagesPageSkeleton() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        <div className="mb-6">
          <Skeleton className="h-8 w-48" />
        </div>
        <div className="mb-6">
          <Skeleton className="h-10 w-full max-w-lg" />
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-8">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-20" />
          ))}
        </div>
        <Skeleton className="h-[400px] rounded-lg" />
      </div>
    </div>
  )
}

function formatDuration(seconds: number | undefined): string {
  if (seconds === undefined) return '-'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600)
    const mins = Math.floor((seconds % 3600) / 60)
    return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`
  }
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return hours > 0 ? `${days}d ${hours}h` : `${days}d`
}

function formatTimeAgo(isoString: string): string {
  const date = new Date(isoString)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSecs = Math.floor(diffMs / 1000)

  if (diffSecs < 60) return `${diffSecs}s ago`
  if (diffSecs < 3600) return `${Math.floor(diffSecs / 60)}m ago`
  if (diffSecs < 86400) return `${Math.floor(diffSecs / 3600)}h ago`
  return `${Math.floor(diffSecs / 86400)}d ago`
}

function formatTimestamp(isoString: string): string {
  return new Date(isoString).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function OutageDetails({ outage }: { outage: LinkOutage }) {
  if (outage.outage_type === 'status') {
    return (
      <span className="text-muted-foreground">
        {outage.previous_status} &rarr; {outage.new_status}
      </span>
    )
  }
  if (outage.outage_type === 'no_data') {
    return (
      <span className="text-muted-foreground">
        telemetry stopped
      </span>
    )
  }
  return (
    <span className="text-muted-foreground">
      peak {outage.peak_loss_pct?.toFixed(1)}%
    </span>
  )
}

function OutageTypeLabel({ type }: { type: 'status' | 'packet_loss' | 'no_data' }) {
  if (type === 'status') {
    return (
      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-200">
        Status
      </span>
    )
  }
  if (type === 'no_data') {
    return (
      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200">
        No Data
      </span>
    )
  }
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
      Packet Loss
    </span>
  )
}

function OngoingIndicator() {
  return (
    <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
      <span className="relative flex h-2 w-2">
        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-400 opacity-75"></span>
        <span className="relative inline-flex rounded-full h-2 w-2 bg-red-500"></span>
      </span>
      ongoing
    </span>
  )
}

export function OutagesPage() {
  const [searchParams, setSearchParams] = useSearchParams()

  // Parse URL params with defaults
  const range = (searchParams.get('range') as OutageTimeRange) || '24h'
  const threshold = (parseInt(searchParams.get('threshold') || '1') as OutageThreshold) || 1
  const type = (searchParams.get('type') as 'all' | 'status' | 'loss' | 'no_data') || 'all'
  const filterParam = searchParams.get('filter') || ''

  // Get filters from the shared status filter component
  const filters = useStatusFilters()

  // Update URL params
  const updateParams = (updates: Record<string, string | undefined>) => {
    const newParams = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(updates)) {
      if (value && value !== getDefaultValue(key)) {
        newParams.set(key, value)
      } else {
        newParams.delete(key)
      }
    }
    setSearchParams(newParams)
  }

  const getDefaultValue = (key: string): string => {
    switch (key) {
      case 'range': return '24h'
      case 'threshold': return '1'
      case 'type': return 'all'
      default: return ''
    }
  }

  const { data, isLoading, error } = useQuery({
    queryKey: ['linkOutages', range, threshold, type, filterParam],
    queryFn: () => fetchLinkOutages({ range, threshold, type, filter: filterParam || undefined }),
    refetchInterval: 60000, // Refresh every minute
  })

  const outages = data?.outages || []
  const summary = data?.summary || { total: 0, ongoing: 0, by_type: { status: 0, packet_loss: 0 } }

  // Sort state
  const [sortField, setSortField] = useState<'started_at' | 'duration'>('started_at')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [pinOngoing, setPinOngoing] = useState(true)

  const sortedOutages = useMemo(() => {
    const compareOutages = (a: LinkOutage, b: LinkOutage) => {
      if (sortField === 'started_at') {
        const aTime = new Date(a.started_at).getTime()
        const bTime = new Date(b.started_at).getTime()
        return sortDir === 'asc' ? aTime - bTime : bTime - aTime
      } else {
        const aDur = a.is_ongoing ? Infinity : (a.duration_seconds || 0)
        const bDur = b.is_ongoing ? Infinity : (b.duration_seconds || 0)
        return sortDir === 'asc' ? aDur - bDur : bDur - aDur
      }
    }

    if (!pinOngoing) {
      return [...outages].sort(compareOutages)
    }

    const ongoing = outages.filter(outage => outage.is_ongoing).sort(compareOutages)
    const notOngoing = outages.filter(outage => !outage.is_ongoing).sort(compareOutages)
    return [...ongoing, ...notOngoing]
  }, [outages, sortField, sortDir, pinOngoing])

  const toggleSort = (field: 'started_at' | 'duration') => {
    if (sortField === field) {
      setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
    } else {
      setSortField(field)
      setSortDir('desc')
    }
  }

  const exportUrl = useMemo(() => {
    const params = new URLSearchParams()
    params.set('range', range)
    params.set('threshold', threshold.toString())
    if (type !== 'all') params.set('type', type)
    if (filterParam) params.set('filter', filterParam)
    return `/api/outages/links/csv?${params.toString()}`
  }, [range, threshold, type, filterParam])

  if (isLoading) {
    return <OutagesPageSkeleton />
  }

  if (error) {
    return (
      <div className="flex-1 overflow-auto">
        <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
          <div className="text-red-500">Error loading outages: {(error as Error).message}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <h1 className="text-2xl font-semibold">Link Outages</h1>
          <a
            href={exportUrl}
            className="inline-flex items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-muted transition-colors"
          >
            <Download className="h-4 w-4" />
            Export CSV
          </a>
        </div>

        {/* Filters */}
        <div className="flex flex-wrap items-center gap-4 mb-6">
          {/* Time range */}
          <div className="flex items-center gap-1 bg-muted rounded-md p-1">
            {timeRanges.map((tr) => (
              <button
                key={tr.value}
                onClick={() => updateParams({ range: tr.value })}
                className={`px-3 py-1 text-sm rounded transition-colors ${
                  range === tr.value
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {tr.label}
              </button>
            ))}
          </div>

          {/* Threshold */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Threshold:</span>
            <div className="flex items-center gap-1 bg-muted rounded-md p-1">
              {thresholds.map((th) => (
                <button
                  key={th.value}
                  onClick={() => updateParams({ threshold: th.value.toString() })}
                  className={`px-3 py-1 text-sm rounded transition-colors ${
                    threshold === th.value
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {th.label}
                </button>
              ))}
            </div>
          </div>

          {/* Type filter */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Type:</span>
            <div className="flex items-center gap-1 bg-muted rounded-md p-1">
              {outageTypes.map((ot) => (
                <button
                  key={ot.value}
                  onClick={() => updateParams({ type: ot.value })}
                  className={`px-3 py-1 text-sm rounded transition-colors ${
                    type === ot.value
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {ot.label}
                </button>
              ))}
            </div>
          </div>

          {/* Search/filter button and tags */}
          <StatusFilters />
        </div>

        {/* Summary cards */}
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-4 mb-8">
          <StatCard label="Total Outages" value={summary.total} format="number" />
          <SummaryCard
            label="Ongoing"
            value={summary.ongoing}
            highlight={summary.ongoing > 0}
          />
          <StatCard label="Status Changes" value={summary.by_type.status} format="number" />
          <StatCard label="Packet Loss" value={summary.by_type.packet_loss} format="number" />
          <StatCard label="No Data" value={summary.by_type.no_data || 0} format="number" />
        </div>

        {/* Outages table */}
        {outages.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-center border border-border rounded-lg">
            <AlertTriangle className="h-12 w-12 text-muted-foreground mb-4" />
            <h3 className="text-lg font-medium mb-2">No outages found</h3>
            <p className="text-sm text-muted-foreground">
              {filters.length > 0 ? 'No outages match the selected filters ' : 'No outages '}
              in the selected time range.
            </p>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-between mb-3">
              <button
                type="button"
                role="switch"
                aria-checked={pinOngoing}
                onClick={() => setPinOngoing(!pinOngoing)}
                className="flex items-center gap-2 text-sm text-muted-foreground"
              >
                <span
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    pinOngoing ? 'bg-primary' : 'bg-muted-foreground/30'
                  }`}
                >
                  <span
                    className={`inline-block h-4 w-4 transform rounded-full bg-background shadow transition-transform ${
                      pinOngoing ? 'translate-x-4' : 'translate-x-0.5'
                    }`}
                  />
                </span>
                Pin ongoing outages to top
              </button>
            </div>
            <div className="border border-border rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-muted/50">
                <tr>
                  <th className="text-left px-4 py-3 font-medium">Link</th>
                  <th className="text-left px-4 py-3 font-medium">Route</th>
                  <th className="text-left px-4 py-3 font-medium">Type</th>
                  <th className="text-left px-4 py-3 font-medium">Details</th>
                  <th
                    className="text-left px-4 py-3 font-medium cursor-pointer hover:text-foreground"
                    onClick={() => toggleSort('started_at')}
                  >
                    Started{' '}
                    {sortField === 'started_at' && (
                      <span className="text-xs">{sortDir === 'asc' ? '↑' : '↓'}</span>
                    )}
                  </th>
                  <th
                    className="text-left px-4 py-3 font-medium cursor-pointer hover:text-foreground"
                    onClick={() => toggleSort('duration')}
                  >
                    Duration{' '}
                    {sortField === 'duration' && (
                      <span className="text-xs">{sortDir === 'asc' ? '↑' : '↓'}</span>
                    )}
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {sortedOutages.map((outage) => (
                  <tr key={outage.id} className="hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <Link
                        to={`/dz/links/${encodeURIComponent(outage.link_pk)}`}
                        className="text-primary hover:underline inline-flex items-center gap-1"
                      >
                        {outage.link_code}
                        <ExternalLink className="h-3 w-3" />
                      </Link>
                      <div className="text-xs text-muted-foreground">{outage.contributor_code} · {outage.link_type}</div>
                    </td>
                    <td className="px-4 py-3">
                      <span className="font-mono">
                        {outage.side_a_metro} &rarr; {outage.side_z_metro}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <OutageTypeLabel type={outage.outage_type} />
                    </td>
                    <td className="px-4 py-3">
                      <OutageDetails outage={outage} />
                    </td>
                    <td className="px-4 py-3">
                      <div>{formatTimeAgo(outage.started_at)}</div>
                      <div className="text-xs text-muted-foreground">
                        {formatTimestamp(outage.started_at)}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {outage.is_ongoing ? (
                        <OngoingIndicator />
                      ) : (
                        formatDuration(outage.duration_seconds)
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
