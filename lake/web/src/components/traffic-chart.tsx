import { useState, useMemo, memo } from 'react'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
} from 'recharts'
import type { TrafficPoint, SeriesInfo } from '@/lib/api'

// Color palette
const COLORS = [
  'hsl(25, 95%, 53%)',   // accent orange
  'hsl(220, 70%, 50%)',  // blue
  'hsl(160, 60%, 45%)',  // green
  'hsl(280, 60%, 55%)',  // purple
  'hsl(340, 70%, 55%)',  // pink
  'hsl(45, 90%, 50%)',   // yellow
  'hsl(190, 70%, 45%)',  // cyan
  'hsl(10, 70%, 50%)',   // red-orange
]

interface TrafficChartProps {
  title: string
  data: TrafficPoint[]
  series: SeriesInfo[]
  stacked?: boolean
}

// Format bandwidth for display
function formatBandwidth(bps: number): string {
  if (bps >= 1e9) {
    return `${(bps / 1e9).toFixed(2)} Gib/s`
  } else if (bps >= 1e6) {
    return `${(bps / 1e6).toFixed(2)} Mib/s`
  } else if (bps >= 1e3) {
    return `${(bps / 1e3).toFixed(2)} Kib/s`
  }
  return `${bps.toFixed(2)} b/s`
}

// Format time for X-axis
function formatTime(timeStr: string, dataRange: number): string {
  const date = new Date(timeStr)
  if (isNaN(date.getTime())) return timeStr

  const oneDay = 86400000
  if (dataRange < oneDay) {
    // Less than 24h: show time
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } else {
    // More than 24h: show date
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }
}

