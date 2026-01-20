import { useQuery } from '@tanstack/react-query'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useTheme } from '@/hooks/use-theme'
import { fetchLatencyHistory } from './utils'

interface LatencyChartsProps {
  linkPk: string
}

export function LatencyCharts({ linkPk }: LatencyChartsProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  const { data: latencyData, isLoading } = useQuery({
    queryKey: ['topology-latency', linkPk],
    queryFn: () => fetchLatencyHistory(linkPk),
    refetchInterval: 60000,
  })

  const rttColor = isDark ? '#22c55e' : '#16a34a' // green
  const rttP95Color = isDark ? '#eab308' : '#ca8a04' // yellow
  const jitterColor = isDark ? '#a855f7' : '#9333ea' // purple

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Loading latency data...
      </div>
    )
  }

  if (!latencyData || latencyData.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        No latency data available
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* RTT Chart */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Round-Trip Time (24h)
        </div>
        <div className="h-36">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={latencyData}>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
              <XAxis
                dataKey="time"
                tick={{ fontSize: 9 }}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                tick={{ fontSize: 9 }}
                tickLine={false}
                axisLine={false}
                tickFormatter={(v) => `${v.toFixed(1)}`}
                width={35}
                unit="ms"
              />
              <RechartsTooltip
                contentStyle={{
                  backgroundColor: 'var(--card)',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '11px',
                }}
                formatter={(value) => [
                  `${(value as number).toFixed(2)} ms`,
                  ''
                ]}
              />
              <Line
                type="monotone"
                dataKey="avgRttMs"
                stroke={rttColor}
                strokeWidth={1.5}
                dot={false}
                name="avgRttMs"
              />
              <Line
                type="monotone"
                dataKey="p95RttMs"
                stroke={rttP95Color}
                strokeWidth={1.5}
                dot={false}
                name="p95RttMs"
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-4 text-xs mt-1">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: rttColor }} />
            Avg
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: rttP95Color }} />
            P95
          </span>
        </div>
      </div>

      {/* Jitter Chart */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Jitter (24h)
        </div>
        <div className="h-36">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={latencyData}>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.5} />
              <XAxis
                dataKey="time"
                tick={{ fontSize: 9 }}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                tick={{ fontSize: 9 }}
                tickLine={false}
                axisLine={false}
                tickFormatter={(v) => `${v.toFixed(1)}`}
                width={35}
                unit="ms"
              />
              <RechartsTooltip
                contentStyle={{
                  backgroundColor: 'var(--card)',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '11px',
                }}
                formatter={(value) => [`${(value as number).toFixed(2)} ms`, 'Jitter']}
              />
              <Line
                type="monotone"
                dataKey="avgJitter"
                stroke={jitterColor}
                strokeWidth={1.5}
                dot={false}
                name="Jitter"
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-4 text-xs mt-1">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: jitterColor }} />
            Avg Jitter
          </span>
        </div>
      </div>
    </div>
  )
}
