import { BarChart3, LineChart, PieChart, AreaChart, ScatterChart } from 'lucide-react'
import type { ChartType, VisualizationConfig, ColumnAnalysis } from '@/lib/visualization'
import { getCompatibleChartTypes } from '@/lib/visualization'

interface ChartConfigPanelProps {
  columns: string[]
  columnAnalysis: ColumnAnalysis[]
  config: VisualizationConfig
  onConfigChange: (config: VisualizationConfig) => void
}

const chartTypeIcons: Record<ChartType, React.ReactNode> = {
  bar: <BarChart3 className="w-4 h-4" />,
  line: <LineChart className="w-4 h-4" />,
  pie: <PieChart className="w-4 h-4" />,
  area: <AreaChart className="w-4 h-4" />,
  scatter: <ScatterChart className="w-4 h-4" />,
}

const chartTypeLabels: Record<ChartType, string> = {
  bar: 'Bar',
  line: 'Line',
  pie: 'Pie',
  area: 'Area',
  scatter: 'Scatter',
}

export function ChartConfigPanel({ columns, columnAnalysis, config, onConfigChange }: ChartConfigPanelProps) {
  const compatibleTypes = getCompatibleChartTypes(columnAnalysis)

  // Get columns suitable for X axis (categorical or temporal preferred)
  const xAxisOptions = columns

  // Get columns suitable for Y axis (numeric preferred for most charts)
  const yAxisOptions = columns

  const handleChartTypeChange = (chartType: ChartType) => {
    onConfigChange({ ...config, chartType })
  }

  const handleXAxisChange = (xAxis: string) => {
    onConfigChange({ ...config, xAxis })
  }

  const handleYAxisChange = (yAxis: string) => {
    // For pie charts, only single Y axis
    if (config.chartType === 'pie') {
      onConfigChange({ ...config, yAxis: [yAxis] })
    } else {
      // Toggle Y axis selection for other charts
      const currentYAxis = config.yAxis
      if (currentYAxis.includes(yAxis)) {
        // Remove if already selected (but keep at least one)
        if (currentYAxis.length > 1) {
          onConfigChange({ ...config, yAxis: currentYAxis.filter(y => y !== yAxis) })
        }
      } else {
        // Add to selection (max 4)
        if (currentYAxis.length < 4) {
          onConfigChange({ ...config, yAxis: [...currentYAxis, yAxis] })
        }
      }
    }
  }

  const getColumnTypeIcon = (col: string) => {
    const analysis = columnAnalysis.find(c => c.name === col)
    if (!analysis) return null

    switch (analysis.dataType) {
      case 'numeric':
        return <span className="text-xs text-blue-500 font-mono">#</span>
      case 'temporal':
        return <span className="text-xs text-green-500 font-mono">T</span>
      case 'categorical':
        return <span className="text-xs text-purple-500 font-mono">A</span>
      default:
        return <span className="text-xs text-muted-foreground font-mono">?</span>
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-4 p-3 bg-secondary/50 rounded-lg text-sm">
      {/* Chart Type Selector */}
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-xs font-medium">Type</span>
        <div className="flex items-center gap-1">
          {(['bar', 'line', 'pie', 'area', 'scatter'] as ChartType[]).map(type => {
            const isCompatible = compatibleTypes.includes(type)
            const isSelected = config.chartType === type
            return (
              <button
                key={type}
                onClick={() => handleChartTypeChange(type)}
                disabled={!isCompatible}
                title={chartTypeLabels[type]}
                className={`p-1.5 rounded transition-colors ${
                  isSelected
                    ? 'bg-foreground text-background'
                    : isCompatible
                    ? 'bg-card hover:bg-muted text-foreground'
                    : 'bg-muted text-muted-foreground cursor-not-allowed opacity-50'
                }`}
              >
                {chartTypeIcons[type]}
              </button>
            )
          })}
        </div>
      </div>

      {/* X Axis Selector */}
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-xs font-medium">X Axis</span>
        <select
          value={config.xAxis}
          onChange={(e) => handleXAxisChange(e.target.value)}
          className="px-2 py-1 rounded border border-border bg-card text-sm focus:outline-none focus:ring-1 focus:ring-accent"
        >
          {xAxisOptions.map(col => (
            <option key={col} value={col}>
              {col}
            </option>
          ))}
        </select>
        {getColumnTypeIcon(config.xAxis)}
      </div>

      {/* Y Axis Selector */}
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground text-xs font-medium">
          Y Axis {config.chartType !== 'pie' && config.chartType !== 'scatter' && '(multi)'}
        </span>
        {config.chartType === 'pie' || config.chartType === 'scatter' ? (
          <select
            value={config.yAxis[0] || ''}
            onChange={(e) => handleYAxisChange(e.target.value)}
            className="px-2 py-1 rounded border border-border bg-card text-sm focus:outline-none focus:ring-1 focus:ring-accent"
          >
            {yAxisOptions.filter(col => col !== config.xAxis).map(col => (
              <option key={col} value={col}>
                {col}
              </option>
            ))}
          </select>
        ) : (
          <div className="flex flex-wrap gap-1">
            {yAxisOptions.filter(col => col !== config.xAxis).map(col => {
              const isSelected = config.yAxis.includes(col)
              return (
                <button
                  key={col}
                  onClick={() => handleYAxisChange(col)}
                  className={`px-2 py-0.5 rounded text-xs transition-colors flex items-center gap-1 ${
                    isSelected
                      ? 'bg-foreground text-background'
                      : 'bg-card border border-border hover:bg-muted'
                  }`}
                >
                  {getColumnTypeIcon(col)}
                  <span className="truncate max-w-[80px]">{col}</span>
                </button>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
