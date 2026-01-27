import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid, ReferenceLine } from 'recharts'
import { useTheme } from '@/hooks/use-theme'
import { fetchSingleLinkHistory } from '@/lib/api'
import type { LinkHourStatus } from '@/lib/api'

interface LinkStatusChartsProps {
  linkPk: string
}

function formatTime(hourStr: string): string {
  const date = new Date(hourStr)
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// Check if there's any packet loss data
function hasPacketLossData(hours: LinkHourStatus[]): boolean {
  return hours.some(h => h.avg_loss_pct > 0)
}

// Check if there's any interface issue data
function hasInterfaceIssueData(hours: LinkHourStatus[]): boolean {
  return hours.some(h =>
    (h.side_a_in_errors ?? 0) > 0 || (h.side_a_out_errors ?? 0) > 0 ||
    (h.side_z_in_errors ?? 0) > 0 || (h.side_z_out_errors ?? 0) > 0 ||
    (h.side_a_in_discards ?? 0) > 0 || (h.side_a_out_discards ?? 0) > 0 ||
    (h.side_z_in_discards ?? 0) > 0 || (h.side_z_out_discards ?? 0) > 0 ||
    (h.side_a_carrier_transitions ?? 0) > 0 || (h.side_z_carrier_transitions ?? 0) > 0
  )
}

export function LinkStatusCharts({ linkPk }: LinkStatusChartsProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  const { data: historyData, isLoading, error } = useQuery({
    queryKey: ['single-link-history', linkPk],
    queryFn: () => fetchSingleLinkHistory(linkPk, '24h', 24),
    refetchInterval: 60000,
    retry: false,
  })

  // Prepare packet loss chart data
  const packetLossData = useMemo(() => {
    if (!historyData?.hours) return []
    return historyData.hours.map(h => ({
      time: formatTime(h.hour),
      fullTime: h.hour,
      total: h.avg_loss_pct,
      sideA: h.side_a_loss_pct ?? 0,
      sideZ: h.side_z_loss_pct ?? 0,
    }))
  }, [historyData])

  // Prepare interface issues chart data
  const interfaceIssuesData = useMemo(() => {
    if (!historyData?.hours) return []
    return historyData.hours.map(h => ({
      time: formatTime(h.hour),
      fullTime: h.hour,
      errors: (h.side_a_in_errors ?? 0) + (h.side_a_out_errors ?? 0) +
              (h.side_z_in_errors ?? 0) + (h.side_z_out_errors ?? 0),
      discards: (h.side_a_in_discards ?? 0) + (h.side_a_out_discards ?? 0) +
                (h.side_z_in_discards ?? 0) + (h.side_z_out_discards ?? 0),
      carrier: (h.side_a_carrier_transitions ?? 0) + (h.side_z_carrier_transitions ?? 0),
    }))
  }, [historyData])

  const showPacketLoss = historyData?.hours && hasPacketLossData(historyData.hours)
  const showInterfaceIssues = historyData?.hours && hasInterfaceIssueData(historyData.hours)

  // Colors
  const lossColor = isDark ? '#a855f7' : '#9333ea' // purple
  const errorColor = isDark ? '#ef4444' : '#dc2626' // red
  const discardColor = isDark ? '#f59e0b' : '#d97706' // amber
  const carrierColor = isDark ? '#8b5cf6' : '#7c3aed' // violet

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Loading link status data...
      </div>
    )
  }

  if (error) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Unable to load link status data
      </div>
    )
  }

  if (!historyData?.hours || historyData.hours.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        No link status data available
      </div>
    )
  }

  if (!showPacketLoss && !showInterfaceIssues) {
    return (
      <div className="text-sm text-green-600 dark:text-green-400 text-center py-4">
        No packet loss or interface issues in the last 24 hours
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Packet Loss Chart */}
      {showPacketLoss && (
        <div>
          <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
            Packet Loss (24h)
          </div>
          <div className="h-36">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={packetLossData}>
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
                  tickFormatter={(v) => `${v.toFixed(1)}%`}
                  width={40}
                  domain={[0, 'auto']}
                />
                <RechartsTooltip
                  contentStyle={{
                    backgroundColor: 'var(--card)',
                    border: '1px solid var(--border)',
                    borderRadius: '6px',
                    fontSize: '11px',
                  }}
                  formatter={(value: number | string | undefined, name?: string) => {
                    const labels: Record<string, string> = {
                      total: 'Average',
                      sideA: 'Side A',
                      sideZ: 'Side Z',
                    }
                    const numericValue = typeof value === 'number' ? value : Number(value ?? 0)
                    const label = name ? (labels[name] || name) : ''
                    return [`${numericValue.toFixed(2)}%`, label]
                  }}
                />
                <Line
                  type="monotone"
                  dataKey="total"
                  stroke={lossColor}
                  strokeWidth={2}
                  dot={false}
                  name="total"
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <div className="flex justify-center gap-4 text-xs mt-1">
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: lossColor }} />
              Packet Loss
            </span>
          </div>
        </div>
      )}

      {/* Interface Issues Chart */}
      {showInterfaceIssues && (
        <div>
          <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
            Interface Issues (24h)
          </div>
          <div className="h-36">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={interfaceIssuesData} barCategoryGap="10%">
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
                  tickFormatter={(v) => {
                    if (v >= 1e6) return `${(v / 1e6).toFixed(1)}M`
                    if (v >= 1e3) return `${(v / 1e3).toFixed(1)}K`
                    return v.toString()
                  }}
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
                      carrier: 'Carrier Transitions',
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
                  dataKey="carrier"
                  fill={carrierColor}
                  radius={[2, 2, 0, 0]}
                  name="carrier"
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
          <div className="flex justify-center gap-4 text-xs mt-1">
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
              Carrier
            </span>
          </div>
        </div>
      )}
    </div>
  )
}
