import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import { ChevronDown, TrendingUp, TrendingDown, Minus, UserPlus, UserMinus, ExternalLink } from 'lucide-react'
import {
  fetchStakeOverview,
  fetchStakeHistory,
  fetchStakeChanges,
  fetchStakeValidators,
  type StakeHistoryResponse,
  type StakeChangesResponse,
  type StakeValidatorsResponse,
} from '@/lib/api'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'

type TimeRange = '24h' | '7d' | '30d'

const timeRangeLabels: Record<TimeRange, string> = {
  '24h': 'Last 24 Hours',
  '7d': 'Last 7 Days',
  '30d': 'Last 30 Days',
}

const timeRangeIntervals: Record<TimeRange, '5m' | '15m' | '1h' | '6h' | '1d'> = {
  '24h': '15m',
  '7d': '1h',
  '30d': '6h',
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

function StakePageSkeleton() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-5xl mx-auto px-4 sm:px-8 py-8">
        <Skeleton className="h-8 w-48 mb-8" />
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-6 mb-8">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </div>
        <Skeleton className="h-[400px] rounded-lg mb-8" />
        <Skeleton className="h-[300px] rounded-lg" />
      </div>
    </div>
  )
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
          <div className="absolute right-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[140px]">
            {(Object.keys(timeRangeLabels) as TimeRange[]).map((range) => (
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

function formatStake(sol: number): string {
  if (sol >= 1_000_000) {
    return `${(sol / 1_000_000).toFixed(2)}M SOL`
  }
  if (sol >= 1_000) {
    return `${(sol / 1_000).toFixed(1)}K SOL`
  }
  return `${sol.toFixed(0)} SOL`
}

function formatStakeCompact(sol: number): string {
  if (sol >= 1_000_000) {
    return `${(sol / 1_000_000).toFixed(2)}M`
  }
  if (sol >= 1_000) {
    return `${(sol / 1_000).toFixed(1)}K`
  }
  return sol.toFixed(0)
}

function formatPercent(pct: number): string {
  return `${pct.toFixed(2)}%`
}

function formatDelta(delta: number, unit: 'sol' | 'pct'): string {
  const sign = delta >= 0 ? '+' : ''
  if (unit === 'sol') {
    if (Math.abs(delta) >= 1_000_000) {
      return `${sign}${(delta / 1_000_000).toFixed(2)}M`
    }
    if (Math.abs(delta) >= 1_000) {
      return `${sign}${(delta / 1_000).toFixed(1)}K`
    }
    return `${sign}${delta.toFixed(0)}`
  }
  return `${sign}${delta.toFixed(2)}%`
}

function DeltaIndicator({ delta }: { delta: number }) {
  if (Math.abs(delta) < 0.001) {
    return <Minus className="h-4 w-4 text-muted-foreground" />
  }
  if (delta > 0) {
    return <TrendingUp className="h-4 w-4 text-green-500" />
  }
  return <TrendingDown className="h-4 w-4 text-red-500" />
}

function MetricCard({
  label,
  value,
  delta,
  deltaLabel,
  deltaUnit,
}: {
  label: string
  value: string
  delta?: number
  deltaLabel?: string
  deltaUnit?: 'sol' | 'pct'
}) {
  return (
    <div className="bg-card border border-border rounded-lg p-4">
      <div className="text-sm text-muted-foreground mb-1">{label}</div>
      <div className="text-2xl font-semibold tabular-nums">{value}</div>
      {delta !== undefined && deltaLabel && deltaUnit && (
        <div className="flex items-center gap-1 mt-2 text-sm">
          <DeltaIndicator delta={delta} />
          <span className={delta > 0 ? 'text-green-500' : delta < 0 ? 'text-red-500' : 'text-muted-foreground'}>
            {formatDelta(delta, deltaUnit)}
          </span>
          <span className="text-muted-foreground">{deltaLabel}</span>
        </div>
      )}
    </div>
  )
}

function StakeChart({
  data,
  timeRange,
}: {
  data: StakeHistoryResponse | undefined
  timeRange: TimeRange
}) {
  if (!data || data.points.length === 0) {
    return (
      <div className="h-[300px] flex items-center justify-center text-muted-foreground">
        No history data available
      </div>
    )
  }

  const chartData = data.points.map((point) => ({
    timestamp: new Date(point.timestamp).getTime(),
    stakeShare: point.stake_share_pct,
    dzStake: point.dz_stake_sol,
  }))

  const formatXAxis = (timestamp: number) => {
    const date = new Date(timestamp)
    if (timeRange === '24h') {
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }
    if (timeRange === '7d') {
      return date.toLocaleDateString([], { weekday: 'short', hour: '2-digit' })
    }
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }

  const formatTooltipLabel = (timestamp: number) => {
    const date = new Date(timestamp)
    return date.toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  return (
    <ResponsiveContainer width="100%" height={300}>
      <LineChart data={chartData} margin={{ top: 20, right: 30, left: 20, bottom: 20 }}>
        <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
        <XAxis
          dataKey="timestamp"
          tickFormatter={formatXAxis}
          className="text-xs"
          stroke="currentColor"
          tick={{ fill: 'currentColor' }}
          tickLine={{ stroke: 'currentColor' }}
          minTickGap={50}
        />
        <YAxis
          domain={['auto', 'auto']}
          tickFormatter={(v) => `${v.toFixed(1)}%`}
          className="text-xs"
          stroke="currentColor"
          tick={{ fill: 'currentColor' }}
          tickLine={{ stroke: 'currentColor' }}
          width={60}
        />
        <Tooltip
          labelFormatter={formatTooltipLabel}
          formatter={(value, name) => {
            if (typeof value !== 'number') return ['-', name]
            if (name === 'stakeShare') {
              return [`${value.toFixed(2)}%`, 'Stake Share']
            }
            return [formatStake(value), 'DZ Stake']
          }}
          contentStyle={{
            backgroundColor: 'hsl(var(--popover))',
            border: '1px solid hsl(var(--border))',
            borderRadius: '6px',
          }}
          labelStyle={{ color: 'hsl(var(--foreground))' }}
        />
        <Line
          type="monotone"
          dataKey="stakeShare"
          stroke="hsl(var(--primary))"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4 }}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}

function StakeChangesSection({
  data,
  timeRange,
}: {
  data: StakeChangesResponse | undefined
  timeRange: TimeRange
}) {
  if (!data) {
    return null
  }

  const { summary, changes } = data
  const hasChanges = changes.length > 0

  return (
    <div className="bg-card border border-border rounded-lg p-4">
      <h2 className="text-lg font-medium mb-4">Stake Changes ({timeRangeLabels[timeRange]})</h2>

      {/* Summary */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
        <div className="text-center p-3 bg-muted/50 rounded-lg">
          <div className="flex items-center justify-center gap-1 text-green-500 mb-1">
            <UserPlus className="h-4 w-4" />
            <span className="font-semibold">{summary.joined_count}</span>
          </div>
          <div className="text-xs text-muted-foreground">Joined</div>
          <div className="text-sm font-medium text-green-500">+{formatStakeCompact(summary.joined_stake_sol)} SOL</div>
        </div>
        <div className="text-center p-3 bg-muted/50 rounded-lg">
          <div className="flex items-center justify-center gap-1 text-red-500 mb-1">
            <UserMinus className="h-4 w-4" />
            <span className="font-semibold">{summary.left_count}</span>
          </div>
          <div className="text-xs text-muted-foreground">Left</div>
          <div className="text-sm font-medium text-red-500">-{formatStakeCompact(summary.left_stake_sol)} SOL</div>
        </div>
        <div className="col-span-2 text-center p-3 bg-muted/50 rounded-lg">
          <div className="text-xs text-muted-foreground mb-1">Net Change</div>
          <div className={`text-xl font-semibold ${summary.net_change_sol >= 0 ? 'text-green-500' : 'text-red-500'}`}>
            {formatDelta(summary.net_change_sol, 'sol')} SOL
          </div>
        </div>
      </div>

      {/* Changes list */}
      {hasChanges ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left py-2 px-2 font-medium text-muted-foreground">Validator</th>
                <th className="text-left py-2 px-2 font-medium text-muted-foreground">Location</th>
                <th className="text-right py-2 px-2 font-medium text-muted-foreground">Stake</th>
                <th className="text-center py-2 px-2 font-medium text-muted-foreground">Action</th>
              </tr>
            </thead>
            <tbody>
              {changes.slice(0, 20).map((change) => (
                <tr key={change.vote_pubkey} className="border-b border-border/50 hover:bg-muted/30">
                  <td className="py-2 px-2">
                    <Link
                      to={`/solana/validators/${change.vote_pubkey}`}
                      className="font-mono text-xs hover:text-primary transition-colors"
                    >
                      {change.vote_pubkey.slice(0, 8)}...{change.vote_pubkey.slice(-4)}
                    </Link>
                  </td>
                  <td className="py-2 px-2 text-muted-foreground">
                    {change.city && change.country
                      ? `${change.city}, ${change.country}`
                      : change.country || '—'}
                  </td>
                  <td className="py-2 px-2 text-right tabular-nums">
                    {formatStakeCompact(change.stake_sol)} SOL
                  </td>
                  <td className="py-2 px-2 text-center">
                    {change.category === 'joined' ? (
                      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-green-500/10 text-green-500">
                        <UserPlus className="h-3 w-3" />
                        Joined
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-red-500/10 text-red-500">
                        <UserMinus className="h-3 w-3" />
                        Left
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {changes.length > 20 && (
            <div className="text-center text-sm text-muted-foreground mt-2">
              Showing 20 of {changes.length} changes
            </div>
          )}
        </div>
      ) : (
        <div className="text-center text-muted-foreground py-4">
          No validator changes in this time period
        </div>
      )}
    </div>
  )
}

function ValidatorsSection({
  data,
}: {
  data: StakeValidatorsResponse | undefined
}) {
  if (!data || data.validators.length === 0) {
    return null
  }

  return (
    <div className="bg-card border border-border rounded-lg p-4">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-medium">Validators on DZ</h2>
        <Link
          to="/solana/validators"
          className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1"
        >
          View all
          <ExternalLink className="h-3 w-3" />
        </Link>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="text-left py-2 px-2 font-medium text-muted-foreground">Validator</th>
              <th className="text-left py-2 px-2 font-medium text-muted-foreground">Location</th>
              <th className="text-left py-2 px-2 font-medium text-muted-foreground">Metro</th>
              <th className="text-right py-2 px-2 font-medium text-muted-foreground">Stake</th>
              <th className="text-right py-2 px-2 font-medium text-muted-foreground">Share</th>
            </tr>
          </thead>
          <tbody>
            {data.validators.slice(0, 15).map((v) => (
              <tr key={v.vote_pubkey} className="border-b border-border/50 hover:bg-muted/30">
                <td className="py-2 px-2">
                  <Link
                    to={`/solana/validators/${v.vote_pubkey}`}
                    className="font-mono text-xs hover:text-primary transition-colors"
                  >
                    {v.vote_pubkey.slice(0, 8)}...{v.vote_pubkey.slice(-4)}
                  </Link>
                </td>
                <td className="py-2 px-2 text-muted-foreground">
                  {v.city && v.country ? `${v.city}, ${v.country}` : v.country || '—'}
                </td>
                <td className="py-2 px-2">
                  {v.metro_code ? (
                    <Link
                      to={`/dz/metros/${v.metro_code}`}
                      className="text-xs hover:text-primary transition-colors"
                    >
                      {v.metro_code}
                    </Link>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
                <td className="py-2 px-2 text-right tabular-nums">
                  {formatStakeCompact(v.stake_sol)}
                </td>
                <td className="py-2 px-2 text-right tabular-nums text-muted-foreground">
                  {v.stake_share_pct.toFixed(3)}%
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.validators.length > 15 && (
          <div className="text-center mt-2">
            <Link
              to="/solana/validators"
              className="text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              View all {data.on_dz_count} validators
            </Link>
          </div>
        )}
      </div>
    </div>
  )
}

export function StakePage() {
  const [timeRange, setTimeRange] = useState<TimeRange>('7d')

  const {
    data: overview,
    isLoading: isOverviewLoading,
    error: overviewError,
  } = useQuery({
    queryKey: ['stakeOverview'],
    queryFn: fetchStakeOverview,
    refetchInterval: 60000,
  })

  const {
    data: history,
    isLoading: isHistoryLoading,
    error: historyError,
  } = useQuery({
    queryKey: ['stakeHistory', timeRange],
    queryFn: () => fetchStakeHistory(timeRange, timeRangeIntervals[timeRange]),
    refetchInterval: 60000,
  })

  const {
    data: changes,
    isLoading: isChangesLoading,
  } = useQuery({
    queryKey: ['stakeChanges', timeRange],
    queryFn: () => fetchStakeChanges(timeRange),
    refetchInterval: 60000,
  })

  const {
    data: validators,
    isLoading: isValidatorsLoading,
  } = useQuery({
    queryKey: ['stakeValidators'],
    queryFn: () => fetchStakeValidators('on_dz', 50),
    refetchInterval: 60000,
  })

  const isLoading = isOverviewLoading || isHistoryLoading
  const showSkeleton = useDelayedLoading(isLoading)

  if (isLoading && showSkeleton) {
    return <StakePageSkeleton />
  }

  if (isLoading) {
    return null
  }

  const error = overviewError || historyError
  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <div className="text-destructive mb-2">Failed to load stake data</div>
          <div className="text-sm text-muted-foreground">
            {error instanceof Error ? error.message : 'Unknown error'}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-5xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <h1 className="text-2xl font-semibold">Stake Analytics</h1>
          <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
        </div>

        {/* Overview metrics */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
          <MetricCard
            label="DZ Stake Share"
            value={overview ? formatPercent(overview.stake_share_pct) : '—'}
            delta={overview?.share_change_24h}
            deltaLabel="24h"
            deltaUnit="pct"
          />
          <MetricCard
            label="DZ Stake"
            value={overview ? formatStake(overview.dz_stake_sol) : '—'}
            delta={overview?.dz_stake_change_24h}
            deltaLabel="24h"
            deltaUnit="sol"
          />
          <MetricCard
            label="Validators on DZ"
            value={overview ? overview.validator_count.toLocaleString() : '—'}
          />
          <MetricCard
            label="7d Share Change"
            value={overview ? formatDelta(overview.share_change_7d, 'pct') : '—'}
          />
        </div>

        {/* Chart */}
        <div className="bg-card border border-border rounded-lg p-4 mb-8">
          <h2 className="text-lg font-medium mb-4">Stake Share Over Time</h2>
          <StakeChart data={history} timeRange={timeRange} />
        </div>

        {/* Two column layout for changes and validators */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Stake changes / attribution */}
          {!isChangesLoading && <StakeChangesSection data={changes} timeRange={timeRange} />}

          {/* Validators */}
          {!isValidatorsLoading && <ValidatorsSection data={validators} />}
        </div>
      </div>
    </div>
  )
}