function TrafficChartImpl({ title, data, series, stacked = false }: TrafficChartProps) {
  // Start with all series selected by default
  const [selectedSeries, setSelectedSeries] = useState<Set<string>>(new Set())
  const [lastClickedIndex, setLastClickedIndex] = useState<number | null>(null)

  // Transform data for Recharts
  const chartData = useMemo(() => {
    if (!data.length) return []

    // Group by timestamp
    const timeMap = new Map<string, Record<string, string | number>>()

    for (const point of data) {
      if (!timeMap.has(point.time)) {
        timeMap.set(point.time, { time: point.time })
      }
      const entry = timeMap.get(point.time)!
      const key = `${point.device}-${point.intf}`
      entry[`${key}-in`] = point.in_bps
      entry[`${key}-out`] = point.out_bps
    }

    return Array.from(timeMap.values()).sort((a, b) => (a.time as string).localeCompare(b.time as string))
  }, [data])

  // Calculate data range for time formatting
  const dataRange = useMemo(() => {
    if (chartData.length < 2) return 0
    const first = new Date(chartData[0].time).getTime()
    const last = new Date(chartData[chartData.length - 1].time).getTime()
    return last - first
  }, [chartData])

  // Handle series selection
  const handleSeriesClick = (seriesKey: string, index: number, event: React.MouseEvent) => {
    if (event.shiftKey && lastClickedIndex !== null) {
      // Shift+click: multi-select range
      const start = Math.min(lastClickedIndex, index)
      const end = Math.max(lastClickedIndex, index)
      const newSelection = new Set(selectedSeries)
      for (let i = start; i <= end; i++) {
        newSelection.add(series[i].key)
      }
      setSelectedSeries(newSelection)
    } else if (event.ctrlKey || event.metaKey) {
      // Ctrl/Cmd+click: toggle single
      const newSelection = new Set(selectedSeries)
      if (newSelection.has(seriesKey)) {
        newSelection.delete(seriesKey)
      } else {
        newSelection.add(seriesKey)
      }
      setSelectedSeries(newSelection)
    } else {
      // Normal click: select only this one
      setSelectedSeries(new Set([seriesKey]))
    }
    setLastClickedIndex(index)
  }

  // Get visible series (selected or all if none selected)
  const visibleSeries = useMemo(() => {
    if (selectedSeries.has('__none__')) {
      return new Set()
    }
    if (selectedSeries.size > 0) {
      return selectedSeries
    }
    // Default: show all series
    return new Set(series.map(s => s.key))
  }, [selectedSeries, series])

  const ChartComponent = stacked ? AreaChart : LineChart
  const SeriesComponent = stacked ? Area : Line

  // Only render visible series to improve performance
  const visibleSeriesList = useMemo(() => {
    return series.filter(s => visibleSeries.has(s.key))
  }, [series, visibleSeries])

  // Calculate tick interval to reduce label density
  const tickInterval = useMemo(() => {
    if (chartData.length <= 20) return 0 // Show all
    if (chartData.length <= 100) return Math.ceil(chartData.length / 20)
    return Math.ceil(chartData.length / 15) // Max ~15 labels for large datasets
  }, [chartData.length])

  // Hide dots for dense time series (performance optimization)
  const showDots = chartData.length <= 50

  if (!data.length || !series.length) {
    return (
      <div className="flex flex-col space-y-2">
        <h3 className="text-lg font-semibold">{title}</h3>
        <div className="border border-border rounded-lg p-8 flex items-center justify-center h-[400px]">
          <p className="text-muted-foreground">No data available</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col space-y-2">
      <h3 className="text-lg font-semibold">{title}</h3>

      {/* Chart */}
      <ResponsiveContainer width="100%" height={400}>
        <ChartComponent data={chartData}>
          <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
          <XAxis
            dataKey="time"
            tickFormatter={(value) => formatTime(value, dataRange)}
            className="text-xs"
            angle={dataRange < 86400000 ? 0 : -45}
            textAnchor={dataRange < 86400000 ? 'middle' : 'end'}
            height={dataRange < 86400000 ? 30 : 60}
            interval={tickInterval}
          />
          <YAxis
            tickFormatter={(value) => formatBandwidth(value)}
            className="text-xs"
            width={80}
          />
          <Tooltip
            isAnimationActive={false}
            content={({ active, payload }) => {
              if (!active || !payload || !payload.length) return null
              // Only show the hovered line, not all lines at this time
              const hoveredEntry = payload[0]
              if (!hoveredEntry || hoveredEntry.value === undefined) return null
              return (
                <div className="bg-background border border-border rounded-lg p-3 shadow-lg">
                  <p className="text-xs mb-1 text-muted-foreground">
                    {formatTime(hoveredEntry.payload.time, dataRange)}
                  </p>
                  <p className="text-sm font-medium" style={{ color: hoveredEntry.color }}>
                    {hoveredEntry.name}
                  </p>
                  <p className="text-sm">{formatBandwidth(hoveredEntry.value)}</p>
                </div>
              )
            }}
          />
          {visibleSeriesList.map((s) => {
            const dataKey = `${s.device}-${s.intf}-${s.direction}`
            const seriesIndex = series.indexOf(s)
            const color = COLORS[seriesIndex % COLORS.length]

            return (
              <SeriesComponent
                key={s.key}
                type="monotone"
                dataKey={dataKey}
                name={s.key}
                stroke={color}
                fill={color}
                strokeWidth={1.5}
                dot={showDots ? { r: 2 } : false}
                activeDot={showDots ? { r: 4 } : false}
                isAnimationActive={false}
                {...(stacked ? { stackId: "1" } : {})}
              />
            )
          })}
        </ChartComponent>
      </ResponsiveContainer>

      {/* Series selection list */}
      <div className="border border-border rounded-lg p-3 max-h-64 overflow-y-auto">
        <div className="flex items-center justify-between mb-2">
          <div className="text-sm font-medium">
            Series ({visibleSeriesList.length}/{series.length})
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => {
                // Show top 10 by mean
                const top10 = [...series]
                  .sort((a, b) => b.mean - a.mean)
                  .slice(0, 10)
                  .map(s => s.key)
                setSelectedSeries(new Set(top10))
              }}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              Top 10
            </button>
            <button
              onClick={() => {
                // Show all series
                setSelectedSeries(new Set(series.map(s => s.key)))
              }}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              All
            </button>
            <button
              onClick={() => setSelectedSeries(new Set(['__none__']))}
              className="text-xs text-muted-foreground hover:text-foreground"
            >
              None
            </button>
          </div>
        </div>
        <div className="space-y-1">
          {series.map((s, i) => {
            const isSelected = selectedSeries.has(s.key)
            const color = COLORS[i % COLORS.length]
            return (
              <div
                key={s.key}
                className={`flex items-center justify-between px-2 py-1 rounded cursor-pointer hover:bg-muted transition-colors ${
                  isSelected ? 'bg-muted' : ''
                }`}
                onClick={(e) => handleSeriesClick(s.key, i, e)}
              >
                <div className="flex items-center space-x-2">
                  <div
                    className="w-3 h-3 rounded-sm"
                    style={{ backgroundColor: color }}
                  />
                  <span className="text-sm">{s.key}</span>
                </div>
                <span className="text-sm text-muted-foreground">
                  {formatBandwidth(s.mean)}
                </span>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// Memoize component to prevent unnecessary re-renders
export const TrafficChart = memo(TrafficChartImpl)
