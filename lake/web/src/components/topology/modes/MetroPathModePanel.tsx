import { Building2, X, ArrowLeft } from 'lucide-react'
import type { MetroDevicePathsResponse, PathMode } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'
import { MetroSelector, type MetroOption } from '../MetroSelector'

// Path colors for comparison
const PATH_COLORS = [
  { light: '#16a34a', dark: '#22c55e' },  // green
  { light: '#2563eb', dark: '#3b82f6' },  // blue
  { light: '#9333ea', dark: '#a855f7' },  // purple
  { light: '#ea580c', dark: '#f97316' },  // orange
  { light: '#0891b2', dark: '#06b6d4' },  // cyan
]

interface MetroPathModePanelProps {
  sourceMetro: string | null
  targetMetro: string | null
  metros: MetroOption[]
  pathsResult: MetroDevicePathsResponse | null
  loading: boolean
  pathMode: PathMode
  viewMode: 'aggregate' | 'drilldown'
  selectedPairIndices: number[]
  onSetSourceMetro: (pk: string | null) => void
  onSetTargetMetro: (pk: string | null) => void
  onPathModeChange: (mode: PathMode) => void
  onViewModeChange: (mode: 'aggregate' | 'drilldown') => void
  onTogglePair: (index: number) => void
  onClearSelection: () => void
  onClear: () => void
}

// Latency color scale (green to red)
function getLatencyColor(latencyMs: number, minLatencyMs: number, maxLatencyMs: number, isDark: boolean): string {
  if (maxLatencyMs === minLatencyMs) {
    return isDark ? '#22c55e' : '#16a34a' // green if all same
  }
  const ratio = (latencyMs - minLatencyMs) / (maxLatencyMs - minLatencyMs)
  // Green -> Yellow -> Red
  if (ratio <= 0.5) {
    // Green to Yellow
    const g = isDark ? 197 : 163
    const r = Math.round(ratio * 2 * (isDark ? 250 : 234))
    return `rgb(${r}, ${g}, ${isDark ? 94 : 80})`
  } else {
    // Yellow to Red
    const adjusted = (ratio - 0.5) * 2
    const g = Math.round((1 - adjusted) * (isDark ? 197 : 163))
    return `rgb(${isDark ? 239 : 220}, ${g}, ${isDark ? 68 : 56})`
  }
}

