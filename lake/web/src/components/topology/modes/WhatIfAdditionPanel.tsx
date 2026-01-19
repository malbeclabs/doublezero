import { PlusCircle, X, Shield } from 'lucide-react'
import type { SimulateLinkAdditionResponse } from '@/lib/api'

interface WhatIfAdditionPanelProps {
  additionSource: string | null
  additionTarget: string | null
  additionMetric: number
  result: SimulateLinkAdditionResponse | null
  isLoading: boolean
  onMetricChange: (metric: number) => void
  onClear: () => void
}

export function WhatIfAdditionPanel({
  additionSource,
  additionTarget,
  additionMetric,
  result,
  isLoading,
  onMetricChange,
  onClear,
}: WhatIfAdditionPanelProps) {
  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <PlusCircle className="h-3.5 w-3.5 text-green-500" />
          Simulate Link Addition
        </span>
        {(additionSource || additionTarget) && (
          <button
            onClick={onClear}
            className="p-1 hover:bg-[var(--muted)] rounded"
            title="Clear"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Metric input */}
      <div className="mb-3">
        <div className="text-muted-foreground mb-1.5">Link Latency</div>
        <div className="flex gap-1">
          {[1000, 5000, 10000, 50000].map(m => (
            <button
              key={m}
              onClick={() => onMetricChange(m)}
              className={`px-2 py-1 rounded text-[10px] transition-colors ${
                additionMetric === m
                  ? 'bg-green-500/20 text-green-500'
                  : 'bg-muted hover:bg-muted/80 text-muted-foreground'
              }`}
            >
              {m / 1000}ms
            </button>
          ))}
        </div>
      </div>

      {!additionSource && (
        <div className="text-muted-foreground">Click a device to set the <span className="text-green-500 font-medium">source</span></div>
      )}
      {additionSource && !additionTarget && (
        <div className="text-muted-foreground">Click another device to set the <span className="text-red-500 font-medium">target</span></div>
      )}

      {isLoading && (
        <div className="text-muted-foreground">Analyzing benefits...</div>
      )}

      {result && !result.error && (
        <div className="space-y-3">
          <div className="text-muted-foreground">
            New link: <span className="font-medium text-green-500">{result.sourceCode}</span> — <span className="font-medium text-red-500">{result.targetCode}</span>
          </div>

          {/* Link already exists warning */}
          {result.error === 'Link already exists between these devices' && (
            <div className="p-2 bg-amber-500/10 border border-amber-500/30 rounded text-amber-500">
              Link already exists
            </div>
          )}

          {/* Redundancy gains */}
          {result.redundancyCount > 0 && (
            <div className="space-y-1">
              <div className="text-cyan-500 font-medium flex items-center gap-1.5">
                <Shield className="h-3 w-3" />
                {result.redundancyCount} device{result.redundancyCount !== 1 ? 's' : ''} would gain redundancy
              </div>
              <div className="space-y-0.5">
                {result.redundancyGains.map(gain => (
                  <div key={gain.devicePK} className="flex items-center gap-1.5 text-cyan-400">
                    <div className="w-2 h-2 rounded-full bg-cyan-500" />
                    <span>{gain.deviceCode}</span>
                    {gain.wasLeaf && <span className="text-[10px] text-muted-foreground">(was leaf)</span>}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Improved paths */}
          {result.improvedPathCount > 0 && (
            <div className="space-y-1 pt-2 border-t border-[var(--border)]">
              <div className="text-green-500 font-medium">
                {result.improvedPathCount} path{result.improvedPathCount !== 1 ? 's' : ''} would improve
              </div>
              <div className="space-y-1">
                {result.improvedPaths.slice(0, 5).map((path, i) => (
                  <div key={i} className="text-muted-foreground">
                    <span className="text-foreground">{path.fromCode}</span> → <span className="text-foreground">{path.toCode}</span>
                    <div className="ml-2 text-[10px] text-green-500">
                      {path.beforeHops} → {path.afterHops} hops (-{path.hopReduction})
                    </div>
                  </div>
                ))}
                {result.improvedPathCount > 5 && (
                  <div className="text-muted-foreground">+{result.improvedPathCount - 5} more</div>
                )}
              </div>
            </div>
          )}

          {result.redundancyCount === 0 && result.improvedPathCount === 0 && (
            <div className="text-muted-foreground flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-muted-foreground" />
              No significant improvements
            </div>
          )}

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Legend</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full border-2 border-green-500" />
                <span>Source device</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full border-2 border-red-500" />
                <span>Target device</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full border-2 border-cyan-500" />
                <span>Gains redundancy</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {result?.error && result.error !== 'Link already exists between these devices' && (
        <div className="text-destructive">{result.error}</div>
      )}
    </div>
  )
}
