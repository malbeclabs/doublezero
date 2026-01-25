import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, CheckCircle2, AlertTriangle, History, Info, ChevronDown, ChevronUp } from 'lucide-react'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { fetchDeviceHistory, fetchDeviceInterfaceHistory } from '@/lib/api'
import type { DeviceHistory, DeviceHourStatus } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'

interface DeviceStatusTimelinesProps {
  timeRange?: string
  onTimeRangeChange?: (range: TimeRange) => void
  issueFilters?: string[]
  healthFilters?: string[]
  devicesWithIssues?: Map<string, string[]>  // Map of device code -> issue reasons (from filter time range)
  devicesWithHealth?: Map<string, string>    // Map of device code -> health status (from filter time range)
}

function DeviceInfoPopover({ device }: { device: DeviceHistory }) {
  const [isOpen, setIsOpen] = useState(false)

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
          className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 min-w-[200px]"
          onMouseEnter={() => setIsOpen(true)}
          onMouseLeave={() => setIsOpen(false)}
        >
          <div className="space-y-2 text-xs">
            <div>
              <div className="text-muted-foreground">Metro</div>
              <div className="font-medium">{device.metro || '—'}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Type</div>
              <div className="font-medium capitalize">{device.device_type?.replace(/_/g, ' ')}</div>
            </div>
            {device.max_users > 0 && (
              <div>
                <div className="text-muted-foreground">Max Users</div>
                <div className="font-medium">{device.max_users}</div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// Status colors and labels for timeline
const statusColors = {
  healthy: 'bg-green-500',
  degraded: 'bg-amber-500',
  unhealthy: 'bg-red-500',
  no_data: 'bg-transparent border border-gray-200 dark:border-gray-700',
  disabled: 'bg-gray-500 dark:bg-gray-700',
}

const statusLabels = {
  healthy: 'Healthy',
  degraded: 'Degraded',
  unhealthy: 'Unhealthy',
  no_data: 'No Data',
  disabled: 'Disabled',
}

function formatDate(isoString: string): string {
  const date = new Date(isoString)
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function formatTimeRange(isoString: string, bucketMinutes: number = 60): string {
  const start = new Date(isoString)
  const end = new Date(start.getTime() + bucketMinutes * 60 * 1000)
  const startTime = start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const endTime = end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (start.getDate() !== end.getDate()) {
    return `${formatDate(isoString)} ${startTime} — ${formatDate(end.toISOString())} ${endTime}`
  }
  return `${formatDate(isoString)} ${startTime} — ${endTime}`
}

interface DeviceStatusTimelineProps {
  hours: DeviceHourStatus[]
  bucketMinutes?: number
  timeRange?: string
}

function DeviceStatusTimeline({ hours, bucketMinutes = 60, timeRange = '24h' }: DeviceStatusTimelineProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)

  const timeLabels: Record<string, string> = {
    '1h': '1h ago',
    '3h': '3h ago',
    '6h': '6h ago',
    '12h': '12h ago',
    '24h': '24h ago',
    '3d': '3d ago',
    '7d': '7d ago',
  }
  const timeLabel = timeLabels[timeRange] || '24h ago'

  return (
    <div className="relative">
      <div className="flex gap-[2px]">
        {hours.map((hour, index) => (
          <div
            key={hour.hour}
            className="relative flex-1 min-w-0"
            onMouseEnter={() => setHoveredIndex(index)}
            onMouseLeave={() => setHoveredIndex(null)}
          >
            <div
              className={`w-full h-6 rounded-sm ${statusColors[hour.status]} cursor-pointer transition-opacity hover:opacity-80`}
            />

            {/* Tooltip */}
            {hoveredIndex === index && (
              <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-50">
                <div className="bg-popover border border-border rounded-lg shadow-lg p-3 whitespace-nowrap text-sm">
                  <div className="font-medium mb-1">
                    {formatTimeRange(hour.hour, bucketMinutes)}
                  </div>
                  <div className={`text-xs mb-2 ${
                    hour.status === 'healthy' ? 'text-green-600 dark:text-green-400' :
                    hour.status === 'degraded' ? 'text-amber-600 dark:text-amber-400' :
                    hour.status === 'unhealthy' ? 'text-red-600 dark:text-red-400' :
                    'text-muted-foreground'
                  }`}>
                    {statusLabels[hour.status]}
                  </div>
                  {hour.status !== 'no_data' && (
                    <div className="space-y-1 text-muted-foreground">
                      {(hour.in_errors > 0 || hour.out_errors > 0) && (
                        <div className="flex justify-between gap-4">
                          <span>Errors:</span>
                          <span className="font-mono">
                            {(hour.in_errors + hour.out_errors).toLocaleString()}
                            <span className="text-xs ml-1">
                              (in: {hour.in_errors.toLocaleString()}, out: {hour.out_errors.toLocaleString()})
                            </span>
                          </span>
                        </div>
                      )}
                      {(hour.in_discards > 0 || hour.out_discards > 0) && (
                        <div className="flex justify-between gap-4">
                          <span>Discards:</span>
                          <span className="font-mono">
                            {(hour.in_discards + hour.out_discards).toLocaleString()}
                            <span className="text-xs ml-1">
                              (in: {hour.in_discards.toLocaleString()}, out: {hour.out_discards.toLocaleString()})
                            </span>
                          </span>
                        </div>
                      )}
                      {hour.carrier_transitions > 0 && (
                        <div className="flex justify-between gap-4">
                          <span>Carrier Transitions:</span>
                          <span className="font-mono">{hour.carrier_transitions.toLocaleString()}</span>
                        </div>
                      )}
                      {hour.max_users > 0 && (
                        <div className="flex justify-between gap-4">
                          <span>Utilization:</span>
                          <span className="font-mono">
                            {hour.utilization_pct.toFixed(1)}%
                            <span className="text-xs ml-1">
                              ({hour.current_users}/{hour.max_users})
                            </span>
                          </span>
                        </div>
                      )}
                      {hour.in_errors === 0 && hour.out_errors === 0 &&
                       hour.in_discards === 0 && hour.out_discards === 0 &&
                       hour.carrier_transitions === 0 && hour.max_users === 0 && (
                        <div className="text-xs">No issues detected</div>
                      )}
                    </div>
                  )}
                </div>
                {/* Arrow */}
                <div className="absolute top-full left-1/2 -translate-x-1/2 -mt-[1px]">
                  <div className="border-8 border-transparent border-t-border" />
                  <div className="absolute top-0 left-1/2 -translate-x-1/2 border-[7px] border-transparent border-t-popover" />
                </div>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Time labels */}
      <div className="flex justify-between mt-1 text-[10px] text-muted-foreground">
        <span>{timeLabel}</span>
        <span>Now</span>
      </div>
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

// Color palette for interfaces (distinct colors)
const INTERFACE_COLORS = [
  '#3b82f6', // blue-500
  '#10b981', // emerald-500
  '#f59e0b', // amber-500
  '#ef4444', // red-500
  '#8b5cf6', // violet-500
  '#ec4899', // pink-500
  '#06b6d4', // cyan-500
  '#84cc16', // lime-500
  '#f97316', // orange-500
  '#6366f1', // indigo-500
]

type MetricType = 'errors' | 'discards' | 'carrier'

const METRIC_CONFIG: Record<MetricType, { label: string; color: string }> = {
  errors: { label: 'Errors', color: '#d946ef' },
  discards: { label: 'Discards', color: '#f43f5e' },
  carrier: { label: 'Carrier Transitions', color: '#f97316' },
}

// Interface toggle button with popover for link info
interface InterfaceToggleButtonProps {
  intf: {
    interface_name: string
    link_pk?: string
    link_code?: string
    link_type?: string
    link_side?: string
  }
  isEnabled: boolean
  color: string
  onToggle: () => void
}

function InterfaceToggleButton({ intf, isEnabled, color, onToggle }: InterfaceToggleButtonProps) {
  const [showPopover, setShowPopover] = useState(false)
  const hasLinkInfo = intf.link_code || intf.link_type

  return (
    <div className="relative">
      <button
        onClick={onToggle}
        onMouseEnter={() => hasLinkInfo && setShowPopover(true)}
        onMouseLeave={() => setShowPopover(false)}
        className={`w-full px-2 py-1 text-xs rounded border transition-colors flex items-center gap-1.5 text-left ${
          isEnabled
            ? 'border-current'
            : 'border-border text-muted-foreground hover:bg-muted'
        }`}
        style={isEnabled ? { borderColor: color, color } : undefined}
      >
        <span
          className="w-2 h-2 rounded-full flex-shrink-0"
          style={{ backgroundColor: isEnabled ? color : 'currentColor', opacity: isEnabled ? 1 : 0.3 }}
        />
        <span className="truncate">{intf.interface_name}</span>
      </button>
      {showPopover && hasLinkInfo && (
        <div
          className="absolute left-full top-0 ml-2 z-50 bg-popover border border-border rounded-lg shadow-lg p-2 whitespace-nowrap text-xs"
          onMouseEnter={() => setShowPopover(true)}
          onMouseLeave={() => setShowPopover(false)}
        >
          <div className="space-y-1">
            {intf.link_code && (
              <div>
                <span className="text-muted-foreground">Link: </span>
                <span className="font-medium">{intf.link_code}</span>
              </div>
            )}
            {intf.link_type && (
              <div>
                <span className="text-muted-foreground">Type: </span>
                <span>{intf.link_type}</span>
              </div>
            )}
            {intf.link_side && (
              <div>
                <span className="text-muted-foreground">Side: </span>
                <span>{intf.link_side}</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

interface InterfaceIssueChartProps {
  devicePk: string
  timeRange: string
  buckets: number
  controlsWidth?: string
}

function InterfaceIssueChart({ devicePk, timeRange, buckets, controlsWidth = 'w-44' }: InterfaceIssueChartProps) {
  const { resolvedTheme } = useTheme()
  const isDarkMode = resolvedTheme === 'dark'
  const [enabledInterfaces, setEnabledInterfaces] = useState<Set<string>>(new Set())
  const [enabledMetrics, setEnabledMetrics] = useState<Set<MetricType>>(new Set(['errors', 'discards', 'carrier']))
  const [initialized, setInitialized] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['device-interface-history', devicePk, timeRange, buckets],
    queryFn: () => fetchDeviceInterfaceHistory(devicePk, timeRange, buckets),
    staleTime: 30_000,
  })

  // Filter interfaces that have any issues
  const interfacesWithIssues = useMemo(() => {
    if (!data?.interfaces) return []
    return data.interfaces.filter(intf =>
      intf.hours.some(h => h.in_errors > 0 || h.out_errors > 0 || h.in_discards > 0 || h.out_discards > 0 || h.carrier_transitions > 0)
    )
  }, [data?.interfaces])

  // Initialize enabled interfaces to all when data loads
  useEffect(() => {
    if (interfacesWithIssues.length > 0 && !initialized) {
      setEnabledInterfaces(new Set(interfacesWithIssues.map(i => i.interface_name)))
      setInitialized(true)
    }
  }, [interfacesWithIssues, initialized])

  // Determine which metrics have data across all interfaces
  const availableMetrics = useMemo(() => {
    const metrics: Set<MetricType> = new Set()
    for (const intf of interfacesWithIssues) {
      for (const h of intf.hours) {
        if (h.in_errors > 0 || h.out_errors > 0) metrics.add('errors')
        if (h.in_discards > 0 || h.out_discards > 0) metrics.add('discards')
        if (h.carrier_transitions > 0) metrics.add('carrier')
      }
    }
    return metrics
  }, [interfacesWithIssues])

  // Build color map for interfaces
  const interfaceColorMap = useMemo(() => {
    const map: Record<string, string> = {}
    interfacesWithIssues.forEach((intf, idx) => {
      map[intf.interface_name] = INTERFACE_COLORS[idx % INTERFACE_COLORS.length]
    })
    return map
  }, [interfacesWithIssues])

  const toggleInterface = (name: string) => {
    setEnabledInterfaces(prev => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      return next
    })
  }

  const toggleMetric = (metric: MetricType) => {
    setEnabledMetrics(prev => {
      const next = new Set(prev)
      if (next.has(metric)) {
        next.delete(metric)
      } else {
        next.add(metric)
      }
      return next
    })
  }

  if (isLoading) {
    return (
      <>
        <div className={`flex-shrink-0 ${controlsWidth}`} />
        <div className="flex-1 flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground mr-2" />
          <span className="text-sm text-muted-foreground">Loading interface history...</span>
        </div>
      </>
    )
  }

  if (error || !data) {
    return (
      <>
        <div className={`flex-shrink-0 ${controlsWidth}`} />
        <div className="flex-1 text-center py-4 text-sm text-muted-foreground">
          Unable to load interface history
        </div>
      </>
    )
  }

  if (data.interfaces.length === 0) {
    return (
      <>
        <div className={`flex-shrink-0 ${controlsWidth}`} />
        <div className="flex-1 text-center py-4 text-sm text-muted-foreground">
          No interface data available
        </div>
      </>
    )
  }

  if (interfacesWithIssues.length === 0) {
    return (
      <>
        <div className={`flex-shrink-0 ${controlsWidth}`} />
        <div className="flex-1 text-center py-4 text-sm text-muted-foreground">
          No interface issues in this time range
        </div>
      </>
    )
  }

  // Transform data for the chart - each data point has values for all interfaces
  const chartData = data.interfaces[0].hours.map((hour, idx) => {
    const date = new Date(hour.hour)
    const point: Record<string, any> = {
      time: date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      fullTime: hour.hour,
    }

    // Add data for each interface
    for (const intf of interfacesWithIssues) {
      const h = intf.hours[idx]
      if (h) {
        point[`${intf.interface_name}_errors`] = h.in_errors + h.out_errors
        point[`${intf.interface_name}_discards`] = h.in_discards + h.out_discards
        point[`${intf.interface_name}_carrier`] = h.carrier_transitions
      }
    }

    return point
  })

  // Custom tooltip
  const CustomTooltip = ({ active, payload }: any) => {
    if (!active || !payload || payload.length === 0) return null
    const data = payload[0]?.payload
    if (!data) return null

    // Group by interface
    const interfaceData: Record<string, { errors: number; discards: number; carrier: number; color: string }> = {}
    for (const intf of interfacesWithIssues) {
      if (!enabledInterfaces.has(intf.interface_name)) continue
      const errors = data[`${intf.interface_name}_errors`] || 0
      const discards = data[`${intf.interface_name}_discards`] || 0
      const carrier = data[`${intf.interface_name}_carrier`] || 0
      if (errors > 0 || discards > 0 || carrier > 0) {
        interfaceData[intf.interface_name] = {
          errors,
          discards,
          carrier,
          color: interfaceColorMap[intf.interface_name],
        }
      }
    }

    const hasData = Object.keys(interfaceData).length > 0

    return (
      <div className="bg-popover border border-border rounded-lg shadow-lg p-3 text-sm max-w-xs">
        <div className="font-medium mb-2">
          {new Date(data.fullTime).toLocaleString([], {
            month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
          })}
        </div>
        {hasData ? (
          <div className="space-y-2">
            {Object.entries(interfaceData).map(([name, values]) => (
              <div key={name} className="space-y-0.5">
                <div className="flex items-center gap-1.5 font-medium">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: values.color }} />
                  <span className="truncate">{name}</span>
                </div>
                <div className="pl-3.5 text-xs text-muted-foreground space-y-0.5">
                  {enabledMetrics.has('errors') && values.errors > 0 && (
                    <div>Errors: {values.errors.toLocaleString()}</div>
                  )}
                  {enabledMetrics.has('discards') && values.discards > 0 && (
                    <div>Discards: {values.discards.toLocaleString()}</div>
                  )}
                  {enabledMetrics.has('carrier') && values.carrier > 0 && (
                    <div>Carrier: {values.carrier.toLocaleString()}</div>
                  )}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-muted-foreground">No issues</div>
        )}
      </div>
    )
  }

  const gridColor = isDarkMode ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)'
  const textColor = isDarkMode ? '#a1a1aa' : '#71717a'

  // Generate lines for each enabled interface + metric combination
  const lines: { dataKey: string; color: string; strokeDasharray?: string }[] = []
  for (const intf of interfacesWithIssues) {
    if (!enabledInterfaces.has(intf.interface_name)) continue
    const color = interfaceColorMap[intf.interface_name]

    if (enabledMetrics.has('errors') && availableMetrics.has('errors')) {
      lines.push({ dataKey: `${intf.interface_name}_errors`, color, strokeDasharray: undefined })
    }
    if (enabledMetrics.has('discards') && availableMetrics.has('discards')) {
      lines.push({ dataKey: `${intf.interface_name}_discards`, color, strokeDasharray: '5 5' })
    }
    if (enabledMetrics.has('carrier') && availableMetrics.has('carrier')) {
      lines.push({ dataKey: `${intf.interface_name}_carrier`, color, strokeDasharray: '2 2' })
    }
  }

  return (
    <>
      {/* Controls on left */}
      <div className={`flex-shrink-0 ${controlsWidth} space-y-3`}>
        {/* Metric toggles */}
        <div className="space-y-1.5">
          <span className="text-xs text-muted-foreground">Metrics</span>
          <div className="flex flex-col gap-1">
            {(['errors', 'discards', 'carrier'] as MetricType[]).map(metric => {
              if (!availableMetrics.has(metric)) return null
              const config = METRIC_CONFIG[metric]
              const isEnabled = enabledMetrics.has(metric)
              const dashArray = metric === 'errors' ? undefined : metric === 'discards' ? '5 5' : '2 2'
              return (
                <button
                  key={metric}
                  onClick={() => toggleMetric(metric)}
                  className={`px-2 py-1 text-xs rounded border transition-colors flex items-center gap-1.5 ${
                    isEnabled
                      ? 'bg-primary/10 border-primary/50 text-foreground'
                      : 'border-border text-muted-foreground hover:bg-muted'
                  }`}
                >
                  <svg width="12" height="2" style={{ opacity: isEnabled ? 1 : 0.3 }}>
                    <line
                      x1="0"
                      y1="1"
                      x2="12"
                      y2="1"
                      stroke={isEnabled ? config.color : 'currentColor'}
                      strokeWidth="2"
                      strokeDasharray={dashArray}
                    />
                  </svg>
                  {config.label}
                </button>
              )
            })}
          </div>
        </div>

        {/* Interface toggles */}
        <div className="space-y-1.5">
          <span className="text-xs text-muted-foreground">Interfaces</span>
          <div className="flex flex-col gap-1">
            {interfacesWithIssues.map(intf => {
              const isEnabled = enabledInterfaces.has(intf.interface_name)
              const color = interfaceColorMap[intf.interface_name]
              return (
                <InterfaceToggleButton
                  key={intf.interface_name}
                  intf={intf}
                  isEnabled={isEnabled}
                  color={color}
                  onToggle={() => toggleInterface(intf.interface_name)}
                />
              )
            })}
          </div>
        </div>
      </div>

      {/* Chart on right - aligned with timeline */}
      <div className="flex-1 min-w-0">
        <div className="h-32 outline-none [&_.recharts-wrapper]:outline-none [&_svg]:outline-none">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 5, right: 5, bottom: 5, left: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={gridColor} vertical={false} />
              <XAxis
                dataKey="time"
                tick={{ fontSize: 10, fill: textColor }}
                tickLine={false}
                axisLine={{ stroke: gridColor }}
                interval="preserveStartEnd"
                minTickGap={50}
              />
              <YAxis
                tick={{ fontSize: 10, fill: textColor }}
                tickLine={false}
                axisLine={false}
                width={40}
                tickFormatter={(v) => v >= 1000 ? `${(v/1000).toFixed(0)}k` : v}
              />
              <RechartsTooltip content={<CustomTooltip />} />
              {lines.map(line => (
                <Line
                  key={line.dataKey}
                  type="monotone"
                  dataKey={line.dataKey}
                  stroke={line.color}
                  strokeWidth={1.5}
                  strokeDasharray={line.strokeDasharray}
                  dot={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>
    </>
  )
}

// Device row component with expand/collapse
interface DeviceRowProps {
  device: DeviceHistory
  devicesWithIssues?: Map<string, string[]>
  bucketMinutes?: number
  dataTimeRange?: string
  buckets: number
  timeRange: string
}

function DeviceRow({ device, devicesWithIssues, bucketMinutes, dataTimeRange, buckets, timeRange }: DeviceRowProps) {
  const [expanded, setExpanded] = useState(false)

  const issueReasons = devicesWithIssues
    ? (devicesWithIssues.get(device.code) ?? [])
    : (device.issue_reasons ?? [])

  const hasIssues = issueReasons.length > 0

  return (
    <div className="border-b border-border last:border-b-0">
      <div
        className={`px-4 py-3 transition-colors ${hasIssues ? 'cursor-pointer hover:bg-muted/30' : ''}`}
        onClick={hasIssues ? () => setExpanded(!expanded) : undefined}
      >
        <div className="flex items-start gap-4">
          {/* Expand/collapse indicator */}
          <div className="flex-shrink-0 w-5 pt-0.5">
            {hasIssues ? (
              expanded ? (
                <ChevronUp className="h-4 w-4 text-muted-foreground" />
              ) : (
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
              )
            ) : (
              <div className="w-4" />
            )}
          </div>

          {/* Device info */}
          <div className="flex-shrink-0 w-44">
            <div className="flex items-center gap-1.5">
              <Link
                to={`/dz/devices/${device.pk}`}
                className="font-mono text-sm truncate hover:underline"
                title={device.code}
                onClick={(e) => e.stopPropagation()}
              >
                {device.code}
              </Link>
              <DeviceInfoPopover device={device} />
            </div>
            {device.contributor && (
              <div className="text-xs text-muted-foreground">{device.contributor}</div>
            )}
            {issueReasons.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-1">
                {issueReasons.includes('interface_errors') && (
                  <span
                    className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                    style={{ backgroundColor: 'rgba(217, 70, 239, 0.15)', color: '#a21caf' }}
                  >
                    Interface Errors
                  </span>
                )}
                {issueReasons.includes('discards') && (
                  <span
                    className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                    style={{ backgroundColor: 'rgba(244, 63, 94, 0.15)', color: '#be123c' }}
                  >
                    Discards
                  </span>
                )}
                {issueReasons.includes('carrier_transitions') && (
                  <span
                    className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                    style={{ backgroundColor: 'rgba(249, 115, 22, 0.15)', color: '#c2410c' }}
                  >
                    Link Flapping
                  </span>
                )}
                {issueReasons.includes('drained') && (
                  <span
                    className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                    style={{ backgroundColor: 'rgba(100, 116, 139, 0.15)', color: '#475569' }}
                  >
                    Drained
                  </span>
                )}
              </div>
            )}
          </div>

          {/* Timeline */}
          <div className="flex-1 min-w-0">
            <DeviceStatusTimeline
              hours={device.hours}
              bucketMinutes={bucketMinutes}
              timeRange={dataTimeRange}
            />
          </div>
        </div>
      </div>

      {/* Expanded content with chart */}
      {expanded && hasIssues && (
        <div className="px-4 pb-4 pt-0">
          <div className="flex items-stretch gap-4">
            {/* Spacer for chevron */}
            <div className="flex-shrink-0 w-5" />
            {/* Chart component with controls on left, chart aligned with timeline */}
            <InterfaceIssueChart
              devicePk={device.pk}
              timeRange={timeRange}
              buckets={buckets}
              controlsWidth="w-44"
            />
          </div>
        </div>
      )}
    </div>
  )
}

export function DeviceStatusTimelines({
  timeRange = '24h',
  onTimeRangeChange,
  issueFilters = ['interface_errors', 'discards', 'carrier_transitions', 'drained'],
  healthFilters = ['healthy', 'degraded', 'unhealthy', 'disabled'],
  devicesWithIssues,
  devicesWithHealth,
}: DeviceStatusTimelinesProps) {
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
    queryKey: ['device-history', timeRange, buckets],
    queryFn: () => fetchDeviceHistory(timeRange, buckets),
    refetchInterval: 60_000, // Refresh every minute
    staleTime: 30_000,
  })

  // Helper to check if a device matches health filters
  const deviceMatchesHealthFilters = (device: DeviceHistory): boolean => {
    if (devicesWithHealth) {
      const health = devicesWithHealth.get(device.code)
      if (health) {
        const filterHealth = health === 'no_data' ? 'unhealthy' : health
        return healthFilters.includes(filterHealth as any)
      }
      return false
    }

    // Fallback: check device's own hours data
    if (!device.hours || device.hours.length === 0) return false
    return device.hours.some(hour => {
      const status = hour.status
      if (status === 'healthy' && healthFilters.includes('healthy')) return true
      if (status === 'degraded' && healthFilters.includes('degraded')) return true
      if (status === 'unhealthy' && healthFilters.includes('unhealthy')) return true
      if (status === 'disabled' && healthFilters.includes('disabled')) return true
      if (status === 'no_data' && healthFilters.includes('unhealthy')) return true
      return false
    })
  }

  // Check which issue filters are selected
  const issueTypesSelected = issueFilters.filter(f => f !== 'no_issues')
  const noIssuesSelected = issueFilters.includes('no_issues')

  // Filter and sort devices by recency of issues
  const filteredDevices = useMemo(() => {
    if (!data?.devices) return []

    const filtered = data.devices.filter(device => {
      const issueReasons = devicesWithIssues
        ? (devicesWithIssues.get(device.code) ?? [])
        : (device.issue_reasons ?? [])
      const hasIssues = issueReasons.length > 0

      let matchesIssue = false
      if (hasIssues) {
        matchesIssue = issueReasons.some(reason => issueTypesSelected.includes(reason))
      } else {
        matchesIssue = noIssuesSelected
      }

      const matchesHealth = deviceMatchesHealthFilters(device)

      return matchesIssue && matchesHealth
    })

    // Sort by most recent issue
    return filtered.sort((a, b) => {
      const getLatestIssueIndex = (device: DeviceHistory): number => {
        if (!device.hours) return -1
        for (let i = device.hours.length - 1; i >= 0; i--) {
          const status = device.hours[i].status
          if (status === 'unhealthy' || status === 'degraded' || status === 'disabled') {
            return i
          }
        }
        return -1
      }

      const aIndex = getLatestIssueIndex(a)
      const bIndex = getLatestIssueIndex(b)

      return bIndex - aIndex
    })
  }, [data?.devices, issueFilters, healthFilters, noIssuesSelected, issueTypesSelected, devicesWithIssues, devicesWithHealth])

  if (isLoading) {
    return (
      <div className="border border-border rounded-lg p-6 flex items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground mr-2" />
        <span className="text-sm text-muted-foreground">Loading device history...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <AlertTriangle className="h-8 w-8 text-amber-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">Unable to load device history</div>
      </div>
    )
  }

  if (filteredDevices.length === 0) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <CheckCircle2 className="h-8 w-8 text-green-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">
          {data?.devices.length === 0
            ? 'No devices available in the selected time range'
            : 'No devices match the selected filters'}
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg">
      <div className="px-4 py-2.5 bg-muted/50 border-b border-border flex items-center gap-2 rounded-t-lg">
        <History className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">
          Device Status History
          <span className="text-sm text-muted-foreground font-normal ml-1">
            ({filteredDevices.length} device{filteredDevices.length !== 1 ? 's' : ''})
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
        {filteredDevices.map((device) => (
          <DeviceRow
            key={device.code}
            device={device}
            devicesWithIssues={devicesWithIssues}
            bucketMinutes={data?.bucket_minutes}
            dataTimeRange={data?.time_range}
            buckets={buckets}
            timeRange={timeRange}
          />
        ))}
      </div>
    </div>
  )
}