export function MetroPathModePanel({
  sourceMetro,
  targetMetro,
  metros,
  pathsResult,
  loading,
  pathMode,
  viewMode,
  selectedPairIndices,
  onSetSourceMetro,
  onSetTargetMetro,
  onPathModeChange,
  onViewModeChange,
  onTogglePair,
  onClearSelection,
  onClear,
}: MetroPathModePanelProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  // Get source and target metro info for labels
  const sourceMetroInfo = metros.find(m => m.pk === sourceMetro)
  const targetMetroInfo = metros.find(m => m.pk === targetMetro)

  // Get selected pairs for drilldown
  const selectedPairs = selectedPairIndices
    .map(idx => pathsResult?.devicePairs[idx])
    .filter((p): p is NonNullable<typeof p> => p !== undefined)

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Building2 className="h-3.5 w-3.5 text-cyan-500" />
          Metro Path Finding
        </span>
        {(sourceMetro || targetMetro) && (
          <button onClick={onClear} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear">
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Mode toggle */}
      <div className="flex gap-1 mb-3 p-0.5 bg-[var(--muted)] rounded">
        <button
          onClick={() => onPathModeChange('hops')}
          className={`flex-1 px-2 py-1 rounded text-xs transition-colors ${
            pathMode === 'hops' ? 'bg-[var(--card)] shadow-sm' : 'hover:bg-[var(--card)]/50'
          }`}
          title="Find paths with fewest hops"
        >
          Fewest Hops
        </button>
        <button
          onClick={() => onPathModeChange('latency')}
          className={`flex-1 px-2 py-1 rounded text-xs transition-colors ${
            pathMode === 'latency' ? 'bg-[var(--card)] shadow-sm' : 'hover:bg-[var(--card)]/50'
          }`}
          title="Find paths with lowest latency"
        >
          Lowest Latency
        </button>
      </div>

      {/* Metro selectors */}
      <div className="space-y-2 mb-3">
        <MetroSelector
          metros={metros}
          value={sourceMetro}
          onChange={onSetSourceMetro}
          placeholder="Search source metro..."
          label="Source Metro"
          labelColor="#22c55e"
        />
        <MetroSelector
          metros={metros.filter(m => m.pk !== sourceMetro)}
          value={targetMetro}
          onChange={onSetTargetMetro}
          placeholder="Search target metro..."
          label="Target Metro"
          labelColor="#ef4444"
          disabled={!sourceMetro}
        />
      </div>

      {!sourceMetro && (
        <div className="text-muted-foreground text-[10px]">Select a source and target metro to find paths</div>
      )}

      {loading && (
        <div className="text-muted-foreground">Finding paths...</div>
      )}

      {pathsResult?.error && (
        <div className="text-destructive">{pathsResult.error}</div>
      )}

      {/* Results */}
      {pathsResult && !pathsResult.error && pathsResult.devicePairs.length > 0 && (
        <>
          {viewMode === 'aggregate' ? (
            /* Aggregate view */
            <div>
              {/* Summary stats */}
              <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1.5">
                {sourceMetroInfo?.code || 'Source'} → {targetMetroInfo?.code || 'Target'}
              </div>

              <div className="grid grid-cols-2 gap-x-3 gap-y-1 mb-3 text-[11px]">
                <div>
                  <span className="text-muted-foreground">Source devices: </span>
                  <span className="font-medium">{pathsResult.sourceDeviceCount}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Target devices: </span>
                  <span className="font-medium">{pathsResult.targetDeviceCount}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Total pairs: </span>
                  <span className="font-medium">{pathsResult.totalPairs}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Hop range: </span>
                  <span className="font-medium">
                    {pathsResult.minHops === pathsResult.maxHops
                      ? pathsResult.minHops
                      : `${pathsResult.minHops}-${pathsResult.maxHops}`}
                  </span>
                </div>
                <div className="col-span-2">
                  <span className="text-muted-foreground">Latency: </span>
                  <span className="font-medium">
                    {pathsResult.minLatencyMs.toFixed(2)} - {pathsResult.maxLatencyMs.toFixed(2)}ms
                  </span>
                  <span className="text-muted-foreground"> (avg {pathsResult.avgLatencyMs.toFixed(2)}ms)</span>
                </div>
              </div>

              {/* Device pairs list */}
              <div className="border-t border-[var(--border)] pt-2 space-y-1">
                <div className="flex items-center justify-between mb-1">
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wider">
                    Device Pairs (click to select, compare up to 5)
                  </div>
                  {selectedPairIndices.length > 0 && (
                    <button
                      onClick={onClearSelection}
                      className="text-[10px] text-muted-foreground hover:text-foreground"
                    >
                      Clear ({selectedPairIndices.length})
                    </button>
                  )}
                </div>

                {/* Compare button */}
                {selectedPairIndices.length >= 2 && (
                  <button
                    onClick={() => onViewModeChange('drilldown')}
                    className="w-full mb-2 px-2 py-1.5 rounded bg-cyan-500/20 text-cyan-500 hover:bg-cyan-500/30 transition-colors font-medium"
                  >
                    Compare {selectedPairIndices.length} Paths
                  </button>
                )}

                <div className="max-h-48 overflow-y-auto space-y-0.5">
                  {pathsResult.devicePairs.map((pair, index) => {
                    const latencyMs = pair.bestPath.measuredLatencyMs || pair.bestPath.totalMetric / 1000
                    const bgColor = getLatencyColor(
                      latencyMs,
                      pathsResult.minLatencyMs,
                      pathsResult.maxLatencyMs,
                      isDark
                    )
                    const isSelected = selectedPairIndices.includes(index)
                    const selectionIndex = selectedPairIndices.indexOf(index)
                    const selectionColor = selectionIndex >= 0
                      ? (isDark ? PATH_COLORS[selectionIndex % PATH_COLORS.length].dark : PATH_COLORS[selectionIndex % PATH_COLORS.length].light)
                      : undefined

                    return (
                      <button
                        key={`${pair.sourceDevicePK}-${pair.targetDevicePK}`}
                        onClick={() => onTogglePair(index)}
                        className={`w-full text-left px-2 py-1 rounded flex items-center gap-2 transition-colors ${
                          isSelected
                            ? 'bg-[var(--muted)]'
                            : 'hover:bg-[var(--muted)]'
                        }`}
                        style={isSelected ? { borderLeft: `3px solid ${selectionColor}` } : undefined}
                      >
                        <div
                          className="w-2 h-2 rounded-sm flex-shrink-0"
                          style={{ backgroundColor: bgColor }}
                          title={`${latencyMs.toFixed(2)}ms`}
                        />
                        <span className="font-medium">{pair.sourceDeviceCode}</span>
                        <span className="text-muted-foreground">→</span>
                        <span className="font-medium">{pair.targetDeviceCode}</span>
                        <span className="text-muted-foreground text-[10px] ml-auto">
                          {pair.bestPath.hopCount} hops, {latencyMs.toFixed(1)}ms
                        </span>
                      </button>
                    )
                  })}
                </div>
              </div>
            </div>
          ) : (
            /* Drilldown / Comparison view */
            <div>
              <button
                onClick={() => onViewModeChange('aggregate')}
                className="flex items-center gap-1 text-muted-foreground hover:text-foreground mb-2"
              >
                <ArrowLeft className="h-3 w-3" />
                Back to summary
              </button>

              {selectedPairs.length === 0 ? (
                <div className="text-muted-foreground">No paths selected</div>
              ) : selectedPairs.length === 1 ? (
                /* Single path view */
                <SinglePathView pair={selectedPairs[0]} isDark={isDark} colorIndex={0} />
              ) : (
                /* Side-by-side comparison */
                <div className="space-y-3">
                  <div className="text-[10px] text-muted-foreground uppercase tracking-wider">
                    Comparing {selectedPairs.length} Paths
                  </div>
                  <div
                    className="grid gap-3"
                    style={{ gridTemplateColumns: `repeat(${Math.min(selectedPairs.length, 3)}, 1fr)` }}
                  >
                    {selectedPairs.map((pair, idx) => (
                      <SinglePathView
                        key={`${pair.sourceDevicePK}-${pair.targetDevicePK}`}
                        pair={pair}
                        isDark={isDark}
                        colorIndex={idx}
                        compact={selectedPairs.length > 2}
                      />
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {pathsResult && !pathsResult.error && pathsResult.devicePairs.length === 0 && (
        <div className="text-muted-foreground">No paths found between these metros</div>
      )}
    </div>
  )
}

// Single path display component
function SinglePathView({
  pair,
  isDark,
  colorIndex,
  compact = false,
}: {
  pair: NonNullable<MetroDevicePathsResponse['devicePairs'][number]>
  isDark: boolean
  colorIndex: number
  compact?: boolean
}) {
  const color = isDark ? PATH_COLORS[colorIndex % PATH_COLORS.length].dark : PATH_COLORS[colorIndex % PATH_COLORS.length].light
  const latencyMs = pair.bestPath.measuredLatencyMs || pair.bestPath.totalMetric / 1000

  return (
    <div
      className="border border-[var(--border)] rounded p-2"
      style={{ borderLeftColor: color, borderLeftWidth: 3 }}
    >
      <div className={`font-medium mb-1 ${compact ? 'text-[10px]' : 'text-[11px]'}`}>
        {pair.sourceDeviceCode} → {pair.targetDeviceCode}
      </div>

      <div className={`text-muted-foreground mb-2 ${compact ? 'text-[9px]' : 'text-[10px]'}`}>
        {pair.bestPath.hopCount} hops, {latencyMs.toFixed(2)}ms total
      </div>

      {/* Hop-by-hop breakdown */}
      <div className={`space-y-0.5 ${compact ? 'text-[9px]' : 'text-[10px]'}`}>
        {pair.bestPath.path.map((hop, i) => {
          const isLastHop = i === pair.bestPath.path.length - 1
          // Edge latency is stored on the DESTINATION hop, so look at next hop for latency from this hop
          const nextHop = !isLastHop ? pair.bestPath.path[i + 1] : null
          const hopLatencyMs = nextHop?.edgeMeasuredMs ?? (nextHop?.edgeMetric ? nextHop.edgeMetric / 1000 : null)

          return (
            <div key={hop.devicePK} className="flex items-center gap-1">
              <span className="text-muted-foreground w-3 flex-shrink-0">{i + 1}.</span>
              <span
                className={
                  i === 0
                    ? 'text-green-500'
                    : isLastHop
                      ? 'text-red-500'
                      : 'text-foreground'
                }
              >
                {hop.deviceCode}
              </span>
              {/* Show latency to next hop */}
              {!isLastHop && hopLatencyMs !== null && hopLatencyMs > 0 && (
                <span className="text-muted-foreground ml-auto">
                  {hopLatencyMs.toFixed(1)}ms
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
