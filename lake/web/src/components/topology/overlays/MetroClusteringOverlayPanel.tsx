import { MapPin, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'

interface MetroInfo {
  code: string
  name: string
}

interface MetroClusteringOverlayPanelProps {
  metroInfoMap: Map<string, MetroInfo>
  collapsedMetros: Set<string>
  getMetroColor: (pk: string) => string
  getDeviceCountForMetro: (pk: string) => number
  totalDeviceCount: number
  onToggleMetroCollapse: (pk: string) => void
  onCollapseAll: () => void
  onExpandAll: () => void
  isLoading?: boolean
}

export function MetroClusteringOverlayPanel({
  metroInfoMap,
  collapsedMetros,
  getMetroColor,
  getDeviceCountForMetro,
  totalDeviceCount,
  onToggleMetroCollapse,
  onCollapseAll,
  onExpandAll,
  isLoading,
}: MetroClusteringOverlayPanelProps) {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <MapPin className="h-3.5 w-3.5 text-blue-500" />
          Metro Clustering
        </span>
        <button
          onClick={() => toggleOverlay('metroClustering')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading metro data...</div>
      )}

      {!isLoading && metroInfoMap.size > 0 && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Total Metros</span>
              <span className="font-medium">{metroInfoMap.size}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Devices</span>
              <span className="font-medium">{totalDeviceCount}</span>
            </div>
          </div>

          {/* Metro list with colors - clickable to collapse/expand */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Metros (click to collapse)</div>
            <div className="space-y-0.5">
              {Array.from(metroInfoMap.entries())
                .sort((a, b) => a[1].code.localeCompare(b[1].code))
                .map(([pk, info]) => {
                  const deviceCount = getDeviceCountForMetro(pk)
                  if (deviceCount === 0) return null
                  const isCollapsed = collapsedMetros.has(pk)
                  return (
                    <button
                      key={pk}
                      onClick={() => onToggleMetroCollapse(pk)}
                      className={`w-full flex items-center justify-between gap-2 px-1.5 py-1 rounded transition-colors ${
                        isCollapsed
                          ? 'bg-blue-500/20 border border-blue-500/30'
                          : 'hover:bg-[var(--muted)]'
                      }`}
                      title={isCollapsed ? 'Click to expand' : 'Click to collapse'}
                    >
                      <div className="flex items-center gap-1.5">
                        <div
                          className={`w-3 h-3 flex-shrink-0 ${isCollapsed ? 'rounded' : 'rounded-full'}`}
                          style={{ backgroundColor: getMetroColor(pk) }}
                        />
                        <span className="truncate">{info.code}</span>
                      </div>
                      <span className={isCollapsed ? 'text-blue-400 font-medium' : 'text-muted-foreground'}>
                        {isCollapsed ? `(${deviceCount})` : deviceCount}
                      </span>
                    </button>
                  )
                })}
            </div>
          </div>

          {/* Collapse all / Expand all buttons */}
          <div className="pt-2 border-t border-[var(--border)] flex gap-2">
            <button
              onClick={onCollapseAll}
              className="flex-1 px-2 py-1 bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded text-[10px]"
              disabled={collapsedMetros.size === metroInfoMap.size}
            >
              Collapse All
            </button>
            <button
              onClick={onExpandAll}
              className="flex-1 px-2 py-1 bg-[var(--muted)] hover:bg-[var(--muted)]/80 rounded text-[10px]"
              disabled={collapsedMetros.size === 0}
            >
              Expand All
            </button>
          </div>

          {/* Keyboard shortcut hint */}
          <div className="pt-2 border-t border-[var(--border)] text-muted-foreground">
            Press <kbd className="px-1 py-0.5 bg-[var(--muted)] rounded text-[10px]">m</kbd> to toggle
          </div>
        </div>
      )}
    </div>
  )
}
