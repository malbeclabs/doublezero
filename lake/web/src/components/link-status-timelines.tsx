import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, CheckCircle2, AlertTriangle, History, Info, ChevronDown, ChevronUp } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid, ReferenceLine } from 'recharts'
import { fetchLinkHistory } from '@/lib/api'
import type { LinkHistory, LinkHourStatus } from '@/lib/api'
import { StatusTimeline } from './status-timeline'
import { CriticalityBadge } from './criticality-badge'
import { useTheme } from '@/hooks/use-theme'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'

interface LinkStatusTimelinesProps {
  timeRange?: string
  onTimeRangeChange?: (range: TimeRange) => void
  issueFilters?: string[]
  healthFilters?: string[]
  linksWithIssues?: Map<string, string[]>  // Map of link code -> issue reasons (from filter time range)
  linksWithHealth?: Map<string, string>    // Map of link code -> health status (from filter time range)
  criticalityMap?: Map<string, 'critical' | 'important' | 'redundant'>  // Map of link code -> criticality level
}

function formatBandwidth(bps: number): string {
  if (bps >= 1_000_000_000) {
    return `${(bps / 1_000_000_000).toFixed(0)} Gbps`
  } else if (bps >= 1_000_000) {
    return `${(bps / 1_000_000).toFixed(0)} Mbps`
  } else if (bps >= 1_000) {
    return `${(bps / 1_000).toFixed(0)} Kbps`
  }
  return `${bps} bps`
}

