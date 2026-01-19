import { useQuery } from '@tanstack/react-query'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useTheme } from '@/hooks/use-theme'
import { fetchTrafficHistory, formatChartAxisRate, formatChartTooltipRate } from './utils'

interface TrafficChartsProps {
  entityType: 'link' | 'device' | 'validator'
  entityPk: string
}

export function TrafficCharts({ entityType, entityPk }: TrafficChartsProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  const { data: trafficData, isLoading } = useQuery({
    queryKey: ['topology-traffic', entityType, entityPk],
    queryFn: () => fetchTrafficHistory(entityType, entityPk),
    refetchInterval: 60000,
  })

  const chartColor = isDark ? '#60a5fa' : '#2563eb'
  const chartColorSecondary = isDark ? '#f97316' : '#ea580c'

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        Loading traffic data...
      </div>
    )
  }

  if (!trafficData || trafficData.length === 0) {
    return (
      <div className="text-sm text-muted-foreground text-center py-4">
        No traffic data available
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Average Traffic Chart */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Avg Traffic Rate (24h)
        </div>
        <div className="h-36">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={trafficData}>
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
                tickFormatter={(v) => formatChartAxisRate(v)}
                width={40}
              />
              <RechartsTooltip
                contentStyle={{
                  backgroundColor: 'var(--card)',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '11px',
                }}
                formatter={(value) => formatChartTooltipRate(value as number)}
              />
              <Line
                type="monotone"
                dataKey="avgIn"
                stroke={chartColor}
                strokeWidth={1.5}
                dot={false}
                name="In"
              />
              <Line
                type="monotone"
                dataKey="avgOut"
                stroke={chartColorSecondary}
                strokeWidth={1.5}
                dot={false}
                name="Out"
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-4 text-xs mt-1">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColor }} />
            In
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColorSecondary }} />
            Out
          </span>
        </div>
      </div>

      {/* Peak Traffic Chart */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Peak Traffic Rate (24h)
        </div>
        <div className="h-36">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={trafficData}>
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
                tickFormatter={(v) => formatChartAxisRate(v)}
                width={40}
              />
              <RechartsTooltip
                contentStyle={{
                  backgroundColor: 'var(--card)',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '11px',
                }}
                formatter={(value) => formatChartTooltipRate(value as number)}
              />
              <Line
                type="monotone"
                dataKey="peakIn"
                stroke={chartColor}
                strokeWidth={1.5}
                dot={false}
                name="In"
              />
              <Line
                type="monotone"
                dataKey="peakOut"
                stroke={chartColorSecondary}
                strokeWidth={1.5}
                dot={false}
                name="Out"
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-4 text-xs mt-1">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColor }} />
            In
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: chartColorSecondary }} />
            Out
          </span>
        </div>
      </div>
    </div>
  )
}
