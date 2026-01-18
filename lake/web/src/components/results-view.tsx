import { useState, useEffect, useMemo } from 'react'
import { Table, BarChart3, Loader2, Download, MessageCircle, Sparkles, Share2 } from 'lucide-react'
import type { QueryResponse } from '@/lib/api'
import type { VisualizationConfig, ColumnAnalysis } from '@/lib/visualization'
import { analyzeColumns, getDefaultConfig, getCompatibleChartTypes } from '@/lib/visualization'
import { containsGraphData, extractGraphData } from '@/lib/neo4j-utils'
import { ResultsTable } from './results-table'
import { ResultsChart } from './results-chart'
import { ChartConfigPanel } from './chart-config-panel'
import { CypherGraph } from './cypher-graph'

type ViewMode = 'table' | 'chart' | 'graph'

interface ResultsViewProps {
  results: QueryResponse | null
  isRecommending?: boolean
  recommendedConfig?: VisualizationConfig | null
  onAskAboutResults?: () => void
  onRequestVisualization?: () => void
}

export function ResultsView({
  results,
  isRecommending = false,
  recommendedConfig,
  onAskAboutResults,
  onRequestVisualization,
}: ResultsViewProps) {
  const [viewMode, setViewMode] = useState<ViewMode>('table')
  const [config, setConfig] = useState<VisualizationConfig | null>(null)

  // Analyze columns when results change
  const columnAnalysis = useMemo<ColumnAnalysis[]>(() => {
    if (!results || !results.columns.length || !results.rows.length) {
      return []
    }
    return analyzeColumns(results.columns, results.rows)
  }, [results])

  // Check if visualization is possible
  const canVisualize = useMemo(() => {
    if (columnAnalysis.length === 0) return false
    return getCompatibleChartTypes(columnAnalysis).length > 0
  }, [columnAnalysis])

  // Check if results contain graph data (Neo4j nodes/relationships/paths)
  const hasGraphData = useMemo(() => {
    if (!results || !results.rows.length) return false
    return containsGraphData(results.rows)
  }, [results])

  // Extract graph data for visualization (only compute if hasGraphData)
  const graphData = useMemo(() => {
    if (!hasGraphData || !results) return null
    return extractGraphData(results.rows)
  }, [hasGraphData, results])

  // Initialize config when results change or recommendation arrives
  useEffect(() => {
    if (recommendedConfig) {
      setConfig(recommendedConfig)
      setViewMode('chart') // Switch to chart when recommendation arrives
    } else if (results && columnAnalysis.length > 0) {
      // Just prepare a default config but don't switch to chart
      const defaultConfig = getDefaultConfig(columnAnalysis)
      setConfig(defaultConfig)
    } else {
      setConfig(null)
    }
  }, [results, recommendedConfig, columnAnalysis])

  // Reset to table view when results change (new query)
  useEffect(() => {
    if (results) {
      setViewMode('table')
    }
  }, [results?.columns.join(','), results?.row_count]) // Only trigger on actual new results

  // Reset to table view when results are null
  useEffect(() => {
    if (!results) {
      setViewMode('table')
      setConfig(null)
    }
  }, [results])

  const downloadCSV = () => {
    if (!results) return

    const escapeCSV = (value: unknown): string => {
      if (value === null || value === undefined) return ''
      const str = typeof value === 'object' ? JSON.stringify(value) : String(value)
      if (str.includes(',') || str.includes('"') || str.includes('\n')) {
        return `"${str.replace(/"/g, '""')}"`
      }
      return str
    }

    const header = results.columns.map(escapeCSV).join(',')
    const body = results.rows.map(row => row.map(escapeCSV).join(',')).join('\n')
    const csv = header + '\n' + body

    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `query-results-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  if (!results) {
    return null
  }

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      {/* Header with view toggle */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
        <div className="flex items-center gap-3">
          {/* View Mode Toggle */}
          <div className="flex items-center rounded-md border border-border overflow-hidden">
            <button
              onClick={() => setViewMode('table')}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors ${
                viewMode === 'table'
                  ? 'bg-foreground text-background'
                  : 'bg-card hover:bg-muted text-foreground'
              }`}
            >
              <Table className="w-4 h-4" />
              <span>Table</span>
            </button>
            <button
              onClick={() => setViewMode('chart')}
              disabled={!canVisualize}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors ${
                viewMode === 'chart'
                  ? 'bg-foreground text-background'
                  : canVisualize
                  ? 'bg-card hover:bg-muted text-foreground'
                  : 'bg-muted text-muted-foreground cursor-not-allowed'
              }`}
            >
              <BarChart3 className="w-4 h-4" />
              <span>Chart</span>
            </button>
            <button
              onClick={() => setViewMode('graph')}
              disabled={!hasGraphData}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors ${
                viewMode === 'graph'
                  ? 'bg-foreground text-background'
                  : hasGraphData
                  ? 'bg-card hover:bg-muted text-foreground'
                  : 'bg-muted text-muted-foreground cursor-not-allowed'
              }`}
            >
              <Share2 className="w-4 h-4" />
              <span>Graph</span>
            </button>
          </div>

          {/* Loading indicator for LLM recommendation */}
          {isRecommending && (
            <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <Loader2 className="w-4 h-4 animate-spin" />
              <span>Analyzing...</span>
            </div>
          )}
        </div>

        {/* Right side actions */}
        <div className="flex items-center gap-4">
          {onRequestVisualization && canVisualize && viewMode === 'table' && !isRecommending && (
            <button
              onClick={onRequestVisualization}
              className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <Sparkles className="h-4 w-4" />
              Visualize
            </button>
          )}
          {onAskAboutResults && (
            <button
              onClick={onAskAboutResults}
              className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <MessageCircle className="h-4 w-4" />
              Ask about results
            </button>
          )}
          <button
            onClick={downloadCSV}
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <Download className="h-4 w-4" />
            Download CSV
          </button>
          <span className="text-sm text-muted-foreground">
            {results.row_count.toLocaleString()} rows
          </span>
        </div>
      </div>

      {/* Chart config panel (shown in chart mode) */}
      {viewMode === 'chart' && config && (
        <div className="px-4 py-2 border-b border-border">
          <ChartConfigPanel
            columns={results.columns}
            columnAnalysis={columnAnalysis}
            config={config}
            onConfigChange={setConfig}
          />
        </div>
      )}

      {/* Content area */}
      <div className={viewMode === 'chart' || viewMode === 'graph' ? 'p-4' : ''}>
        {viewMode === 'table' ? (
          <ResultsTable results={results} embedded />
        ) : viewMode === 'graph' && graphData ? (
          <CypherGraph data={graphData} />
        ) : viewMode === 'chart' && config ? (
          <ResultsChart results={results} config={config} />
        ) : (
          <div className="flex items-center justify-center h-[400px] text-muted-foreground">
            Unable to visualize this data. Try adjusting your query.
          </div>
        )}
      </div>
    </div>
  )
}
