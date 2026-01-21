import { useState, useMemo, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Wrench, X, AlertTriangle, Trash2, Download, Server, Link2 } from 'lucide-react'
import { fetchMaintenanceImpact, fetchAutocomplete } from '@/lib/api'
import type { MaintenanceItem, MaintenanceImpactResponse, SearchSuggestion } from '@/lib/api'

// Selected item type for the maintenance list
interface SelectedMaintenanceItem {
  type: 'device' | 'link'
  pk: string
  code: string
}

// Search input with autocomplete
function SearchInput({
  onSelect,
  placeholder,
  filterType,
}: {
  onSelect: (item: SelectedMaintenanceItem) => void
  placeholder: string
  filterType?: 'device' | 'link'
}) {
  const [query, setQuery] = useState('')
  const [isOpen, setIsOpen] = useState(false)

  const { data: suggestions } = useQuery({
    queryKey: ['autocomplete', query],
    queryFn: () => fetchAutocomplete(query, 20),
    enabled: query.length >= 2,
    staleTime: 30000,
  })

  // Filter suggestions by type if specified
  const filteredSuggestions = useMemo(() => {
    if (!suggestions?.suggestions) return []
    if (!filterType) {
      return suggestions.suggestions.filter(s => s.type === 'device' || s.type === 'link')
    }
    return suggestions.suggestions.filter(s => s.type === filterType)
  }, [suggestions, filterType])

  const handleSelect = (suggestion: SearchSuggestion) => {
    onSelect({
      type: suggestion.type as 'device' | 'link',
      pk: suggestion.id,
      code: suggestion.label,
    })
    setQuery('')
    setIsOpen(false)
  }

  return (
    <div className="relative">
      <input
        type="text"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value)
          setIsOpen(true)
        }}
        onFocus={() => setIsOpen(true)}
        onBlur={() => setTimeout(() => setIsOpen(false), 200)}
        placeholder={placeholder}
        className="w-full px-3 py-2 text-sm bg-muted border border-border rounded-md focus:outline-none focus:ring-2 focus:ring-accent"
      />
      {isOpen && filteredSuggestions.length > 0 && (
        <div className="absolute z-50 w-full mt-1 bg-card border border-border rounded-md shadow-lg max-h-60 overflow-y-auto">
          {filteredSuggestions.map((suggestion) => (
            <button
              key={`${suggestion.type}-${suggestion.id}`}
              onClick={() => handleSelect(suggestion)}
              className="w-full px-3 py-2 text-left text-sm hover:bg-muted flex items-center gap-2"
            >
              {suggestion.type === 'device' ? (
                <Server className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <Link2 className="h-3.5 w-3.5 text-muted-foreground" />
              )}
              <span>{suggestion.label}</span>
              <span className="text-muted-foreground text-xs ml-auto">{suggestion.sublabel}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// Impact indicator component
function ImpactBadge({ impact, disconnected, causesPartition }: MaintenanceItem) {
  if (causesPartition) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-400 rounded">
        <AlertTriangle className="h-3 w-3" />
        Partition
      </span>
    )
  }
  if (disconnected > 0) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400 rounded">
        {disconnected} disconnected
      </span>
    )
  }
  if (impact > 10) {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400 rounded">
        {impact} paths
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-400 rounded">
      Low impact
    </span>
  )
}

export function MaintenancePlannerPage() {
  const [selectedItems, setSelectedItems] = useState<SelectedMaintenanceItem[]>([])
  const [analysisResult, setAnalysisResult] = useState<MaintenanceImpactResponse | null>(null)
  const [isAnalyzing, setIsAnalyzing] = useState(false)

  // Add an item to the maintenance list
  const handleAddItem = useCallback((item: SelectedMaintenanceItem) => {
    setSelectedItems(prev => {
      // Check if already added
      if (prev.some(i => i.pk === item.pk && i.type === item.type)) {
        return prev
      }
      return [...prev, item]
    })
    // Clear previous analysis when items change
    setAnalysisResult(null)
  }, [])

  // Remove an item from the maintenance list
  const handleRemoveItem = useCallback((pk: string) => {
    setSelectedItems(prev => prev.filter(i => i.pk !== pk))
    setAnalysisResult(null)
  }, [])

  // Clear all items
  const handleClearAll = useCallback(() => {
    setSelectedItems([])
    setAnalysisResult(null)
  }, [])

  // Analyze maintenance impact
  const handleAnalyze = useCallback(async () => {
    if (selectedItems.length === 0) return

    setIsAnalyzing(true)
    try {
      const devices = selectedItems.filter(i => i.type === 'device').map(i => i.pk)
      const links = selectedItems.filter(i => i.type === 'link').map(i => i.pk)
      const result = await fetchMaintenanceImpact(devices, links)
      setAnalysisResult(result)
    } catch (err) {
      console.error('Failed to analyze maintenance impact:', err)
      setAnalysisResult({
        items: [],
        totalImpact: 0,
        totalDisconnected: 0,
        recommendedOrder: [],
        error: err instanceof Error ? err.message : 'Failed to analyze impact',
      })
    } finally {
      setIsAnalyzing(false)
    }
  }, [selectedItems])

  // Export maintenance plan as text
  const handleExport = useCallback(() => {
    if (!analysisResult) return

    const lines: string[] = [
      'Maintenance Plan',
      '================',
      '',
      `Generated: ${new Date().toISOString()}`,
      '',
      'Summary',
      '-------',
      `Total items: ${selectedItems.length}`,
      `Total affected paths: ${analysisResult.totalImpact}`,
      `Total disconnected devices: ${analysisResult.totalDisconnected}`,
      '',
    ]

    // Disconnected devices warning
    if (analysisResult.disconnectedList && analysisResult.disconnectedList.length > 0) {
      lines.push('⚠️  DEVICES THAT WILL LOSE CONNECTIVITY:')
      for (const device of analysisResult.disconnectedList) {
        lines.push(`    - ${device}`)
      }
      lines.push('')
    }

    // Affected links by metro
    if (analysisResult.affectedMetros && analysisResult.affectedMetros.length > 0) {
      lines.push('Affected Links by Metro')
      lines.push('-----------------------')
      lines.push('ISIS adjacencies that will go down:')
      lines.push('')
      for (const pair of analysisResult.affectedMetros) {
        lines.push(`  ${pair.sourceMetro} ↔ ${pair.targetMetro} (${pair.affectedLinks.length} link${pair.affectedLinks.length !== 1 ? 's' : ''} down)`)
        for (const link of pair.affectedLinks) {
          lines.push(`    ✕ ${link.sourceDevice} → ${link.targetDevice}`)
        }
        lines.push('')
      }
    }

    // Routing impact
    if (analysisResult.affectedPaths && analysisResult.affectedPaths.length > 0) {
      lines.push('Routing Impact')
      lines.push('--------------')
      lines.push('Device pairs whose shortest path changes:')
      lines.push('')
      for (const path of analysisResult.affectedPaths) {
        const latencyBeforeMs = (path.metricBefore / 1000).toFixed(2)
        const latencyAfterMs = (path.metricAfter / 1000).toFixed(2)
        lines.push(`  ${path.source} → ${path.target}`)
        if (path.hopsAfter === -1) {
          lines.push(`    Status: DISCONNECTED (was ${path.hopsBefore} hops, ${latencyBeforeMs}ms)`)
        } else {
          const hopDiff = path.hopsAfter - path.hopsBefore
          const latencyDiffMs = ((path.metricAfter - path.metricBefore) / 1000).toFixed(2)
          lines.push(`    Hops: ${path.hopsBefore} → ${path.hopsAfter} (${hopDiff > 0 ? '+' : ''}${hopDiff})`)
          lines.push(`    Latency: ${latencyBeforeMs}ms → ${latencyAfterMs}ms (${Number(latencyDiffMs) > 0 ? '+' : ''}${latencyDiffMs}ms)`)
          lines.push(`    Status: ${path.status.toUpperCase()}`)
        }
        lines.push('')
      }
    }

    // Recommended order
    lines.push('Recommended Maintenance Order')
    lines.push('-----------------------------')
    lines.push('(Items ordered from least impactful to most impactful)')
    lines.push('')

    for (let i = 0; i < analysisResult.recommendedOrder.length; i++) {
      const pk = analysisResult.recommendedOrder[i]
      const item = analysisResult.items.find(it => it.pk === pk)
      if (item) {
        lines.push(`${i + 1}. [${item.type.toUpperCase()}] ${item.code}`)
        lines.push(`   Impact: ${item.impact} paths affected`)
        if (item.disconnectedDevices && item.disconnectedDevices.length > 0) {
          lines.push(`   Disconnects: ${item.disconnectedDevices.join(', ')}`)
        }
        if (item.causesPartition) {
          lines.push('   ⚠️  WARNING: Causes network partition!')
        }
        lines.push('')
      }
    }

    const text = lines.join('\n')
    const blob = new Blob([text], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'maintenance-plan.txt'
    a.click()
    URL.revokeObjectURL(url)
  }, [selectedItems, analysisResult])

  // Build item lookup from analysis result
  const itemLookup = useMemo(() => {
    const map = new Map<string, MaintenanceItem>()
    if (analysisResult) {
      for (const item of analysisResult.items) {
        map.set(item.pk, item)
      }
    }
    return map
  }, [analysisResult])

  return (
    <div className="flex-1 flex flex-col bg-background overflow-hidden">
      {/* Header */}
      <div className="border-b border-border px-6 py-4">
        <div className="flex items-center gap-3">
          <Wrench className="h-5 w-5 text-muted-foreground" />
          <h1 className="text-lg font-semibold">Maintenance Planner</h1>
        </div>
        <p className="text-sm text-muted-foreground mt-1">
          Plan maintenance windows by selecting devices and links to take offline, then analyze the impact.
        </p>
      </div>

      <div className="flex-1 overflow-auto p-6">
        <div className="max-w-4xl mx-auto space-y-6">
          {/* Add items section */}
          <div className="bg-card border border-border rounded-lg p-4">
            <h2 className="text-sm font-medium mb-3">Add Items to Maintenance</h2>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Search Devices</label>
                <SearchInput
                  onSelect={handleAddItem}
                  placeholder="Search for a device..."
                  filterType="device"
                />
              </div>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Search Links</label>
                <SearchInput
                  onSelect={handleAddItem}
                  placeholder="Search for a link..."
                  filterType="link"
                />
              </div>
            </div>
          </div>

          {/* Selected items list */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-sm font-medium">
                Selected Items ({selectedItems.length})
              </h2>
              {selectedItems.length > 0 && (
                <button
                  onClick={handleClearAll}
                  className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
                >
                  <Trash2 className="h-3 w-3" />
                  Clear All
                </button>
              )}
            </div>

            {selectedItems.length === 0 ? (
              <div className="text-sm text-muted-foreground py-8 text-center">
                No items selected. Use the search above to add devices or links.
              </div>
            ) : (
              <div className="space-y-2">
                {selectedItems.map((item) => {
                  const analysisItem = itemLookup.get(item.pk)
                  return (
                    <div
                      key={item.pk}
                      className="flex items-center justify-between p-2 bg-muted rounded-md"
                    >
                      <div className="flex items-center gap-2">
                        {item.type === 'device' ? (
                          <Server className="h-4 w-4 text-muted-foreground" />
                        ) : (
                          <Link2 className="h-4 w-4 text-muted-foreground" />
                        )}
                        <span className="text-sm font-medium">{item.code}</span>
                        <span className="text-xs text-muted-foreground">({item.type})</span>
                      </div>
                      <div className="flex items-center gap-2">
                        {analysisItem && <ImpactBadge {...analysisItem} />}
                        <button
                          onClick={() => handleRemoveItem(item.pk)}
                          className="p-1 text-muted-foreground hover:text-foreground rounded"
                        >
                          <X className="h-4 w-4" />
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}

            {/* Analyze button */}
            {selectedItems.length > 0 && (
              <div className="mt-4 flex gap-2">
                <button
                  onClick={handleAnalyze}
                  disabled={isAnalyzing}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-accent text-accent-foreground rounded-md hover:bg-accent/90 disabled:opacity-50 transition-colors"
                >
                  {isAnalyzing ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Analyzing...
                    </>
                  ) : (
                    <>
                      <AlertTriangle className="h-4 w-4" />
                      Analyze Impact
                    </>
                  )}
                </button>
                {analysisResult && !analysisResult.error && (
                  <button
                    onClick={handleExport}
                    className="flex items-center gap-2 px-4 py-2 bg-muted hover:bg-muted/80 rounded-md transition-colors"
                  >
                    <Download className="h-4 w-4" />
                    Export Plan
                  </button>
                )}
              </div>
            )}
          </div>

          {/* Analysis results */}
          {analysisResult && (
            <div className="bg-card border border-border rounded-lg p-4">
              <h2 className="text-sm font-medium mb-3">Impact Analysis</h2>

              {analysisResult.error ? (
                <div className="text-destructive text-sm">{analysisResult.error}</div>
              ) : (
                <>
                  {/* Summary */}
                  {(() => {
                    // Compute real stats from affected paths
                    const adjacenciesDown = analysisResult.affectedMetros?.reduce(
                      (sum, m) => sum + (m.affectedLinks?.length || 0), 0
                    ) || 0
                    const rerouted = analysisResult.affectedPaths?.filter(p => p.status === 'rerouted' || p.status === 'degraded') || []
                    const disconnected = analysisResult.affectedPaths?.filter(p => p.status === 'disconnected') || []
                    const avgHopIncrease = rerouted.length > 0
                      ? rerouted.reduce((sum, p) => sum + (p.hopsAfter - p.hopsBefore), 0) / rerouted.length
                      : 0
                    // Convert metric (microseconds) to milliseconds
                    const avgLatencyIncreaseMs = rerouted.length > 0
                      ? rerouted.reduce((sum, p) => sum + (p.metricAfter - p.metricBefore), 0) / rerouted.length / 1000
                      : 0

                    return (
                      <div className="grid grid-cols-4 gap-3 mb-6">
                        <div className="bg-muted rounded-lg p-3">
                          <div className="text-xs text-muted-foreground mb-1">ISIS Adjacencies Down</div>
                          <div className="text-xl font-bold">{adjacenciesDown}</div>
                        </div>
                        <div className={`rounded-lg p-3 ${rerouted.length > 0 ? 'bg-yellow-100 dark:bg-yellow-900/40' : 'bg-muted'}`}>
                          <div className="text-xs text-muted-foreground mb-1">Paths Rerouted</div>
                          <div className={`text-xl font-bold ${rerouted.length > 0 ? 'text-yellow-700 dark:text-yellow-400' : ''}`}>
                            {rerouted.length}
                          </div>
                          {rerouted.length > 0 && (
                            <div className="text-xs text-muted-foreground mt-1">
                              avg +{avgHopIncrease.toFixed(1)} hops, +{avgLatencyIncreaseMs.toFixed(2)}ms
                            </div>
                          )}
                        </div>
                        <div className={`rounded-lg p-3 ${disconnected.length > 0 ? 'bg-red-100 dark:bg-red-900/40' : 'bg-muted'}`}>
                          <div className="text-xs text-muted-foreground mb-1">Paths Disconnected</div>
                          <div className={`text-xl font-bold ${disconnected.length > 0 ? 'text-red-700 dark:text-red-400' : ''}`}>
                            {disconnected.length}
                          </div>
                        </div>
                        <div className={`rounded-lg p-3 ${analysisResult.totalDisconnected > 0 ? 'bg-red-100 dark:bg-red-900/40' : 'bg-muted'}`}>
                          <div className="text-xs text-muted-foreground mb-1">Devices Isolated</div>
                          <div className={`text-xl font-bold ${analysisResult.totalDisconnected > 0 ? 'text-red-700 dark:text-red-400' : ''}`}>
                            {analysisResult.totalDisconnected}
                          </div>
                        </div>
                      </div>
                    )
                  })()}

                  {/* Warnings */}
                  {analysisResult.items.some(i => i.causesPartition) && (
                    <div className="mb-4 p-3 bg-red-100 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg">
                      <div className="flex items-center gap-2 text-red-700 dark:text-red-400 font-medium text-sm">
                        <AlertTriangle className="h-4 w-4" />
                        Warning: Some items will cause network partition
                      </div>
                      <ul className="mt-2 text-sm text-red-600 dark:text-red-400 space-y-1">
                        {analysisResult.items.filter(i => i.causesPartition).map(item => (
                          <li key={item.pk}>• {item.code}</li>
                        ))}
                      </ul>
                    </div>
                  )}

                  {/* Disconnected devices */}
                  {analysisResult.disconnectedList && analysisResult.disconnectedList.length > 0 && (
                    <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                      <div className="text-sm font-medium text-red-700 dark:text-red-400 mb-2">
                        Devices That Will Lose Connectivity ({analysisResult.disconnectedList.length})
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {analysisResult.disconnectedList.map((code, idx) => (
                          <span key={idx} className="px-2 py-1 bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-400 rounded text-xs font-medium">
                            {code}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Affected metro pairs */}
                  {analysisResult.affectedMetros && analysisResult.affectedMetros.length > 0 && (
                    <div className="mb-4">
                      <h3 className="text-sm font-medium mb-2">Affected Links by Metro</h3>
                      <p className="text-xs text-muted-foreground mb-2">
                        ISIS adjacencies that will go down when maintenance targets are taken offline.
                      </p>
                      <div className="space-y-3">
                        {analysisResult.affectedMetros.map((pair, idx) => (
                          <div key={idx} className="p-3 bg-muted rounded">
                            <div className="flex items-center gap-2 mb-2">
                              <span className="font-medium text-sm">{pair.sourceMetro}</span>
                              <span className="text-muted-foreground">↔</span>
                              <span className="font-medium text-sm">{pair.targetMetro}</span>
                              <span className={`ml-auto text-xs px-2 py-0.5 rounded ${
                                pair.status === 'disconnected'
                                  ? 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-400'
                                  : 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400'
                              }`}>
                                {pair.affectedLinks.length} link{pair.affectedLinks.length !== 1 ? 's' : ''} down
                              </span>
                            </div>
                            <div className="space-y-1">
                              {pair.affectedLinks.map((link, linkIdx) => (
                                <div key={linkIdx} className="flex items-center gap-2 text-xs text-muted-foreground pl-2">
                                  <span className="text-red-500">✕</span>
                                  <span className="font-mono">{link.sourceDevice}</span>
                                  <span>→</span>
                                  <span className="font-mono">{link.targetDevice}</span>
                                </div>
                              ))}
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Routing Impact - detailed path changes */}
                  {analysisResult.affectedPaths && analysisResult.affectedPaths.length > 0 && (
                    <div className="mb-4">
                      <h3 className="text-sm font-medium mb-2">Routing Impact</h3>
                      <p className="text-xs text-muted-foreground mb-3">
                        Device pairs whose shortest path changes when maintenance targets go offline.
                      </p>

                      {/* Table header */}
                      <div className="grid grid-cols-12 gap-2 px-3 py-2 bg-muted/50 rounded-t text-xs font-medium text-muted-foreground">
                        <div className="col-span-5">Path</div>
                        <div className="col-span-2 text-center">Hops</div>
                        <div className="col-span-3 text-center">Latency</div>
                        <div className="col-span-2 text-right">Status</div>
                      </div>

                      {/* Table rows */}
                      <div className="border border-border rounded-b divide-y divide-border">
                        {analysisResult.affectedPaths.map((path, idx) => {
                          const hopDiff = path.hopsAfter - path.hopsBefore
                          // Convert metric (microseconds) to milliseconds
                          const latencyBeforeMs = path.metricBefore / 1000
                          const latencyAfterMs = path.metricAfter / 1000
                          const latencyDiffMs = latencyAfterMs - latencyBeforeMs
                          return (
                            <div key={idx} className="grid grid-cols-12 gap-2 px-3 py-2 text-sm items-center hover:bg-muted/30">
                              <div className="col-span-5 flex items-center gap-1 min-w-0">
                                <span className="font-mono text-xs truncate">{path.source}</span>
                                <span className="text-muted-foreground text-xs">→</span>
                                <span className="font-mono text-xs truncate">{path.target}</span>
                              </div>
                              <div className="col-span-2 text-center text-xs">
                                {path.hopsAfter === -1 ? (
                                  <span className="text-red-500">{path.hopsBefore} → ✕</span>
                                ) : (
                                  <span>
                                    {path.hopsBefore} → {path.hopsAfter}
                                    <span className={`ml-1 ${hopDiff > 0 ? 'text-yellow-600 dark:text-yellow-400' : 'text-green-600'}`}>
                                      ({hopDiff > 0 ? '+' : ''}{hopDiff})
                                    </span>
                                  </span>
                                )}
                              </div>
                              <div className="col-span-3 text-center text-xs">
                                {path.metricAfter === -1 ? (
                                  <span className="text-red-500">{latencyBeforeMs.toFixed(2)}ms → ✕</span>
                                ) : (
                                  <span>
                                    {latencyBeforeMs.toFixed(2)} → {latencyAfterMs.toFixed(2)}ms
                                    <span className={`ml-1 ${latencyDiffMs > 0 ? 'text-yellow-600 dark:text-yellow-400' : 'text-green-600'}`}>
                                      ({latencyDiffMs > 0 ? '+' : ''}{latencyDiffMs.toFixed(2)})
                                    </span>
                                  </span>
                                )}
                              </div>
                              <div className="col-span-2 text-right">
                                <span className={`text-xs px-2 py-0.5 rounded ${
                                  path.status === 'disconnected'
                                    ? 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-400'
                                    : path.status === 'degraded'
                                    ? 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400'
                                    : 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-400'
                                }`}>
                                  {path.status}
                                </span>
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}

                  {/* Recommended order */}
                  <div>
                    <h3 className="text-sm font-medium mb-2">Recommended Maintenance Order</h3>
                    <p className="text-xs text-muted-foreground mb-3">
                      Items ordered from least impactful to most impactful. Take down items in this order to minimize disruption.
                    </p>
                    <div className="space-y-2">
                      {analysisResult.recommendedOrder.map((pk, index) => {
                        const item = itemLookup.get(pk)
                        if (!item) return null
                        return (
                          <div
                            key={pk}
                            className="flex items-center gap-3 p-3 bg-muted rounded-md"
                          >
                            <span className="w-6 h-6 flex items-center justify-center bg-accent text-accent-foreground rounded-full text-xs font-bold">
                              {index + 1}
                            </span>
                            <div className="flex items-center gap-2 flex-1">
                              {item.type === 'device' ? (
                                <Server className="h-4 w-4 text-muted-foreground" />
                              ) : (
                                <Link2 className="h-4 w-4 text-muted-foreground" />
                              )}
                              <span className="font-medium">{item.code}</span>
                              <span className="text-xs text-muted-foreground">({item.type})</span>
                            </div>
                            <div className="flex items-center gap-4 text-sm">
                              <span className="text-muted-foreground">
                                {item.impact} paths
                              </span>
                              <ImpactBadge {...item} />
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                </>
              )}
            </div>
          )}

          {/* Help text */}
          <div className="text-xs text-muted-foreground">
            <p className="mb-2"><strong>How to use:</strong></p>
            <ol className="list-decimal list-inside space-y-1">
              <li>Search and add devices or links you plan to take offline for maintenance</li>
              <li>Click "Analyze Impact" to see how the network will be affected</li>
              <li>Review the recommended order - take down least impactful items first</li>
              <li>Export the plan for reference during the maintenance window</li>
            </ol>
          </div>
        </div>
      </div>
    </div>
  )
}
