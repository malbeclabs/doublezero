// Chart types supported
export type ChartType = 'bar' | 'line' | 'pie' | 'area' | 'scatter'

// Visualization configuration
export interface VisualizationConfig {
  chartType: ChartType
  xAxis: string           // Column name for X axis
  yAxis: string[]         // Column name(s) for Y axis (multiple for grouped charts)
  groupBy?: string        // Optional grouping column
}

// Column data type inference
export type ColumnDataType = 'numeric' | 'categorical' | 'temporal' | 'unknown'

export interface ColumnAnalysis {
  name: string
  dataType: ColumnDataType
  uniqueCount: number
  hasNulls: boolean
  sampleValues: unknown[]
}

// Thresholds for skipping visualization
const MAX_COLUMNS_FOR_VIZ = 20
const MAX_ROWS_FOR_VIZ = 1000

/**
 * Check if data should skip automatic visualization recommendation
 */
export function shouldSkipVisualization(columns: string[], rows: unknown[][]): boolean {
  return columns.length > MAX_COLUMNS_FOR_VIZ || rows.length > MAX_ROWS_FOR_VIZ
}

/**
 * Infer the data type of a column from its values
 */
function inferColumnType(values: unknown[]): ColumnDataType {
  const nonNullValues = values.filter(v => v !== null && v !== undefined)
  if (nonNullValues.length === 0) return 'unknown'

  const sample = nonNullValues.slice(0, 100)

  // Check if all numeric
  const allNumeric = sample.every(v => {
    if (typeof v === 'number') return true
    if (typeof v === 'string') {
      const num = Number(v)
      return !isNaN(num) && v.trim() !== ''
    }
    return false
  })
  if (allNumeric) return 'numeric'

  // Check if temporal (ISO dates, common date formats)
  const datePatterns = [
    /^\d{4}-\d{2}-\d{2}/, // ISO date
    /^\d{4}-\d{2}-\d{2}T/, // ISO datetime
    /^\d{2}\/\d{2}\/\d{4}/, // MM/DD/YYYY
  ]
  const allTemporal = sample.every(v => {
    if (typeof v !== 'string') return false
    return datePatterns.some(p => p.test(v)) || !isNaN(Date.parse(v))
  })
  if (allTemporal) return 'temporal'

  // Default to categorical
  return 'categorical'
}

/**
 * Analyze columns to determine their data types and characteristics
 */
export function analyzeColumns(columns: string[], rows: unknown[][]): ColumnAnalysis[] {
  return columns.map((name, colIndex) => {
    const values = rows.map(row => row[colIndex])
    const nonNullValues = values.filter(v => v !== null && v !== undefined)
    const uniqueValues = new Set(nonNullValues.map(v => String(v)))

    return {
      name,
      dataType: inferColumnType(values),
      uniqueCount: uniqueValues.size,
      hasNulls: nonNullValues.length < values.length,
      sampleValues: nonNullValues.slice(0, 5),
    }
  })
}

/**
 * Get chart types that are compatible with the given column analysis
 */
export function getCompatibleChartTypes(analysis: ColumnAnalysis[]): ChartType[] {
  const compatible: ChartType[] = []

  const numericCols = analysis.filter(c => c.dataType === 'numeric')
  const categoricalCols = analysis.filter(c => c.dataType === 'categorical')
  const temporalCols = analysis.filter(c => c.dataType === 'temporal')

  // Bar: categorical X + numeric Y
  if (categoricalCols.length >= 1 && numericCols.length >= 1) {
    compatible.push('bar')
  }

  // Line: temporal/categorical X + numeric Y
  if ((temporalCols.length >= 1 || categoricalCols.length >= 1) && numericCols.length >= 1) {
    compatible.push('line')
  }

  // Pie: categorical + numeric (with few categories)
  if (categoricalCols.length >= 1 && numericCols.length >= 1) {
    const catCol = categoricalCols[0]
    if (catCol.uniqueCount <= 10) {
      compatible.push('pie')
    }
  }

  // Area: temporal/categorical X + numeric Y
  if ((temporalCols.length >= 1 || categoricalCols.length >= 1) && numericCols.length >= 1) {
    compatible.push('area')
  }

  // Scatter: 2+ numeric columns
  if (numericCols.length >= 2) {
    compatible.push('scatter')
  }

  return compatible
}

/**
 * Get a default visualization config based on column analysis
 */
export function getDefaultConfig(analysis: ColumnAnalysis[]): VisualizationConfig | null {
  const compatible = getCompatibleChartTypes(analysis)
  if (compatible.length === 0) return null

  const numericCols = analysis.filter(c => c.dataType === 'numeric')
  const categoricalCols = analysis.filter(c => c.dataType === 'categorical')
  const temporalCols = analysis.filter(c => c.dataType === 'temporal')

  // Prefer temporal for X axis if available
  const xAxisCol = temporalCols[0] || categoricalCols[0] || analysis[0]
  const yAxisCols = numericCols.length > 0 ? numericCols : [analysis.find(c => c !== xAxisCol)!]

  // Choose chart type based on data shape
  let chartType: ChartType = 'bar'

  if (temporalCols.length >= 1 && numericCols.length >= 1) {
    chartType = 'line'
  } else if (categoricalCols.length >= 1 && numericCols.length >= 1) {
    const catCol = categoricalCols[0]
    if (catCol.uniqueCount <= 6 && numericCols.length === 1) {
      chartType = 'pie'
    } else {
      chartType = 'bar'
    }
  } else if (numericCols.length >= 2) {
    chartType = 'scatter'
  }

  return {
    chartType,
    xAxis: xAxisCol.name,
    yAxis: yAxisCols.slice(0, 3).map(c => c.name), // Limit to 3 Y columns
  }
}

/**
 * Transform query results into chart-compatible data format
 */
export function transformDataForChart(
  columns: string[],
  rows: unknown[][],
  config: VisualizationConfig
): Record<string, unknown>[] {
  const xIndex = columns.indexOf(config.xAxis)
  const yIndices = config.yAxis.map(y => columns.indexOf(y))

  return rows.map(row => {
    const item: Record<string, unknown> = {
      [config.xAxis]: row[xIndex],
    }
    config.yAxis.forEach((yCol, i) => {
      const val = row[yIndices[i]]
      // Convert to number for chart compatibility
      item[yCol] = typeof val === 'number' ? val : Number(val) || 0
    })
    return item
  })
}

/**
 * Transform data specifically for pie charts
 */
export function transformDataForPieChart(
  columns: string[],
  rows: unknown[][],
  config: VisualizationConfig
): { name: string; value: number }[] {
  const xIndex = columns.indexOf(config.xAxis)
  const yIndex = columns.indexOf(config.yAxis[0])

  return rows.map(row => ({
    name: String(row[xIndex] ?? 'Unknown'),
    value: typeof row[yIndex] === 'number' ? row[yIndex] : Number(row[yIndex]) || 0,
  }))
}
