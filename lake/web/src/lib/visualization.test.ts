import { describe, it, expect } from 'vitest'
import {
  shouldSkipVisualization,
  analyzeColumns,
  getCompatibleChartTypes,
  getDefaultConfig,
  transformDataForChart,
  transformDataForPieChart,
  type ColumnAnalysis,
} from './visualization'

describe('shouldSkipVisualization', () => {
  it('returns false for small datasets', () => {
    const columns = ['a', 'b', 'c']
    const rows = [[1, 2, 3], [4, 5, 6]]
    expect(shouldSkipVisualization(columns, rows)).toBe(false)
  })

  it('returns true when too many columns', () => {
    const columns = Array.from({ length: 25 }, (_, i) => `col${i}`)
    const rows = [[1]]
    expect(shouldSkipVisualization(columns, rows)).toBe(true)
  })

  it('returns true when too many rows', () => {
    const columns = ['a']
    const rows = Array.from({ length: 1500 }, () => [1])
    expect(shouldSkipVisualization(columns, rows)).toBe(true)
  })

  it('returns false at exactly the limit', () => {
    const columns = Array.from({ length: 20 }, (_, i) => `col${i}`)
    const rows = Array.from({ length: 1000 }, () => [1])
    expect(shouldSkipVisualization(columns, rows)).toBe(false)
  })
})

describe('analyzeColumns', () => {
  it('identifies numeric columns', () => {
    const columns = ['value']
    const rows = [[1], [2], [3], [4], [5]]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis).toHaveLength(1)
    expect(analysis[0].name).toBe('value')
    expect(analysis[0].dataType).toBe('numeric')
    expect(analysis[0].uniqueCount).toBe(5)
    expect(analysis[0].hasNulls).toBe(false)
  })

  it('identifies categorical columns', () => {
    const columns = ['category']
    const rows = [['apple'], ['banana'], ['cherry']]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].dataType).toBe('categorical')
  })

  it('identifies temporal columns with ISO dates', () => {
    const columns = ['date']
    const rows = [['2024-01-01'], ['2024-01-02'], ['2024-01-03']]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].dataType).toBe('temporal')
  })

  it('identifies temporal columns with ISO datetimes', () => {
    const columns = ['timestamp']
    const rows = [['2024-01-01T12:00:00Z'], ['2024-01-02T14:30:00Z']]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].dataType).toBe('temporal')
  })

  it('handles null values', () => {
    const columns = ['value']
    const rows = [[1], [null], [3], [undefined], [5]]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].hasNulls).toBe(true)
    expect(analysis[0].uniqueCount).toBe(3)
  })

  it('handles numeric strings', () => {
    const columns = ['amount']
    const rows = [['100'], ['200'], ['300']]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].dataType).toBe('numeric')
  })

  it('returns unknown for all-null columns', () => {
    const columns = ['empty']
    const rows = [[null], [null], [undefined]]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].dataType).toBe('unknown')
  })

  it('provides sample values', () => {
    const columns = ['value']
    const rows = [[1], [2], [3], [4], [5], [6], [7], [8], [9], [10]]
    const analysis = analyzeColumns(columns, rows)

    expect(analysis[0].sampleValues).toHaveLength(5)
    expect(analysis[0].sampleValues).toEqual([1, 2, 3, 4, 5])
  })
})

describe('getCompatibleChartTypes', () => {
  it('returns bar for categorical + numeric', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'category', dataType: 'categorical', uniqueCount: 5, hasNulls: false, sampleValues: [] },
      { name: 'value', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toContain('bar')
  })

  it('returns line for temporal + numeric', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'date', dataType: 'temporal', uniqueCount: 30, hasNulls: false, sampleValues: [] },
      { name: 'value', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toContain('line')
  })

  it('returns pie for categorical + numeric with few categories', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'status', dataType: 'categorical', uniqueCount: 5, hasNulls: false, sampleValues: [] },
      { name: 'count', dataType: 'numeric', uniqueCount: 5, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toContain('pie')
  })

  it('excludes pie for too many categories', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'city', dataType: 'categorical', uniqueCount: 50, hasNulls: false, sampleValues: [] },
      { name: 'count', dataType: 'numeric', uniqueCount: 50, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).not.toContain('pie')
  })

  it('returns scatter for 2+ numeric columns', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'x', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'y', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toContain('scatter')
  })

  it('returns area for temporal + numeric', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'date', dataType: 'temporal', uniqueCount: 30, hasNulls: false, sampleValues: [] },
      { name: 'value', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toContain('area')
  })

  it('returns empty for incompatible columns', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'text1', dataType: 'categorical', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'text2', dataType: 'categorical', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const types = getCompatibleChartTypes(analysis)
    expect(types).toEqual([])
  })
})

