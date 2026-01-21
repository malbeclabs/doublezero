import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, CheckCircle2, AlertTriangle, History, Info } from 'lucide-react'
import { fetchDeviceHistory } from '@/lib/api'
import type { DeviceHistory, DeviceHourStatus } from '@/lib/api'

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

      <div className="divide-y divide-border">
        {filteredDevices.map((device) => (
          <div key={device.code} className="px-4 py-3 hover:bg-muted/30 transition-colors">
            <div className="flex items-start gap-4">
              {/* Device info */}
              <div className="flex-shrink-0 w-48">
                <div className="flex items-center gap-1.5">
                  <Link to={`/dz/devices/${device.pk}`} className="font-mono text-sm truncate hover:underline" title={device.code}>
                    {device.code}
                  </Link>
                  <DeviceInfoPopover device={device} />
                </div>
                {device.contributor && (
                  <div className="text-xs text-muted-foreground">{device.contributor}</div>
                )}
                {(() => {
                  const issueReasons = devicesWithIssues
                    ? (devicesWithIssues.get(device.code) ?? [])
                    : (device.issue_reasons ?? [])
                  return issueReasons.length > 0 && (
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
                  )
                })()}
              </div>

              {/* Timeline */}
              <div className="flex-1 min-w-0">
                <DeviceStatusTimeline
                  hours={device.hours}
                  bucketMinutes={data?.bucket_minutes}
                  timeRange={data?.time_range}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
