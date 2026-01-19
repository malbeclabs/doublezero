import { MinusCircle, X, AlertTriangle } from 'lucide-react'
import type { SimulateLinkRemovalResponse } from '@/lib/api'

interface WhatIfRemovalPanelProps {
  removalLink: { sourcePK: string; targetPK: string } | null
  result: SimulateLinkRemovalResponse | null
  isLoading: boolean
  onClear: () => void
}

export function WhatIfRemovalPanel({ removalLink, result, isLoading, onClear }: WhatIfRemovalPanelProps) {
  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <MinusCircle className="h-3.5 w-3.5 text-red-500" />
          Simulate Link Removal
        </span>
        {removalLink && (
          <button
            onClick={onClear}
            className="p-1 hover:bg-[var(--muted)] rounded"
            title="Clear"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {!removalLink && (
        <div className="text-muted-foreground">Click a link to simulate its removal</div>
      )}

      {isLoading && (
        <div className="text-muted-foreground">Analyzing impact...</div>
      )}

      {result && !result.error && (
        <div className="space-y-3">
          <div className="text-muted-foreground">
            Removing link: <span className="font-medium text-foreground">{result.sourceCode}</span> — <span className="font-medium text-foreground">{result.targetCode}</span>
          </div>

          {/* Partition warning */}
          {result.causesPartition && (
            <div className="p-2 bg-red-500/10 border border-red-500/30 rounded text-red-500 flex items-center gap-1.5">
              <AlertTriangle className="h-3.5 w-3.5" />
              <span className="font-medium">Would cause network partition!</span>
            </div>
          )}

          {/* Disconnected devices */}
          {result.disconnectedCount > 0 && (
            <div className="space-y-1">
              <div className="text-red-500 font-medium">
                {result.disconnectedCount} device{result.disconnectedCount !== 1 ? 's' : ''} would become unreachable
              </div>
              <div className="space-y-0.5">
                {result.disconnectedDevices.slice(0, 5).map(device => (
                  <div key={device.pk} className="flex items-center gap-1.5 text-red-400">
                    <div className="w-2 h-2 rounded-full bg-red-500" />
                    <span>{device.code}</span>
                  </div>
                ))}
                {result.disconnectedCount > 5 && (
                  <div className="text-muted-foreground">+{result.disconnectedCount - 5} more</div>
                )}
              </div>
            </div>
          )}

          {/* Affected paths */}
          {result.affectedPathCount > 0 && (
            <div className="space-y-1 pt-2 border-t border-[var(--border)]">
              <div className="text-amber-500 font-medium">
                {result.affectedPathCount} path{result.affectedPathCount !== 1 ? 's' : ''} affected
              </div>
              <div className="space-y-1">
                {result.affectedPaths.slice(0, 5).map((path, i) => (
                  <div key={i} className="text-muted-foreground">
                    <span className="text-foreground">{path.fromCode}</span> → <span className="text-foreground">{path.toCode}</span>
                    <div className="ml-2 text-[10px]">
                      {path.hasAlternate ? (
                        <span className="text-amber-500">
                          {path.beforeHops} → {path.afterHops} hops (+{path.afterHops - path.beforeHops})
                          {path.beforeMetric > 0 && path.afterMetric > 0 && (
                            <span className="ml-1 text-muted-foreground">
                              | {(path.afterMetric / 1000).toFixed(1)}ms (+{((path.afterMetric - path.beforeMetric) / 1000).toFixed(1)}ms)
                            </span>
                          )}
                        </span>
                      ) : (
                        <span className="text-red-500">No alternate path</span>
                      )}
                    </div>
                  </div>
                ))}
                {result.affectedPathCount > 5 && (
                  <div className="text-muted-foreground">+{result.affectedPathCount - 5} more</div>
                )}
              </div>
            </div>
          )}

          {result.disconnectedCount === 0 && result.affectedPathCount === 0 && (
            <div className="text-green-500 flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-green-500" />
              Safe to remove - no impact
            </div>
          )}

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Legend</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-red-500" style={{ borderTop: '2px dashed #ef4444' }} />
                <span>Removed link</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full border-2 border-red-500" />
                <span>Disconnected device</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {result?.error && (
        <div className="text-destructive">{result.error}</div>
      )}
    </div>
  )
}
