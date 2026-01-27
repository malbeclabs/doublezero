import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid, ReferenceLine } from 'recharts'
import { useTheme } from '@/hooks/use-theme'
import { fetchDeviceInterfaceHistory } from '@/lib/api'

interface InterfaceHealthChartsProps {
  devicePk: string
}

interface AggregatedHealthData {
  time: string
  errors: number
  discards: number
  carrierTransitions: number
}

function formatTime(hourStr: string): string {
  const date = new Date(hourStr)
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatCount(value: number): string {
  if (value >= 1e6) return `${(value / 1e6).toFixed(1)}M`
  if (value >= 1e3) return `${(value / 1e3).toFixed(1)}K`
  return value.toString()
}

export function InterfaceHealthCharts({ devicePk }: InterfaceHealthChartsProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  const { data: historyData, isLoading, error } = useQuery({
    queryKey: ['device-interface-health', devicePk],
    queryFn: () => fetchDeviceInterfaceHistory(devicePk, '24h', 24),
    refetchInterval: 60000,
    retry: false,
  })

  // Aggregate data across all interfaces
  const chartData = useMemo(() => {
    if (!historyData?.interfaces || historyData.interfaces.length === 0) return []

    // Build a map of hour -> aggregated metrics
    const hourMap = new Map<string, AggregatedHealthData>()

    for (const iface of historyData.interfaces) {
      for (const hour of iface.hours) {
        const existing = hourMap.get(hour.hour)
        if (existing) {
          existing.errors += (hour.in_errors || 0) + (hour.out_errors || 0)
          existing.discards += (hour.in_discards || 0) + (hour.out_discards || 0)
          existing.carrierTransitions += hour.carrier_transitions || 0
        } else {
          hourMap.set(hour.hour, {
            time: formatTime(hour.hour),
            errors: (hour.in_errors || 0) + (hour.out_errors || 0),
            discards: (hour.in_discards || 0) + (hour.out_discards || 0),
            carrierTransitions: hour.carrier_transitions || 0,
          })
        }
      }
    }

    // Sort by time and return
    return Array.from(hourMap.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([, data]) => data)
  }, [historyData])

  // Check if there's any data to show
  const hasAnyIssues = useMemo(() => {
    return chartData.some(d => d.errors > 0 || d.discards > 0 || d.carrierTransitions > 0)
  }, [chartData])

  const errorColor = isDark ? '#ef4444' : '#dc2626'
  const discardColor = isDark ? '#f59e0b' : '#d97706'
  const carrierColor = isDark ? '#8b5cf6' : '#7c3aed'

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Loading interface health data...
      </div>
    )
  }

  if (error) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Unable to load interface health data
      </div>
    )
  }

  if (!chartData || chartData.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        No interface health data available
      </div>
    )
  }

  if (!hasAnyIssues) {
    return (
      <div className="text-sm text-green-600 dark:text-green-400 text-center py-4">
        No interface issues in the last 24 hours
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        Interface Health (24h)
      </div>
      <div className="h-36">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={chartData} barCategoryGap="10%">
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
              tickFormatter={formatCount}
              width={35}
            />
            <ReferenceLine y={0} stroke="var(--border)" />
            <RechartsTooltip
              contentStyle={{
                backgroundColor: 'var(--card)',
                border: '1px solid var(--border)',
                borderRadius: '6px',
                fontSize: '11px',
              }}
              formatter={(value: number | string | undefined, name?: string) => {
                const labels: Record<string, string> = {
                  errors: 'Errors',
                  discards: 'Discards',
                  carrierTransitions: 'Carrier Transitions',
                }
                const numericValue = typeof value === 'number' ? value : Number(value ?? 0)
                const label = name ? (labels[name] || name) : ''
                return [numericValue.toLocaleString(), label]
              }}
            />
            <Bar
              dataKey="errors"
              fill={errorColor}
              radius={[2, 2, 0, 0]}
              name="errors"
            />
            <Bar
              dataKey="discards"
              fill={discardColor}
              radius={[2, 2, 0, 0]}
              name="discards"
            />
            <Bar
              dataKey="carrierTransitions"
              fill={carrierColor}
              radius={[2, 2, 0, 0]}
              name="carrierTransitions"
            />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className="flex justify-center gap-4 text-xs">
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm" style={{ backgroundColor: errorColor }} />
          Errors
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm" style={{ backgroundColor: discardColor }} />
          Discards
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm" style={{ backgroundColor: carrierColor }} />
          Carrier Transitions
        </span>
      </div>
    </div>
  )
}
