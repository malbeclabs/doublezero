import { Zap, X, ArrowRight, AlertTriangle } from 'lucide-react'
import type { FailureImpactResponse } from '@/lib/api'

interface ImpactPanelProps {
  devicePK: string | null
  result: FailureImpactResponse | null
  isLoading: boolean
  onClose: () => void
}

// Convert ISIS metric (microseconds) to milliseconds for display
function metricToMs(metric: number): string {
  return (metric / 1000).toFixed(2)
}

export function ImpactPanel({ devicePK, result, isLoading, onClose }: ImpactPanelProps) {
  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Zap className="h-3.5 w-3.5 text-purple-500" />
          Device Failure
        </span>
        {devicePK && (
          <button onClick={onClose} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear">
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Show prompt when no device selected */}
      {!devicePK && !isLoading && (
        <div className="text-muted-foreground">
          Click a device to analyze what happens if it fails.
        </div>
      )}

      {isLoading && (
        <div className="text-muted-foreground">Analyzing impact...</div>
      )}

      {result && !result.error && (
        <div className="space-y-4">
          <div className="text-muted-foreground">
            If <span className="font-medium text-foreground">{result.deviceCode}</span> goes down:
          </div>

          {/* Unreachable Devices Section */}
          <div className="space-y-2">
            <div className="font-medium text-muted-foreground uppercase tracking-wider text-[10px]">
              Unreachable Devices
            </div>
            {result.unreachableCount === 0 ? (
              <div className="text-green-500 flex items-center gap-1.5">
                <div className="w-2 h-2 rounded-full bg-green-500" />
                None - all devices remain reachable
              </div>
            ) : (
              <div className="space-y-2">
                <div className="text-red-500 font-medium flex items-center gap-1.5">
                  <AlertTriangle className="h-3.5 w-3.5" />
                  {result.unreachableCount} device{result.unreachableCount !== 1 ? 's' : ''} would be isolated
                </div>
                <div className="space-y-0.5">
                  {result.unreachableDevices.map(device => (
                    <div key={device.pk} className="flex items-center gap-1.5 pl-1">
                      <div className={`w-2 h-2 rounded-full ${device.status === 'active' ? 'bg-green-500' : 'bg-red-500'}`} />
                      <span>{device.code}</span>
                      <span className="text-muted-foreground">({device.deviceType})</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* Affected Paths Section */}
          <div className="space-y-2">
            <div className="font-medium text-muted-foreground uppercase tracking-wider text-[10px]">
              Affected Paths
            </div>
            {result.affectedPathCount === 0 ? (
              <div className="text-green-500 flex items-center gap-1.5">
                <div className="w-2 h-2 rounded-full bg-green-500" />
                No paths would need to reroute
              </div>
            ) : (
              <div className="space-y-2">
                <div className="text-yellow-500 font-medium">
                  {result.affectedPathCount} path{result.affectedPathCount !== 1 ? 's' : ''} would reroute
                </div>
                <div className="space-y-2">
                  {result.affectedPaths.map((path, idx) => {
                    const hopDelta = path.hasAlternate ? path.afterHops - path.beforeHops : 0
                    const metricDelta = path.hasAlternate ? path.afterMetric - path.beforeMetric : 0

                    return (
                      <div key={idx} className="border border-border rounded p-2 space-y-1">
                        {/* Path endpoints */}
                        <div className="flex items-center gap-1 font-medium">
                          <span>{path.fromCode}</span>
                          <ArrowRight className="h-3 w-3 text-muted-foreground" />
                          <span>{path.toCode}</span>
                        </div>

                        {/* Before/After comparison */}
                        <div className="grid grid-cols-2 gap-2 text-muted-foreground">
                          <div>
                            <span className="text-[10px] uppercase tracking-wider">Before</span>
                            <div className="text-foreground">
                              {path.beforeHops} hop{path.beforeHops !== 1 ? 's' : ''}, {metricToMs(path.beforeMetric)}ms
                            </div>
                          </div>
                          <div>
                            <span className="text-[10px] uppercase tracking-wider">After</span>
                            {path.hasAlternate ? (
                              <div className="text-foreground">
                                {path.afterHops} hop{path.afterHops !== 1 ? 's' : ''}, {metricToMs(path.afterMetric)}ms
                              </div>
                            ) : (
                              <div className="text-red-500">No alternate</div>
                            )}
                          </div>
                        </div>

                        {/* Impact summary */}
                        {path.hasAlternate && (hopDelta > 0 || metricDelta > 0) && (
                          <div className="text-yellow-500 text-[10px]">
                            +{hopDelta} hop{hopDelta !== 1 ? 's' : ''}, +{metricToMs(metricDelta)}ms latency
                          </div>
                        )}
                        {!path.hasAlternate && (
                          <div className="text-red-500 text-[10px] flex items-center gap-1">
                            <AlertTriangle className="h-3 w-3" />
                            Connection would be lost
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {result?.error && (
        <div className="text-destructive">{result.error}</div>
      )}
    </div>
  )
}
