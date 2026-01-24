import { Route, X, ArrowRightLeft } from 'lucide-react'
import type { MultiPathResponse, PathMode } from '@/lib/api'
import { useTheme } from '@/hooks/use-theme'
import { DeviceSelector, type DeviceOption } from '../DeviceSelector'

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
  devices: DeviceOption[]
  showReverse: boolean
  reversePathsResult: MultiPathResponse | null
  reversePathLoading: boolean
  selectedReversePathIndex: number
  onPathModeChange: (mode: PathMode) => void
  onSelectPath: (index: number) => void
  onSelectReversePath: (index: number) => void
  onClearPath: () => void
  onSetSource: (pk: string | null) => void
  onSetTarget: (pk: string | null) => void
  onToggleReverse: () => void
}

export function PathModePanel({
  pathSource,
  pathTarget,
  pathsResult,
  pathLoading,
  pathMode,
  selectedPathIndex,
  devices,
  showReverse,
  reversePathsResult,
  reversePathLoading,
  selectedReversePathIndex,
  onPathModeChange,
  onSelectPath,
  onSelectReversePath,
  onClearPath,
  onSetSource,
  onSetTarget,
  onToggleReverse,
}: PathModePanelProps) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  // Get source and target device codes for labels
  const sourceDevice = devices.find(d => d.pk === pathSource)
  const targetDevice = devices.find(d => d.pk === pathTarget)

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

      {/* Device selectors */}
      <div className="space-y-2 mb-3">
        <DeviceSelector
          devices={devices}
          value={pathSource}
          onChange={onSetSource}
          placeholder="Search source device..."
          label="Source"
          labelColor="#22c55e"
        />
        <DeviceSelector
          devices={devices.filter(d => d.pk !== pathSource)}
          value={pathTarget}
          onChange={onSetTarget}
          placeholder="Search target device..."
          label="Target"
          labelColor="#ef4444"
          disabled={!pathSource}
        />
      </div>

      {/* Show reverse toggle */}
      {pathSource && pathTarget && (
        <label className="flex items-center gap-2 mb-3 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={showReverse}
            onChange={onToggleReverse}
            className="rounded border-[var(--border)]"
          />
          <span className="text-muted-foreground flex items-center gap-1">
            <ArrowRightLeft className="h-3 w-3" />
            Show reverse path
          </span>
        </label>
      )}

      {!pathSource && (
        <div className="text-muted-foreground text-[10px]">Or click a device on the map</div>
      )}
      {pathLoading && (
        <div className="text-muted-foreground">Finding paths...</div>
      )}

      {/* Path results - side by side when reverse is enabled */}
      {pathsResult && !pathsResult.error && pathsResult.paths.length > 0 && (
        <div className={showReverse ? 'grid grid-cols-2 gap-3' : ''}>
          {/* Forward path: Source → Target */}
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1">
              {sourceDevice?.code || 'Source'} → {targetDevice?.code || 'Target'}
            </div>

            {/* Path selector - show if multiple paths */}
            {pathsResult.paths.length > 1 && (
              <div className="mb-2">
                <div className="text-muted-foreground mb-1">
                  {pathsResult.paths.length} paths
                </div>
                <div className="flex flex-wrap gap-1">
                  {pathsResult.paths.map((_, i) => (
                    <button
                      key={i}
                      onClick={() => onSelectPath(i)}
                      className={`px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors ${
                        selectedPathIndex === i
                          ? 'bg-primary text-primary-foreground'
                          : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                      }`}
                      style={{
                        borderLeft: `3px solid ${isDark ? PATH_COLORS[i % PATH_COLORS.length].dark : PATH_COLORS[i % PATH_COLORS.length].light}`,
                      }}
                    >
                      {i + 1}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* Selected path details */}
            {pathsResult.paths[selectedPathIndex] && (
              <>
                <div className="space-y-0.5 text-muted-foreground text-[11px]">
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

          {/* Reverse path: Target → Source */}
          {showReverse && (
            <div className="border-l border-[var(--border)] pl-3">
              <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-1">
                {targetDevice?.code || 'Target'} → {sourceDevice?.code || 'Source'}
              </div>

              {reversePathLoading && (
                <div className="text-muted-foreground text-[11px]">Finding...</div>
              )}

              {reversePathsResult && !reversePathsResult.error && reversePathsResult.paths.length > 0 && (
                <>
                  {/* Reverse path selector - show if multiple paths */}
                  {reversePathsResult.paths.length > 1 && (
                    <div className="mb-2">
                      <div className="text-muted-foreground mb-1">
                        {reversePathsResult.paths.length} paths
                      </div>
                      <div className="flex flex-wrap gap-1">
                        {reversePathsResult.paths.map((_, i) => (
                          <button
                            key={i}
                            onClick={() => onSelectReversePath(i)}
                            className={`px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors ${
                              selectedReversePathIndex === i
                                ? 'bg-primary text-primary-foreground'
                                : 'bg-muted hover:bg-muted/80 text-muted-foreground'
                            }`}
                            style={{
                              borderLeft: `3px solid ${isDark ? PATH_COLORS[i % PATH_COLORS.length].dark : PATH_COLORS[i % PATH_COLORS.length].light}`,
                            }}
                          >
                            {i + 1}
                          </button>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Selected reverse path details */}
                  {reversePathsResult.paths[selectedReversePathIndex] && (
                    <>
                      <div className="space-y-0.5 text-muted-foreground text-[11px]">
                        <div>Hops: <span className="text-foreground font-medium">{reversePathsResult.paths[selectedReversePathIndex].hopCount}</span></div>
                        <div>Latency: <span className="text-foreground font-medium">{(reversePathsResult.paths[selectedReversePathIndex].totalMetric / 1000).toFixed(2)}ms</span></div>
                      </div>
                      <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-0.5">
                        {reversePathsResult.paths[selectedReversePathIndex].path.map((hop, i) => (
                          <div key={hop.devicePK} className="flex items-center gap-1">
                            <span className="text-muted-foreground w-4">{i + 1}.</span>
                            <span className={i === 0 ? 'text-red-500' : i === reversePathsResult.paths[selectedReversePathIndex].path.length - 1 ? 'text-green-500' : 'text-foreground'}>
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
                </>
              )}

              {reversePathsResult?.error && (
                <div className="text-destructive text-[11px]">{reversePathsResult.error}</div>
              )}
            </div>
          )}
        </div>
      )}

      {pathsResult?.error && (
        <div className="text-destructive">{pathsResult.error}</div>
      )}
    </div>
  )
}
