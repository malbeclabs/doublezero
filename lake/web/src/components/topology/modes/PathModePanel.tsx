import { Route, X } from 'lucide-react'
import type { MultiPathResponse, PathMode } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'

// Path colors for K-shortest paths visualization
const PATH_COLORS = [
  { light: '#16a34a', dark: '#22c55e' },  // green - primary/shortest
  { light: '#2563eb', dark: '#3b82f6' },  // blue - alternate 1
  { light: '#9333ea', dark: '#a855f7' },  // purple - alternate 2
  { light: '#ea580c', dark: '#f97316' },  // orange - alternate 3
  { light: '#0891b2', dark: '#06b6d4' },  // cyan - alternate 4
]

interface PathModePanelProps {
  pathSource: string | null
  pathTarget: string | null
  pathsResult: MultiPathResponse | null
  pathLoading: boolean
  pathMode: PathMode
  selectedPathIndex: number
  onPathModeChange: (mode: PathMode) => void
  onSelectPath: (index: number) => void
  onClearPath: () => void
}

export function PathModePanel({
  pathSource,
  pathTarget,
  pathsResult,
  pathLoading,
  pathMode,
  selectedPathIndex,
  onPathModeChange,
  onSelectPath,
  onClearPath,
}: PathModePanelProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Route className="h-3.5 w-3.5 text-amber-500" />
          Path Finding
        </span>
        {(pathSource || pathTarget) && (
          <button onClick={onClearPath} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear path">
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
          title="Find path with fewest hops"
        >
          Fewest Hops
        </button>
        <button
          onClick={() => onPathModeChange('latency')}
          className={`flex-1 px-2 py-1 rounded text-xs transition-colors ${
            pathMode === 'latency' ? 'bg-[var(--card)] shadow-sm' : 'hover:bg-[var(--card)]/50'
          }`}
          title="Find path with lowest latency"
        >
          Lowest Latency
        </button>
      </div>

      {!pathSource && (
        <div className="text-muted-foreground">Click a device to set the <span className="text-green-500 font-medium">source</span></div>
      )}
      {pathSource && !pathTarget && (
        <div className="text-muted-foreground">Click another device to set the <span className="text-red-500 font-medium">target</span></div>
      )}
      {pathLoading && (
        <div className="text-muted-foreground">Finding paths...</div>
      )}
      {pathsResult && !pathsResult.error && pathsResult.paths.length > 0 && (
        <div>
          {/* Path selector - show if multiple paths */}
          {pathsResult.paths.length > 1 && (
            <div className="mb-2">
              <div className="text-muted-foreground mb-1">
                {pathsResult.paths.length} paths found
              </div>
              <div className="flex flex-wrap gap-1">
                {pathsResult.paths.map((_, i) => (
                  <button
                    key={i}
                    onClick={() => onSelectPath(i)}
                    className={`px-2 py-0.5 rounded text-[10px] font-medium transition-colors ${
                      selectedPathIndex === i
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                    }`}
                    style={{
                      borderLeft: `3px solid ${isDark ? PATH_COLORS[i % PATH_COLORS.length].dark : PATH_COLORS[i % PATH_COLORS.length].light}`,
                    }}
                  >
                    Path {i + 1}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Selected path details */}
          {pathsResult.paths[selectedPathIndex] && (
            <>
              <div className="space-y-1 text-muted-foreground">
                <div>Hops: <span className="text-foreground font-medium">{pathsResult.paths[selectedPathIndex].hopCount}</span></div>
                <div>Latency: <span className="text-foreground font-medium">{(pathsResult.paths[selectedPathIndex].totalMetric / 1000).toFixed(2)}ms</span></div>
              </div>
              <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-0.5">
                {pathsResult.paths[selectedPathIndex].path.map((hop, i) => (
                  <div key={hop.devicePK} className="flex items-center gap-1">
                    <span className="text-muted-foreground w-4">{i + 1}.</span>
                    <span className={i === 0 ? 'text-green-500' : i === pathsResult.paths[selectedPathIndex].path.length - 1 ? 'text-red-500' : 'text-foreground'}>
                      {hop.deviceCode}
                    </span>
                    {hop.edgeMetric !== undefined && hop.edgeMetric > 0 && (
                      <span className="text-muted-foreground text-[10px]">({(hop.edgeMetric / 1000).toFixed(1)}ms)</span>
                    )}
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      )}
      {pathsResult?.error && (
        <div className="text-destructive">{pathsResult.error}</div>
      )}
    </div>
  )
}
