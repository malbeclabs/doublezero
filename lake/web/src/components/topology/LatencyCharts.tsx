import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LineChart, Line, XAxis, YAxis, ResponsiveContainer, Tooltip as RechartsTooltip, CartesianGrid } from 'recharts'
import { useTheme } from '@/hooks/use-theme'
import { fetchLatencyHistory, type TimeRange, type TimeRangePreset } from './utils'

interface LatencyChartsProps {
  linkPk: string
}

const TIME_RANGE_OPTIONS: { value: TimeRangePreset; label: string }[] = [
  { value: '15m', label: '15 min' },
  { value: '30m', label: '30 min' },
  { value: '1h', label: '1 hour' },
  { value: '3h', label: '3 hours' },
  { value: '6h', label: '6 hours' },
  { value: '12h', label: '12 hours' },
  { value: '24h', label: '24 hours' },
  { value: '2d', label: '2 days' },
  { value: '7d', label: '7 days' },
  { value: 'custom', label: 'Custom' },
]

// Legend item component with click to toggle
function LegendItem({
  color,
  label,
  active,
  onClick,
  dashed = false
}: {
  color: string
  label: string
  active: boolean
  onClick: () => void
  dashed?: boolean
}) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-1 px-1.5 py-0.5 rounded transition-opacity ${
        active ? 'opacity-100' : 'opacity-40'
      } hover:bg-[var(--muted)]/50`}
    >
      {dashed ? (
        <span
          className="w-3 h-0.5"
          style={{
            backgroundColor: color,
            backgroundImage: `repeating-linear-gradient(90deg, ${color} 0, ${color} 2px, transparent 2px, transparent 4px)`,
            backgroundSize: '4px 1px',
          }}
        />
      ) : (
        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: color }} />
      )}
      {label}
    </button>
  )
}

// Time range selector component
function TimeRangeSelector({
  value,
  onChange,
}: {
  value: TimeRange
  onChange: (range: TimeRange) => void
}) {
  const [showCustom, setShowCustom] = useState(value.preset === 'custom')
  const [customFrom, setCustomFrom] = useState(value.from || '')
  const [customTo, setCustomTo] = useState(value.to || '')

  const handlePresetChange = (preset: TimeRangePreset) => {
    if (preset === 'custom') {
      setShowCustom(true)
    } else {
      setShowCustom(false)
      onChange({ preset })
    }
  }

  const handleApplyCustom = () => {
    if (customFrom && customTo) {
      onChange({ preset: 'custom', from: customFrom, to: customTo })
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1">
        {TIME_RANGE_OPTIONS.map((opt) => (
          <button
            key={opt.value}
            onClick={() => handlePresetChange(opt.value)}
            className={`px-2 py-0.5 text-xs rounded border transition-colors ${
              value.preset === opt.value
                ? 'bg-[var(--primary)] text-[var(--primary-foreground)] border-[var(--primary)]'
                : 'border-[var(--border)] hover:bg-[var(--muted)]/50'
            }`}
          >
            {opt.label}
          </button>
        ))}
      </div>
      {showCustom && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <div className="flex items-center gap-1">
            <span className="text-muted-foreground">From:</span>
            <input
              type="text"
              placeholder="yyyy-mm-dd-hh:mm:ss"
              value={customFrom}
              onChange={(e) => setCustomFrom(e.target.value)}
              className="px-2 py-1 rounded border border-[var(--border)] bg-transparent w-40 font-mono text-xs"
            />
          </div>
          <div className="flex items-center gap-1">
            <span className="text-muted-foreground">To:</span>
            <input
              type="text"
              placeholder="yyyy-mm-dd-hh:mm:ss"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              className="px-2 py-1 rounded border border-[var(--border)] bg-transparent w-40 font-mono text-xs"
            />
          </div>
          <button
            onClick={handleApplyCustom}
            disabled={!customFrom || !customTo}
            className="px-2 py-1 text-xs rounded bg-[var(--primary)] text-[var(--primary-foreground)] disabled:opacity-50"
          >
            Apply
          </button>
        </div>
      )}
    </div>
  )
}

export function LatencyCharts({ linkPk }: LatencyChartsProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  // State for time range
  const [timeRange, setTimeRange] = useState<TimeRange>({ preset: '24h' })

  // State for visible lines
  const [rttLines, setRttLines] = useState({
    avgA: true,
    p95A: true,
    avgZ: true,
    p95Z: true,
    avg: true,
    p95: true,
  })
  const [jitterLines, setJitterLines] = useState({
    a: true,
    z: true,
    avg: true,
  })

  const { data: latencyData, isLoading } = useQuery({
    queryKey: ['topology-latency', linkPk, timeRange],
    queryFn: () => fetchLatencyHistory(linkPk, timeRange),
    refetchInterval: 60000,
  })

  // Colors for per-direction data
  const rttAAvgColor = isDark ? '#22c55e' : '#16a34a' // green for A avg
  const rttAP95Color = isDark ? '#86efac' : '#4ade80' // light green for A p95
  const rttZAvgColor = isDark ? '#3b82f6' : '#2563eb' // blue for Z avg
  const rttZP95Color = isDark ? '#93c5fd' : '#60a5fa' // light blue for Z p95
  const jitterAColor = isDark ? '#a855f7' : '#9333ea' // purple for A
  const jitterZColor = isDark ? '#f97316' : '#ea580c' // orange for Z

  // Check if we have per-direction data
  const hasDirectionalData = latencyData?.some(
    (d) => (d.avgRttAtoZMs && d.avgRttAtoZMs > 0) || (d.avgRttZtoAMs && d.avgRttZtoAMs > 0)
  ) ?? false

  // Get time range label for chart title
  const getTimeRangeLabel = () => {
    if (timeRange.preset === 'custom') {
      return 'Custom Range'
    }
    const opt = TIME_RANGE_OPTIONS.find(o => o.value === timeRange.preset)
    return opt?.label || '24 hours'
  }

  return (
    <div className="space-y-4">
      {/* Time range selector */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Time Range
        </div>
        <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
      </div>

      {isLoading ? (
        <div className="text-sm text-muted-foreground text-center py-4">
          Loading latency data...
        </div>
      ) : !latencyData || latencyData.length === 0 ? (
        <div className="text-sm text-muted-foreground text-center py-4">
          No latency data available for this time range
        </div>
      ) : (
        <>
      {/* RTT Chart - Per Direction */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Round-Trip Time by Direction ({getTimeRangeLabel()})
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
                formatter={(value, name) => {
                  const labels: Record<string, string> = {
                    avgRttAtoZMs: 'Avg from A',
                    p95RttAtoZMs: 'P95 from A',
                    avgRttZtoAMs: 'Avg from Z',
                    p95RttZtoAMs: 'P95 from Z',
                    avgRttMs: 'Avg',
                    p95RttMs: 'P95',
                  }
                  return [`${Number(value ?? 0).toFixed(2)} ms`, labels[name ?? ''] || name || '']
                }}
              />
              {hasDirectionalData ? (
                <>
                  {rttLines.avgA && (
                    <Line
                      type="monotone"
                      dataKey="avgRttAtoZMs"
                      stroke={rttAAvgColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="avgRttAtoZMs"
                    />
                  )}
                  {rttLines.p95A && (
                    <Line
                      type="monotone"
                      dataKey="p95RttAtoZMs"
                      stroke={rttAP95Color}
                      strokeWidth={1.5}
                      strokeDasharray="4 2"
                      dot={false}
                      name="p95RttAtoZMs"
                    />
                  )}
                  {rttLines.avgZ && (
                    <Line
                      type="monotone"
                      dataKey="avgRttZtoAMs"
                      stroke={rttZAvgColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="avgRttZtoAMs"
                    />
                  )}
                  {rttLines.p95Z && (
                    <Line
                      type="monotone"
                      dataKey="p95RttZtoAMs"
                      stroke={rttZP95Color}
                      strokeWidth={1.5}
                      strokeDasharray="4 2"
                      dot={false}
                      name="p95RttZtoAMs"
                    />
                  )}
                </>
              ) : (
                <>
                  {rttLines.avg && (
                    <Line
                      type="monotone"
                      dataKey="avgRttMs"
                      stroke={rttAAvgColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="avgRttMs"
                    />
                  )}
                  {rttLines.p95 && (
                    <Line
                      type="monotone"
                      dataKey="p95RttMs"
                      stroke={rttAP95Color}
                      strokeWidth={1.5}
                      strokeDasharray="4 2"
                      dot={false}
                      name="p95RttMs"
                    />
                  )}
                </>
              )}
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-1 text-xs mt-1 flex-wrap">
          {hasDirectionalData ? (
            <>
              <LegendItem
                color={rttAAvgColor}
                label="Avg A"
                active={rttLines.avgA}
                onClick={() => setRttLines(s => ({ ...s, avgA: !s.avgA }))}
              />
              <LegendItem
                color={rttAP95Color}
                label="P95 A"
                active={rttLines.p95A}
                onClick={() => setRttLines(s => ({ ...s, p95A: !s.p95A }))}
                dashed
              />
              <LegendItem
                color={rttZAvgColor}
                label="Avg Z"
                active={rttLines.avgZ}
                onClick={() => setRttLines(s => ({ ...s, avgZ: !s.avgZ }))}
              />
              <LegendItem
                color={rttZP95Color}
                label="P95 Z"
                active={rttLines.p95Z}
                onClick={() => setRttLines(s => ({ ...s, p95Z: !s.p95Z }))}
                dashed
              />
            </>
          ) : (
            <>
              <LegendItem
                color={rttAAvgColor}
                label="Avg"
                active={rttLines.avg}
                onClick={() => setRttLines(s => ({ ...s, avg: !s.avg }))}
              />
              <LegendItem
                color={rttAP95Color}
                label="P95"
                active={rttLines.p95}
                onClick={() => setRttLines(s => ({ ...s, p95: !s.p95 }))}
                dashed
              />
            </>
          )}
        </div>
      </div>

      {/* Jitter Chart - Per Direction */}
      <div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
          Jitter by Direction ({getTimeRangeLabel()})
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
                formatter={(value, name) => {
                  const label = name === 'jitterAtoZMs' ? 'From A' : name === 'jitterZtoAMs' ? 'From Z' : 'Jitter'
                  return [`${Number(value ?? 0).toFixed(2)} ms`, label]
                }}
              />
              {hasDirectionalData ? (
                <>
                  {jitterLines.a && (
                    <Line
                      type="monotone"
                      dataKey="jitterAtoZMs"
                      stroke={jitterAColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="jitterAtoZMs"
                    />
                  )}
                  {jitterLines.z && (
                    <Line
                      type="monotone"
                      dataKey="jitterZtoAMs"
                      stroke={jitterZColor}
                      strokeWidth={1.5}
                      dot={false}
                      name="jitterZtoAMs"
                    />
                  )}
                </>
              ) : (
                jitterLines.avg && (
                  <Line
                    type="monotone"
                    dataKey="avgJitter"
                    stroke={jitterAColor}
                    strokeWidth={1.5}
                    dot={false}
                    name="Jitter"
                  />
                )
              )}
            </LineChart>
          </ResponsiveContainer>
        </div>
        <div className="flex justify-center gap-1 text-xs mt-1">
          {hasDirectionalData ? (
            <>
              <LegendItem
                color={jitterAColor}
                label="From A"
                active={jitterLines.a}
                onClick={() => setJitterLines(s => ({ ...s, a: !s.a }))}
              />
              <LegendItem
                color={jitterZColor}
                label="From Z"
                active={jitterLines.z}
                onClick={() => setJitterLines(s => ({ ...s, z: !s.z }))}
              />
            </>
          ) : (
            <LegendItem
              color={jitterAColor}
              label="Avg Jitter"
              active={jitterLines.avg}
              onClick={() => setJitterLines(s => ({ ...s, avg: !s.avg }))}
            />
          )}
        </div>
      </div>
        </>
      )}
    </div>
  )
}