describe('getDefaultConfig', () => {
  it('returns null for incompatible data', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'text', dataType: 'unknown', uniqueCount: 0, hasNulls: true, sampleValues: [] },
    ]

    expect(getDefaultConfig(analysis)).toBeNull()
  })

  it('returns line chart for temporal + numeric', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'date', dataType: 'temporal', uniqueCount: 30, hasNulls: false, sampleValues: [] },
      { name: 'value', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const config = getDefaultConfig(analysis)
    expect(config).not.toBeNull()
    expect(config?.chartType).toBe('line')
    expect(config?.xAxis).toBe('date')
    expect(config?.yAxis).toContain('value')
  })

  it('returns pie chart for categorical + numeric with few categories', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'status', dataType: 'categorical', uniqueCount: 4, hasNulls: false, sampleValues: [] },
      { name: 'count', dataType: 'numeric', uniqueCount: 4, hasNulls: false, sampleValues: [] },
    ]

    const config = getDefaultConfig(analysis)
    expect(config?.chartType).toBe('pie')
  })

  it('returns bar chart for categorical + numeric with many categories', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'city', dataType: 'categorical', uniqueCount: 20, hasNulls: false, sampleValues: [] },
      { name: 'count', dataType: 'numeric', uniqueCount: 20, hasNulls: false, sampleValues: [] },
    ]

    const config = getDefaultConfig(analysis)
    expect(config?.chartType).toBe('bar')
  })

  it('returns scatter for numeric-only data', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'x', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'y', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const config = getDefaultConfig(analysis)
    expect(config?.chartType).toBe('scatter')
  })

  it('limits Y axis columns to 3', () => {
    const analysis: ColumnAnalysis[] = [
      { name: 'date', dataType: 'temporal', uniqueCount: 30, hasNulls: false, sampleValues: [] },
      { name: 'a', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'b', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'c', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
      { name: 'd', dataType: 'numeric', uniqueCount: 100, hasNulls: false, sampleValues: [] },
    ]

    const config = getDefaultConfig(analysis)
    expect(config?.yAxis.length).toBeLessThanOrEqual(3)
  })
})

describe('transformDataForChart', () => {
  it('transforms rows into chart data format', () => {
    const columns = ['category', 'value']
    const rows = [['A', 10], ['B', 20], ['C', 30]]
    const config = { chartType: 'bar' as const, xAxis: 'category', yAxis: ['value'] }

    const result = transformDataForChart(columns, rows, config)

    expect(result).toHaveLength(3)
    expect(result[0]).toEqual({ category: 'A', value: 10 })
    expect(result[1]).toEqual({ category: 'B', value: 20 })
  })

  it('converts string values to numbers for Y axis', () => {
    const columns = ['x', 'y']
    const rows = [['a', '100'], ['b', '200']]
    const config = { chartType: 'bar' as const, xAxis: 'x', yAxis: ['y'] }

    const result = transformDataForChart(columns, rows, config)

    expect(result[0].y).toBe(100)
    expect(result[1].y).toBe(200)
  })

  it('handles non-numeric Y values as 0', () => {
    const columns = ['x', 'y']
    const rows = [['a', 'not a number']]
    const config = { chartType: 'bar' as const, xAxis: 'x', yAxis: ['y'] }

    const result = transformDataForChart(columns, rows, config)

    expect(result[0].y).toBe(0)
  })

  it('handles multiple Y axis columns', () => {
    const columns = ['x', 'y1', 'y2']
    const rows = [['a', 10, 20], ['b', 30, 40]]
    const config = { chartType: 'line' as const, xAxis: 'x', yAxis: ['y1', 'y2'] }

    const result = transformDataForChart(columns, rows, config)

    expect(result[0]).toEqual({ x: 'a', y1: 10, y2: 20 })
    expect(result[1]).toEqual({ x: 'b', y1: 30, y2: 40 })
  })
})

describe('transformDataForPieChart', () => {
  it('transforms rows into pie chart format', () => {
    const columns = ['category', 'count']
    const rows = [['Active', 100], ['Inactive', 50], ['Pending', 25]]
    const config = { chartType: 'pie' as const, xAxis: 'category', yAxis: ['count'] }

    const result = transformDataForPieChart(columns, rows, config)

    expect(result).toHaveLength(3)
    expect(result[0]).toEqual({ name: 'Active', value: 100 })
    expect(result[1]).toEqual({ name: 'Inactive', value: 50 })
  })

  it('handles null/undefined names', () => {
    const columns = ['name', 'value']
    const rows = [[null, 50], [undefined, 30]]
    const config = { chartType: 'pie' as const, xAxis: 'name', yAxis: ['value'] }

    const result = transformDataForPieChart(columns, rows, config)

    expect(result[0].name).toBe('Unknown')
    expect(result[1].name).toBe('Unknown')
  })

  it('converts string values to numbers', () => {
    const columns = ['name', 'value']
    const rows = [['A', '100'], ['B', '200']]
    const config = { chartType: 'pie' as const, xAxis: 'name', yAxis: ['value'] }

    const result = transformDataForPieChart(columns, rows, config)

    expect(result[0].value).toBe(100)
    expect(result[1].value).toBe(200)
  })

  it('handles non-numeric values as 0', () => {
    const columns = ['name', 'value']
    const rows = [['A', 'invalid']]
    const config = { chartType: 'pie' as const, xAxis: 'name', yAxis: ['value'] }

    const result = transformDataForPieChart(columns, rows, config)

    expect(result[0].value).toBe(0)
  })
})