function LinkInfoPopover({ link, criticality }: { link: LinkHistory; criticality?: 'critical' | 'important' | 'redundant' }) {
  const [isOpen, setIsOpen] = useState(false)

  const criticalityInfo = {
    critical: {
      label: 'Single Point of Failure',
      description: 'One endpoint has no other connections.',
      className: 'text-red-500',
    },
    important: {
      label: 'Limited Redundancy',
      description: 'Each endpoint has only 2 connections.',
      className: 'text-amber-500',
    },
    redundant: {
      label: 'Well Connected',
      description: 'Both endpoints have 3+ connections.',
      className: 'text-green-500',
    },
  }

  return (
    <div className="relative inline-block">
      <button
        className="text-muted-foreground hover:text-foreground transition-colors p-0.5 -m-0.5"
        onMouseEnter={() => setIsOpen(true)}
        onMouseLeave={() => setIsOpen(false)}
        onClick={() => setIsOpen(!isOpen)}
      >
        <Info className="h-3.5 w-3.5" />
      </button>
      {isOpen && (
        <div
          className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 min-w-[220px]"
          onMouseEnter={() => setIsOpen(true)}
          onMouseLeave={() => setIsOpen(false)}
        >
          <div className="space-y-2 text-xs">
            <div>
              <div className="text-muted-foreground">Route</div>
              <div className="font-medium">{link.side_a_metro} — {link.side_z_metro}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Devices</div>
              <div className="font-mono text-[11px]">
                <div>{link.side_a_device}</div>
                <div>{link.side_z_device}</div>
              </div>
            </div>
            <div className="flex gap-4">
              <div>
                <div className="text-muted-foreground">Type</div>
                <div className="font-medium">{link.link_type}</div>
              </div>
              {link.bandwidth_bps > 0 && (
                <div>
                  <div className="text-muted-foreground">Bandwidth</div>
                  <div className="font-medium">{formatBandwidth(link.bandwidth_bps)}</div>
                </div>
              )}
            </div>
            {link.committed_rtt_us > 0 && (
              <div>
                <div className="text-muted-foreground">Committed RTT</div>
                <div className="font-medium">{(link.committed_rtt_us / 1000).toFixed(2)} ms</div>
              </div>
            )}
            {criticality && (
              <div className="pt-2 mt-2 border-t border-border">
                <div className="text-muted-foreground">Redundancy</div>
                <div className={`font-medium ${criticalityInfo[criticality].className}`}>
                  {criticalityInfo[criticality].label}
                </div>
                <div className="text-muted-foreground mt-1">
                  {criticalityInfo[criticality].description}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function useBucketCount() {
  const [buckets, setBuckets] = useState(72)

  useEffect(() => {
    const updateBuckets = () => {
      const width = window.innerWidth
      if (width < 640) {
        setBuckets(24) // mobile
      } else if (width < 1024) {
        setBuckets(48) // tablet
      } else {
        setBuckets(72) // desktop
      }
    }

    updateBuckets()
    window.addEventListener('resize', updateBuckets)
    return () => window.removeEventListener('resize', updateBuckets)
  }, [])

  return buckets
}

// Check if link has interface issues in any bucket
function hasInterfaceIssues(hours: LinkHourStatus[]): boolean {
  return hours.some(h =>
    (h.side_a_in_errors ?? 0) > 0 || (h.side_a_out_errors ?? 0) > 0 ||
    (h.side_z_in_errors ?? 0) > 0 || (h.side_z_out_errors ?? 0) > 0 ||
    (h.side_a_in_discards ?? 0) > 0 || (h.side_a_out_discards ?? 0) > 0 ||
    (h.side_z_in_discards ?? 0) > 0 || (h.side_z_out_discards ?? 0) > 0 ||
    (h.side_a_carrier_transitions ?? 0) > 0 || (h.side_z_carrier_transitions ?? 0) > 0
  )
}

// Check if link has packet loss in any bucket
function hasPacketLoss(hours: LinkHourStatus[]): boolean {
  return hours.some(h => h.avg_loss_pct > 0)
}

type MetricType = 'errors' | 'discards' | 'carrier'

const METRIC_CONFIG: Record<MetricType, { label: string; dashArray?: string }> = {
  errors: { label: 'Errors', dashArray: undefined },
  discards: { label: 'Discards', dashArray: '5 5' },
  carrier: { label: 'Carrier', dashArray: '2 2' },
}

// Colors for side A and side Z
const SIDE_COLORS = {
  A: '#3b82f6', // blue
  Z: '#10b981', // emerald
}

interface LinkInterfaceChartProps {
  hours: LinkHourStatus[]
  bucketMinutes: number
  controlsWidth?: string
}

function LinkInterfaceChart({ hours, bucketMinutes, controlsWidth = 'w-32' }: LinkInterfaceChartProps) {
  const { resolvedTheme } = useTheme()
  const isDarkMode = resolvedTheme === 'dark'
  const [enabledMetrics, setEnabledMetrics] = useState<Set<MetricType>>(new Set(['errors', 'discards', 'carrier']))
  const [enabledSides, setEnabledSides] = useState<Set<'A' | 'Z'>>(new Set(['A', 'Z']))

  // Determine which metrics have data
  const availableMetrics = useMemo(() => {
    const metrics: Set<MetricType> = new Set()
    for (const h of hours) {
      if ((h.side_a_in_errors ?? 0) > 0 || (h.side_a_out_errors ?? 0) > 0 ||
          (h.side_z_in_errors ?? 0) > 0 || (h.side_z_out_errors ?? 0) > 0) {
        metrics.add('errors')
      }
      if ((h.side_a_in_discards ?? 0) > 0 || (h.side_a_out_discards ?? 0) > 0 ||
          (h.side_z_in_discards ?? 0) > 0 || (h.side_z_out_discards ?? 0) > 0) {
        metrics.add('discards')
      }
      if ((h.side_a_carrier_transitions ?? 0) > 0 || (h.side_z_carrier_transitions ?? 0) > 0) {
        metrics.add('carrier')
      }
    }
    return metrics
  }, [hours])

  // Determine which sides have data
  const availableSides = useMemo(() => {
    const sides: Set<'A' | 'Z'> = new Set()
    for (const h of hours) {
      if ((h.side_a_in_errors ?? 0) > 0 || (h.side_a_out_errors ?? 0) > 0 ||
          (h.side_a_in_discards ?? 0) > 0 || (h.side_a_out_discards ?? 0) > 0 ||
          (h.side_a_carrier_transitions ?? 0) > 0) {
        sides.add('A')
      }
      if ((h.side_z_in_errors ?? 0) > 0 || (h.side_z_out_errors ?? 0) > 0 ||
          (h.side_z_in_discards ?? 0) > 0 || (h.side_z_out_discards ?? 0) > 0 ||
          (h.side_z_carrier_transitions ?? 0) > 0) {
        sides.add('Z')
      }
    }
    return sides
  }, [hours])

  const toggleMetric = (metric: MetricType) => {
    setEnabledMetrics(prev => {
      const next = new Set(prev)
      if (next.has(metric)) next.delete(metric)
      else next.add(metric)
      return next
    })
  }

  const toggleSide = (side: 'A' | 'Z') => {
    setEnabledSides(prev => {
      const next = new Set(prev)
      if (next.has(side)) next.delete(side)
      else next.add(side)
      return next
    })
  }

  // Transform data for chart - in values positive, out values negative
  const chartData = hours.map(h => {
    const date = new Date(h.hour)
    return {
      time: date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      fullTime: h.hour,
      // Side A
      A_errors_in: h.side_a_in_errors ?? 0,
      A_errors_out: -(h.side_a_out_errors ?? 0),
      A_discards_in: h.side_a_in_discards ?? 0,
      A_discards_out: -(h.side_a_out_discards ?? 0),
      A_carrier: h.side_a_carrier_transitions ?? 0,
      // Side Z
      Z_errors_in: h.side_z_in_errors ?? 0,
      Z_errors_out: -(h.side_z_out_errors ?? 0),
      Z_discards_in: h.side_z_in_discards ?? 0,
      Z_discards_out: -(h.side_z_out_discards ?? 0),
      Z_carrier: h.side_z_carrier_transitions ?? 0,
    }
  })

  const CustomTooltip = ({ active, payload }: any) => {
    if (!active || !payload || payload.length === 0) return null
    const data = payload[0]?.payload
    if (!data) return null

    const formatTime = (isoString: string) => {
      const start = new Date(isoString)
      const end = new Date(start.getTime() + bucketMinutes * 60 * 1000)
      return `${start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ${end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    }

    return (
      <div className="bg-popover border border-border rounded-lg shadow-lg p-3 text-sm">
        <div className="font-medium mb-2">{formatTime(data.fullTime)}</div>
        <div className="space-y-1.5">
          {(['A', 'Z'] as const).map(side => {
            if (!enabledSides.has(side)) return null
            const errorsIn = data[`${side}_errors_in`] || 0
            const errorsOut = Math.abs(data[`${side}_errors_out`] || 0)
            const discardsIn = data[`${side}_discards_in`] || 0
            const discardsOut = Math.abs(data[`${side}_discards_out`] || 0)
            const carrier = data[`${side}_carrier`] || 0
            const hasData = (enabledMetrics.has('errors') && (errorsIn > 0 || errorsOut > 0)) ||
                           (enabledMetrics.has('discards') && (discardsIn > 0 || discardsOut > 0)) ||
                           (enabledMetrics.has('carrier') && carrier > 0)
            if (!hasData) return null
            return (
              <div key={side}>
                <div className="flex items-center gap-1.5 font-medium">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: SIDE_COLORS[side] }} />
                  <span>Side {side}</span>
                </div>
                <div className="pl-3.5 text-xs text-muted-foreground space-y-0.5">
                  {enabledMetrics.has('errors') && (errorsIn > 0 || errorsOut > 0) && (
                    <div>Errors: {errorsIn.toLocaleString()} in / {errorsOut.toLocaleString()} out</div>
                  )}
                  {enabledMetrics.has('discards') && (discardsIn > 0 || discardsOut > 0) && (
                    <div>Discards: {discardsIn.toLocaleString()} in / {discardsOut.toLocaleString()} out</div>
                  )}
                  {enabledMetrics.has('carrier') && carrier > 0 && (
                    <div>Carrier: {carrier.toLocaleString()}</div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  const gridColor = isDarkMode ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'
  const textColor = isDarkMode ? '#a1a1aa' : '#71717a'

  // Generate lines for each enabled side + metric
  const lines: { dataKey: string; color: string; strokeDasharray?: string }[] = []
  for (const side of ['A', 'Z'] as const) {
    if (!enabledSides.has(side)) continue
    const color = SIDE_COLORS[side]
    if (enabledMetrics.has('errors') && availableMetrics.has('errors')) {
      lines.push({ dataKey: `${side}_errors_in`, color })
      lines.push({ dataKey: `${side}_errors_out`, color })
    }
    if (enabledMetrics.has('discards') && availableMetrics.has('discards')) {
      lines.push({ dataKey: `${side}_discards_in`, color, strokeDasharray: '5 5' })
      lines.push({ dataKey: `${side}_discards_out`, color, strokeDasharray: '5 5' })
    }
    if (enabledMetrics.has('carrier') && availableMetrics.has('carrier')) {
      lines.push({ dataKey: `${side}_carrier`, color, strokeDasharray: '2 2' })
    }
  }

  return (
    <>
      {/* Controls */}
      <div className={`flex-shrink-0 ${controlsWidth} space-y-3`}>
        <div className="space-y-1.5">
          <span className="text-xs text-muted-foreground">Metrics</span>
          <div className="flex flex-wrap gap-1">
            {(['errors', 'discards', 'carrier'] as MetricType[]).map(metric => {
              if (!availableMetrics.has(metric)) return null
              const config = METRIC_CONFIG[metric]
              const isEnabled = enabledMetrics.has(metric)
              return (
                <button
                  key={metric}
                  onClick={() => toggleMetric(metric)}
                  className={`px-2 py-1 text-xs rounded border transition-colors flex items-center gap-1.5 ${
                    isEnabled
                      ? 'bg-background border-foreground/20 text-foreground shadow-sm'
                      : 'bg-muted/50 border-transparent text-muted-foreground hover:bg-muted'
                  }`}
                >
                  <svg width="12" height="2" style={{ opacity: isEnabled ? 1 : 0.3 }}>
                    <line x1="0" y1="1" x2="12" y2="1" stroke="currentColor" strokeWidth="2" strokeDasharray={config.dashArray} />
                  </svg>
                  {config.label}
                </button>
              )
            })}
          </div>
        </div>
        <div className="space-y-1.5">
          <span className="text-xs text-muted-foreground">Sides</span>
          <div className="flex flex-wrap gap-1">
            {(['A', 'Z'] as const).map(side => {
              if (!availableSides.has(side)) return null
              const isEnabled = enabledSides.has(side)
              return (
                <button
                  key={side}
                  onClick={() => toggleSide(side)}
                  className={`px-2 py-1 text-xs rounded border transition-colors flex items-center gap-1.5 ${
                    isEnabled
                      ? 'bg-background border-current shadow-sm'
                      : 'bg-muted/50 border-transparent text-muted-foreground hover:bg-muted'
                  }`}
                  style={isEnabled ? { borderColor: SIDE_COLORS[side], color: SIDE_COLORS[side] } : undefined}
                >
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: isEnabled ? SIDE_COLORS[side] : 'currentColor', opacity: isEnabled ? 1 : 0.3 }} />
                  Side {side}
                </button>
              )
            })}
          </div>
        </div>
      </div>

      {/* Chart */}
      <div className="flex-1 min-w-0 h-32">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData} margin={{ top: 5, right: 5, bottom: 5, left: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={gridColor} vertical={false} />
            <XAxis dataKey="time" tick={{ fontSize: 10, fill: textColor }} tickLine={false} axisLine={{ stroke: gridColor }} interval="preserveStartEnd" minTickGap={50} />
            <YAxis tick={{ fontSize: 10, fill: textColor }} tickLine={false} axisLine={false} width={40} domain={[(dataMin: number) => Math.min(dataMin, 0), (dataMax: number) => Math.max(dataMax, 0)]} tickFormatter={(v) => Math.abs(v) >= 1000 ? `${(Math.abs(v)/1000).toFixed(0)}k` : Math.abs(v).toString()} />
            <ReferenceLine y={0} stroke={isDarkMode ? '#666' : '#999'} strokeWidth={1.5} label={{ value: 'in ↑ / out ↓', position: 'right', fontSize: 9, fill: textColor }} />
            <RechartsTooltip content={<CustomTooltip />} />
            {lines.map(line => (
              <Line key={line.dataKey} type="monotone" dataKey={line.dataKey} stroke={line.color} strokeWidth={1.5} strokeDasharray={line.strokeDasharray} dot={false} connectNulls />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </>
  )
}

interface LinkPacketLossChartProps {
  hours: LinkHourStatus[]
  bucketMinutes: number
  controlsWidth?: string
}

function LinkPacketLossChart({ hours, bucketMinutes, controlsWidth = 'w-32' }: LinkPacketLossChartProps) {
  const { resolvedTheme } = useTheme()
  const isDarkMode = resolvedTheme === 'dark'
  const [enabledSeries, setEnabledSeries] = useState<Set<'total' | 'A' | 'Z'>>(new Set(['total', 'A', 'Z']))

  // Check which series have data
  const availableSeries = useMemo(() => {
    const series: Set<'total' | 'A' | 'Z'> = new Set()
    for (const h of hours) {
      if (h.avg_loss_pct > 0) series.add('total')
      if ((h.side_a_loss_pct ?? 0) > 0) series.add('A')
      if ((h.side_z_loss_pct ?? 0) > 0) series.add('Z')
    }
    return series
  }, [hours])

  const toggleSeries = (series: 'total' | 'A' | 'Z') => {
    setEnabledSeries(prev => {
      const next = new Set(prev)
      if (next.has(series)) next.delete(series)
      else next.add(series)
      return next
    })
  }

  const chartData = hours.map(h => ({
    time: new Date(h.hour).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    fullTime: h.hour,
    total: h.avg_loss_pct,
    A: h.side_a_loss_pct ?? 0,
    Z: h.side_z_loss_pct ?? 0,
  }))

  const CustomTooltip = ({ active, payload }: any) => {
    if (!active || !payload || payload.length === 0) return null
    const data = payload[0]?.payload
    if (!data) return null

    const formatTime = (isoString: string) => {
      const start = new Date(isoString)
      const end = new Date(start.getTime() + bucketMinutes * 60 * 1000)
      return `${start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ${end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    }

    return (
      <div className="bg-popover border border-border rounded-lg shadow-lg p-3 text-sm">
        <div className="font-medium mb-2">{formatTime(data.fullTime)}</div>
        <div className="space-y-1 text-xs">
          {enabledSeries.has('total') && <div>Total: {data.total.toFixed(2)}%</div>}
          {enabledSeries.has('A') && availableSeries.has('A') && <div style={{ color: SIDE_COLORS.A }}>Side A: {data.A.toFixed(2)}%</div>}
          {enabledSeries.has('Z') && availableSeries.has('Z') && <div style={{ color: SIDE_COLORS.Z }}>Side Z: {data.Z.toFixed(2)}%</div>}
        </div>
      </div>
    )
  }

  const gridColor = isDarkMode ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'
  const textColor = isDarkMode ? '#a1a1aa' : '#71717a'

  const SERIES_CONFIG = {
    total: { color: '#a855f7', label: 'Total' }, // purple
    A: { color: SIDE_COLORS.A, label: 'Side A' },
    Z: { color: SIDE_COLORS.Z, label: 'Side Z' },
  }

  return (
    <>
      {/* Controls */}
      <div className={`flex-shrink-0 ${controlsWidth} space-y-1.5`}>
        <span className="text-xs text-muted-foreground">Series</span>
        <div className="flex flex-wrap gap-1">
          {(['total', 'A', 'Z'] as const).map(series => {
            if (series !== 'total' && !availableSeries.has(series)) return null
            const config = SERIES_CONFIG[series]
            const isEnabled = enabledSeries.has(series)
            return (
              <button
                key={series}
                onClick={() => toggleSeries(series)}
                className={`px-2 py-1 text-xs rounded border transition-colors flex items-center gap-1.5 ${
                  isEnabled
                    ? 'bg-background border-current shadow-sm'
                    : 'bg-muted/50 border-transparent text-muted-foreground hover:bg-muted'
                }`}
                style={isEnabled ? { borderColor: config.color, color: config.color } : undefined}
              >
                <span className="w-2 h-2 rounded-full" style={{ backgroundColor: isEnabled ? config.color : 'currentColor', opacity: isEnabled ? 1 : 0.3 }} />
                {config.label}
              </button>
            )
          })}
        </div>
      </div>

      {/* Chart */}
      <div className="flex-1 min-w-0 h-32">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData} margin={{ top: 5, right: 5, bottom: 5, left: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={gridColor} vertical={false} />
            <XAxis dataKey="time" tick={{ fontSize: 10, fill: textColor }} tickLine={false} axisLine={{ stroke: gridColor }} interval="preserveStartEnd" minTickGap={50} />
            <YAxis tick={{ fontSize: 10, fill: textColor }} tickLine={false} axisLine={false} width={40} domain={[0, 100]} tickFormatter={(v) => `${v}%`} />
            <RechartsTooltip content={<CustomTooltip />} />
            {enabledSeries.has('total') && <Line type="monotone" dataKey="total" stroke={SERIES_CONFIG.total.color} strokeWidth={2} dot={false} connectNulls />}
            {enabledSeries.has('A') && availableSeries.has('A') && <Line type="monotone" dataKey="A" stroke={SERIES_CONFIG.A.color} strokeWidth={1.5} strokeDasharray="5 5" dot={false} connectNulls />}
            {enabledSeries.has('Z') && availableSeries.has('Z') && <Line type="monotone" dataKey="Z" stroke={SERIES_CONFIG.Z.color} strokeWidth={1.5} strokeDasharray="5 5" dot={false} connectNulls />}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </>
  )
}

interface LinkRowProps {
  link: LinkHistory
  linksWithIssues?: Map<string, string[]>
  criticalityMap?: Map<string, 'critical' | 'important' | 'redundant'>
  bucketMinutes?: number
  dataTimeRange?: string
}

function LinkRow({ link, linksWithIssues, criticalityMap, bucketMinutes = 60, dataTimeRange }: LinkRowProps) {
  const [expanded, setExpanded] = useState(false)

  const issueReasons = linksWithIssues
    ? (linksWithIssues.get(link.code) ?? [])
    : (link.issue_reasons ?? [])

  const showInterfaceChart = hasInterfaceIssues(link.hours)
  const showPacketLossChart = hasPacketLoss(link.hours)
  const hasExpandableContent = showInterfaceChart || showPacketLossChart

  return (
    <div className="border-b border-border last:border-b-0">
      <div
        className={`px-4 py-3 transition-colors ${hasExpandableContent ? 'cursor-pointer hover:bg-muted/30' : ''}`}
        onClick={hasExpandableContent ? () => setExpanded(!expanded) : undefined}
      >
        <div className="flex items-start gap-4">
          {/* Expand/collapse indicator */}
          <div className="flex-shrink-0 w-5 pt-0.5">
            {hasExpandableContent ? (
              expanded ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />
            ) : (
              <div className="w-4" />
            )}
          </div>

          {/* Link info */}
          <div className="flex-shrink-0 w-52 sm:w-60 lg:w-68">
            <div className="flex items-center gap-1.5">
              <Link to={`/dz/links/${link.pk}`} className="font-mono text-sm truncate hover:underline" title={link.code} onClick={(e) => e.stopPropagation()}>
                {link.code}
              </Link>
              <LinkInfoPopover link={link} criticality={criticalityMap?.get(link.code)} />
              {criticalityMap?.get(link.code) && criticalityMap.get(link.code) !== 'redundant' && (
                <CriticalityBadge criticality={criticalityMap.get(link.code)!} />
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              {link.link_type}{link.contributor && ` · ${link.contributor}`}
            </div>
            {issueReasons.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-1">
                {issueReasons.includes('packet_loss') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(168, 85, 247, 0.15)', color: '#9333ea' }}>Loss</span>
                )}
                {issueReasons.includes('high_latency') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(59, 130, 246, 0.15)', color: '#2563eb' }}>High Latency</span>
                )}
                {issueReasons.includes('extended_loss') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(249, 115, 22, 0.15)', color: '#ea580c' }}>Extended Loss</span>
                )}
                {issueReasons.includes('drained') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(100, 116, 139, 0.15)', color: '#475569' }}>Drained</span>
                )}
                {issueReasons.includes('no_data') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(236, 72, 153, 0.15)', color: '#db2777' }}>No Data</span>
                )}
                {issueReasons.includes('interface_errors') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(239, 68, 68, 0.15)', color: '#dc2626' }}>Errors</span>
                )}
                {issueReasons.includes('discards') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(20, 184, 166, 0.15)', color: '#0d9488' }}>Discards</span>
                )}
                {issueReasons.includes('carrier_transitions') && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{ backgroundColor: 'rgba(234, 179, 8, 0.15)', color: '#ca8a04' }}>Carrier</span>
                )}
              </div>
            )}
          </div>

          {/* Timeline */}
          <div className="flex-1 min-w-0">
            <StatusTimeline
              hours={link.hours}
              committedRttUs={link.committed_rtt_us}
              bucketMinutes={bucketMinutes}
              timeRange={dataTimeRange}
            />
          </div>
        </div>
      </div>

      {/* Expanded charts */}
      {expanded && hasExpandableContent && (
        <div className="px-4 pb-4 pt-0 space-y-4">
          {showPacketLossChart && (
            <div>
              <div className="flex items-start gap-4 mb-2">
                <div className="flex-shrink-0 w-5" />
                <div className="text-xs font-medium text-muted-foreground">Packet Loss</div>
              </div>
              <div className="flex items-start gap-4">
                <div className="flex-shrink-0 w-5" />
                <LinkPacketLossChart hours={link.hours} bucketMinutes={bucketMinutes} controlsWidth="w-52 sm:w-60 lg:w-68" />
              </div>
            </div>
          )}
          {showInterfaceChart && (
            <div>
              <div className="flex items-start gap-4 mb-2">
                <div className="flex-shrink-0 w-5" />
                <div className="text-xs font-medium text-muted-foreground">Interface Issues</div>
              </div>
              <div className="flex items-start gap-4">
                <div className="flex-shrink-0 w-5" />
                <LinkInterfaceChart hours={link.hours} bucketMinutes={bucketMinutes} controlsWidth="w-52 sm:w-60 lg:w-68" />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function LinkStatusTimelines({
  timeRange = '24h',
  onTimeRangeChange,
  issueFilters = ['packet_loss', 'high_latency', 'extended_loss', 'drained', 'interface_errors', 'discards', 'carrier_transitions'],
  healthFilters = ['healthy', 'degraded', 'unhealthy', 'disabled'],
  linksWithIssues,
  linksWithHealth,
  criticalityMap,
}: LinkStatusTimelinesProps) {
  const timeRangeOptions: { value: TimeRange; label: string }[] = [
    { value: '3h', label: '3h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '3d', label: '3d' },
    { value: '7d', label: '7d' },
  ]
  const buckets = useBucketCount()

  const { data, isLoading, error } = useQuery({
    queryKey: ['link-history', timeRange, buckets],
    queryFn: () => fetchLinkHistory(timeRange, buckets),
    refetchInterval: 60_000, // Refresh every minute
    staleTime: 30_000,
  })

  // Helper to check if a link matches health filters
  // Uses linksWithHealth (from filter time range) if provided, otherwise falls back to link's own hours
  const linkMatchesHealthFilters = (link: LinkHistory): boolean => {
    // If we have health data from the filter time range, use it
    if (linksWithHealth) {
      const health = linksWithHealth.get(link.code)
      if (health) {
        // Map no_data to unhealthy for filter matching (no_data is a status, not a filter option)
        const filterHealth = health === 'no_data' ? 'unhealthy' : health
        return healthFilters.includes(filterHealth as any)
      }
      // Link not in filter data - check if it exists in history
      return false
    }

    // Fallback: check link's own hours data
    if (!link.hours || link.hours.length === 0) return false
    return link.hours.some(hour => {
      const status = hour.status
      if (status === 'healthy' && healthFilters.includes('healthy')) return true
      if (status === 'degraded' && healthFilters.includes('degraded')) return true
      if (status === 'unhealthy' && healthFilters.includes('unhealthy')) return true
      if (status === 'disabled' && healthFilters.includes('disabled')) return true
      if (status === 'no_data' && healthFilters.includes('unhealthy')) return true // no_data maps to unhealthy
      return false
    })
  }

  // Check which issue filters are selected
  const issueTypesSelected = issueFilters.filter(f => f !== 'no_issues')
  const noIssuesSelected = issueFilters.includes('no_issues')

  // Filter and sort links by recency of issues
  const filteredLinks = useMemo(() => {
    if (!data?.links) return []

    // Filter by issue reasons (from filter time range if provided) AND health status
    const filtered = data.links.filter(link => {
      // Use linksWithIssues if provided - if link not in map, it has no issues in the filter time range
      // Only fall back to link.issue_reasons if linksWithIssues is not provided at all
      const issueReasons = linksWithIssues
        ? (linksWithIssues.get(link.code) ?? [])
        : (link.issue_reasons ?? [])
      const hasIssues = issueReasons.length > 0

      // Check if link matches issue filters
      let matchesIssue = false
      if (hasIssues) {
        // Link has issues - check if any match the selected issue types
        matchesIssue = issueReasons.some(reason => issueTypesSelected.includes(reason))
      } else {
        // Link has no issues - include if "no_issues" filter is selected
        matchesIssue = noIssuesSelected
      }

      // Must match at least one health filter
      const matchesHealth = linkMatchesHealthFilters(link)

      return matchesIssue && matchesHealth
    })

    // Sort by most recent issue (higher index in hours = more recent)
    // Issues are: unhealthy, degraded, disabled
    return filtered.sort((a, b) => {
      const getLatestIssueIndex = (link: LinkHistory): number => {
        if (!link.hours) return -1
        for (let i = link.hours.length - 1; i >= 0; i--) {
          const status = link.hours[i].status
          if (status === 'unhealthy' || status === 'degraded' || status === 'disabled') {
            return i
          }
        }
        return -1
      }

      const aIndex = getLatestIssueIndex(a)
      const bIndex = getLatestIssueIndex(b)

      // Higher index = more recent = should come first
      return bIndex - aIndex
    })
  }, [data?.links, issueFilters, healthFilters, noIssuesSelected, issueTypesSelected, linksWithIssues, linksWithHealth])

  if (isLoading) {
    return (
      <div className="border border-border rounded-lg p-6 flex items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground mr-2" />
        <span className="text-sm text-muted-foreground">Loading link history...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <AlertTriangle className="h-8 w-8 text-amber-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">Unable to load link history</div>
      </div>
    )
  }

  if (filteredLinks.length === 0) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <CheckCircle2 className="h-8 w-8 text-green-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">
          {data?.links.length === 0
            ? 'No links available in the selected time range'
            : 'No links match the selected filters'}
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg">
      <div className="px-4 py-2.5 bg-muted/50 border-b border-border flex items-center gap-2 rounded-t-lg">
        <History className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">
          Link Status History
          <span className="text-sm text-muted-foreground font-normal ml-1">
            ({filteredLinks.length} link{filteredLinks.length !== 1 ? 's' : ''})
          </span>
        </h3>
        {onTimeRangeChange && (
          <div className="inline-flex rounded-lg border border-border bg-background/50 p-0.5 ml-auto">
            {timeRangeOptions.map((opt) => (
              <button
                key={opt.value}
                onClick={() => onTimeRangeChange(opt.value)}
                className={`px-2.5 py-0.5 text-xs rounded-md transition-colors ${
                  timeRange === opt.value
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Legend */}
      <div className="px-4 py-2 border-b border-border bg-muted/30 flex items-center gap-4 text-xs text-muted-foreground">
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-green-500" />
          <span>Healthy</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-amber-500" />
          <span>Degraded</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-red-500" />
          <span>Unhealthy</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-transparent border border-gray-200 dark:border-gray-700" />
          <span>No Data</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-gray-500 dark:bg-gray-700" />
          <span>Disabled</span>
        </div>
      </div>

      <div>
        {filteredLinks.map((link) => (
          <LinkRow
            key={link.code}
            link={link}
            linksWithIssues={linksWithIssues}
            criticalityMap={criticalityMap}
            bucketMinutes={data?.bucket_minutes}
            dataTimeRange={data?.time_range}
          />
        ))}
      </div>
    </div>
  )
}
