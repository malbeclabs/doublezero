import { useMemo } from 'react'
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  LineChart,
  Line,
  PieChart,
  Pie,
  Cell,
  AreaChart,
  Area,
  ScatterChart,
  Scatter,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts'
import type { QueryResponse } from '@/lib/api'
import type { VisualizationConfig } from '@/lib/visualization'
import { transformDataForChart, transformDataForPieChart } from '@/lib/visualization'

// Check if a value looks like a date/timestamp
function isDateLike(value: unknown): boolean {
  if (typeof value === 'string') {
    // ISO date patterns
    if (/^\d{4}-\d{2}-\d{2}/.test(value)) return true
    // Try parsing
    const parsed = Date.parse(value)
    return !isNaN(parsed)
  }
  if (typeof value === 'number' && value > 1000000000000) {
    // Looks like a millisecond timestamp
    return true
  }
  return false
}

// Format date for display based on data range
function formatDateTick(value: unknown, dataRange: { min: number; max: number }): string {
  let date: Date
  if (typeof value === 'number') {
    date = new Date(value)
  } else if (typeof value === 'string') {
    date = new Date(value)
  } else {
    return String(value)
  }

  if (isNaN(date.getTime())) return String(value)

  const rangeMs = dataRange.max - dataRange.min
  const oneDay = 86400000
  const oneMonth = 30 * oneDay

  // Format based on data range
  if (rangeMs < oneDay) {
    // Less than a day: show time
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } else if (rangeMs < oneMonth) {
    // Less than a month: show date and time
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' })
  } else {
    // Longer: show month and year
    return date.toLocaleDateString([], { month: 'short', year: '2-digit' })
  }
}

// Calculate appropriate tick interval
function calculateTickInterval(dataLength: number, chartWidth: number = 800): number {
  const maxTicks = Math.floor(chartWidth / 80) // Roughly 80px per tick
  if (dataLength <= maxTicks) return 0 // Show all
  return Math.ceil(dataLength / maxTicks)
}

// Color palette matching the app's accent theme
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

interface ResultsChartProps {
  results: QueryResponse
  config: VisualizationConfig
}

