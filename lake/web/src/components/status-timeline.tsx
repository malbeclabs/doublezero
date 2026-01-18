import { useState } from 'react'
import type { LinkHourStatus } from '@/lib/api'

interface StatusTimelineProps {
  hours: LinkHourStatus[]
  committedRttUs?: number
  bucketMinutes?: number
  timeRange?: string
}

function formatLatency(us: number): string {
  if (us >= 1000) {
    return `${(us / 1000).toFixed(1)}ms`
  }
  return `${us.toFixed(0)}us`
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
  // If the bucket spans multiple days, show both dates
  if (start.getDate() !== end.getDate()) {
    return `${formatDate(isoString)} ${startTime} — ${formatDate(end.toISOString())} ${endTime}`
  }
  return `${formatDate(isoString)} ${startTime} — ${endTime}`
}

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

// Thresholds matching backend classification and methodology
const LOSS_MINOR_PCT = 0.1      // Minor: detectable but not impactful
const LOSS_MODERATE_PCT = 1.0   // Moderate: noticeable degradation
const LOSS_SEVERE_PCT = 10.0    // Severe: significant impact
const LOSS_EXTENDED_PCT = 95.0  // Extended: link effectively down
const LATENCY_WARNING_PCT = 20
const LATENCY_CRITICAL_PCT = 50

function getStatusReasons(hour: LinkHourStatus, committedRttUs?: number): string[] {
  const reasons: string[] = []

  if (hour.status === 'no_data') return reasons

  // Check packet loss (using severity terms from methodology)
  if (hour.avg_loss_pct >= LOSS_EXTENDED_PCT) {
    reasons.push('Extended packet loss (≥95%)')
  } else if (hour.avg_loss_pct >= LOSS_SEVERE_PCT) {
    reasons.push(`Severe packet loss (${hour.avg_loss_pct.toFixed(1)}%)`)
  } else if (hour.avg_loss_pct >= LOSS_MODERATE_PCT) {
    reasons.push(`Moderate packet loss (${hour.avg_loss_pct.toFixed(1)}%)`)
  } else if (hour.avg_loss_pct >= LOSS_MINOR_PCT) {
    reasons.push(`Minor packet loss (${hour.avg_loss_pct.toFixed(2)}%)`)
  }

  // Check latency (only if committed RTT is defined)
  if (committedRttUs && committedRttUs > 0 && hour.avg_latency_us > 0) {
    const latencyOveragePct = ((hour.avg_latency_us - committedRttUs) / committedRttUs) * 100
    if (latencyOveragePct >= LATENCY_CRITICAL_PCT) {
      reasons.push(`High latency (${latencyOveragePct.toFixed(0)}% over SLA)`)
    } else if (latencyOveragePct >= LATENCY_WARNING_PCT) {
      reasons.push(`Elevated latency (${latencyOveragePct.toFixed(0)}% over SLA)`)
    }
  }

  return reasons
}

export function StatusTimeline({ hours, committedRttUs, bucketMinutes = 60, timeRange = '24h' }: StatusTimelineProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)

  const timeLabels: Record<string, string> = {
    '1h': '1h ago',
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
        {hours.map((hour, index) => {
          return (
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
                  {/* Reasons */}
                  {(() => {
                    const reasons = getStatusReasons(hour, committedRttUs)
                    if (reasons.length === 0) return null
                    return (
                      <div className="text-xs text-muted-foreground mb-2 space-y-0.5">
                        {reasons.map((reason, i) => (
                          <div key={i}>• {reason}</div>
                        ))}
                      </div>
                    )
                  })()}
                  {hour.status !== 'no_data' && (
                    <div className="space-y-1 text-muted-foreground">
                      <div className="flex justify-between gap-4">
                        <span>Latency:</span>
                        <span className="font-mono">
                          {formatLatency(hour.avg_latency_us)}
                          {committedRttUs && committedRttUs > 0 && (
                            <span className="text-xs ml-1">
                              ({((hour.avg_latency_us - committedRttUs) / committedRttUs * 100).toFixed(0)}% vs SLA)
                            </span>
                          )}
                        </span>
                      </div>
                      <div className="flex justify-between gap-4">
                        <span>Loss:</span>
                        <span className="font-mono">{hour.avg_loss_pct.toFixed(2)}%</span>
                      </div>
                      <div className="flex justify-between gap-4">
                        <span>Samples:</span>
                        <span className="font-mono">{hour.samples.toLocaleString()}</span>
                      </div>
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
          )
        })}
      </div>

      {/* Time labels */}
      <div className="flex justify-between mt-1 text-[10px] text-muted-foreground">
        <span>{timeLabel}</span>
        <span>Now</span>
      </div>
    </div>
  )
}