export function ResultsChart({ results, config }: ResultsChartProps) {
  // Analyze data for time series detection
  const { data, isTimeSeries, dateRange, tickInterval } = useMemo(() => {
    if (!results.columns.length || !results.rows.length) {
      return { data: [], isTimeSeries: false, dateRange: { min: 0, max: 0 }, tickInterval: 0 }
    }

    const transformedData = config.chartType === 'pie'
      ? transformDataForPieChart(results.columns, results.rows, config)
      : transformDataForChart(results.columns, results.rows, config)

    // Check if X-axis is time series
    const xAxisIndex = results.columns.indexOf(config.xAxis)
    const firstValue = results.rows[0]?.[xAxisIndex]
    const isTime = isDateLike(firstValue)

    // Calculate date range for time series
    let range = { min: 0, max: 0 }
    if (isTime && results.rows.length > 0) {
      const timestamps = results.rows.map(row => {
        const val = row[xAxisIndex]
        return typeof val === 'number' ? val : new Date(String(val)).getTime()
      }).filter(t => !isNaN(t))

      if (timestamps.length > 0) {
        range = { min: Math.min(...timestamps), max: Math.max(...timestamps) }
      }
    }

    // Calculate tick interval
    const interval = calculateTickInterval(results.rows.length)

    return {
      data: transformedData,
      isTimeSeries: isTime,
      dateRange: range,
      tickInterval: interval,
    }
  }, [results, config])

  if (!results.columns.length || !results.rows.length) {
    return (
      <div className="flex items-center justify-center h-[400px] text-muted-foreground">
        No data to visualize
      </div>
    )
  }

  const { chartType } = config
  const dataLength = results.rows.length
  const showDots = dataLength <= 50 // Hide dots for dense time series

  const renderChart = () => {
    switch (chartType) {
      case 'bar':
        return (
          <BarChart data={data} margin={{ top: 20, right: 30, left: 20, bottom: 60 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(220, 13%, 91%)" />
            <XAxis
              dataKey={config.xAxis}
              tick={{ fontSize: 12, fill: 'hsl(220, 9%, 46%)' }}
              angle={-45}
              textAnchor="end"
              height={80}
            />
            <YAxis tick={{ fontSize: 12, fill: 'hsl(220, 9%, 46%)' }} />
            <Tooltip
              contentStyle={{
                backgroundColor: 'white',
                border: '1px solid hsl(220, 13%, 91%)',
                borderRadius: '8px',
                fontSize: '12px',
              }}
            />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {config.yAxis.map((yCol, index) => (
              <Bar
                key={yCol}
                dataKey={yCol}
                fill={COLORS[index % COLORS.length]}
                radius={[4, 4, 0, 0]}
              />
            ))}
          </BarChart>
        )

      case 'line':
        return (
          <LineChart data={data} margin={{ top: 20, right: 30, left: 20, bottom: isTimeSeries ? 40 : 60 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(220, 13%, 91%)" />
            <XAxis
              dataKey={config.xAxis}
              tick={{ fontSize: 11, fill: 'hsl(220, 9%, 46%)' }}
              angle={isTimeSeries ? 0 : -45}
              textAnchor={isTimeSeries ? 'middle' : 'end'}
              height={isTimeSeries ? 40 : 80}
              interval={tickInterval}
              tickFormatter={isTimeSeries ? (value) => formatDateTick(value, dateRange) : undefined}
            />
            <YAxis
              tick={{ fontSize: 11, fill: 'hsl(220, 9%, 46%)' }}
              tickFormatter={(value) => typeof value === 'number' ? value.toLocaleString() : value}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: 'white',
                border: '1px solid hsl(220, 13%, 91%)',
                borderRadius: '8px',
                fontSize: '12px',
              }}
              labelFormatter={isTimeSeries ? (label) => {
                const date = new Date(label)
                return isNaN(date.getTime()) ? label : date.toLocaleString()
              } : undefined}
            />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {config.yAxis.map((yCol, index) => (
              <Line
                key={yCol}
                type="monotone"
                dataKey={yCol}
                stroke={COLORS[index % COLORS.length]}
                strokeWidth={1.5}
                dot={showDots ? { r: 2 } : false}
                activeDot={{ r: 4 }}
              />
            ))}
          </LineChart>
        )

      case 'pie':
        return (
          <PieChart margin={{ top: 20, right: 30, left: 20, bottom: 20 }}>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              cx="50%"
              cy="50%"
              outerRadius={120}
              label={({ name, percent }) => `${name}: ${((percent ?? 0) * 100).toFixed(0)}%`}
              labelLine={{ stroke: 'hsl(220, 9%, 46%)' }}
            >
              {(data as { name: string; value: number }[]).map((_, index) => (
                <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
              ))}
            </Pie>
            <Tooltip
              contentStyle={{
                backgroundColor: 'white',
                border: '1px solid hsl(220, 13%, 91%)',
                borderRadius: '8px',
                fontSize: '12px',
              }}
            />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
          </PieChart>
        )

      case 'area':
        return (
          <AreaChart data={data} margin={{ top: 20, right: 30, left: 20, bottom: isTimeSeries ? 40 : 60 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(220, 13%, 91%)" />
            <XAxis
              dataKey={config.xAxis}
              tick={{ fontSize: 11, fill: 'hsl(220, 9%, 46%)' }}
              angle={isTimeSeries ? 0 : -45}
              textAnchor={isTimeSeries ? 'middle' : 'end'}
              height={isTimeSeries ? 40 : 80}
              interval={tickInterval}
              tickFormatter={isTimeSeries ? (value) => formatDateTick(value, dateRange) : undefined}
            />
            <YAxis
              tick={{ fontSize: 11, fill: 'hsl(220, 9%, 46%)' }}
              tickFormatter={(value) => typeof value === 'number' ? value.toLocaleString() : value}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: 'white',
                border: '1px solid hsl(220, 13%, 91%)',
                borderRadius: '8px',
                fontSize: '12px',
              }}
              labelFormatter={isTimeSeries ? (label) => {
                const date = new Date(label)
                return isNaN(date.getTime()) ? label : date.toLocaleString()
              } : undefined}
            />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {config.yAxis.map((yCol, index) => (
              <Area
                key={yCol}
                type="monotone"
                dataKey={yCol}
                stroke={COLORS[index % COLORS.length]}
                fill={COLORS[index % COLORS.length]}
                fillOpacity={0.3}
                strokeWidth={1.5}
              />
            ))}
          </AreaChart>
        )

      case 'scatter':
        return (
          <ScatterChart margin={{ top: 20, right: 30, left: 20, bottom: 20 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(220, 13%, 91%)" />
            <XAxis
              type="number"
              dataKey={config.xAxis}
              name={config.xAxis}
              tick={{ fontSize: 12, fill: 'hsl(220, 9%, 46%)' }}
            />
            <YAxis
              type="number"
              dataKey={config.yAxis[0]}
              name={config.yAxis[0]}
              tick={{ fontSize: 12, fill: 'hsl(220, 9%, 46%)' }}
            />
            <Tooltip
              cursor={{ strokeDasharray: '3 3' }}
              contentStyle={{
                backgroundColor: 'white',
                border: '1px solid hsl(220, 13%, 91%)',
                borderRadius: '8px',
                fontSize: '12px',
              }}
            />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            <Scatter
              name={`${config.xAxis} vs ${config.yAxis[0]}`}
              data={data}
              fill={COLORS[0]}
            />
          </ScatterChart>
        )

      default:
        return (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            Unsupported chart type: {chartType}
          </div>
        )
    }
  }

  // Use taller chart for time series to show more detail
  const chartHeight = isTimeSeries && dataLength > 100 ? 500 : 400

  return (
    <div className="w-full" style={{ height: chartHeight }}>
      <ResponsiveContainer width="100%" height="100%">
        {renderChart()}
      </ResponsiveContainer>
    </div>
  )
}
