import { useState } from 'react'
import { Zap, X, AlertTriangle, MapPin, ChevronDown, ChevronRight, ArrowRight } from 'lucide-react'
import type { MaintenanceImpactResponse } from '@/lib/api'

interface ImpactPanelProps {
  devicePKs: string[]
  deviceCodes: Map<string, string>  // PK -> code mapping for display
  result: MaintenanceImpactResponse | null
  isLoading: boolean
  onRemoveDevice: (pk: string) => void
  onClear: () => void
}

export function ImpactPanel({ devicePKs, deviceCodes, result, isLoading, onRemoveDevice, onClear }: ImpactPanelProps) {
  const hasDevices = devicePKs.length > 0
  const [expandedItems, setExpandedItems] = useState<Set<string>>(new Set())
  const [showAffectedPaths, setShowAffectedPaths] = useState(false)

  const toggleExpanded = (pk: string) => {
    setExpandedItems(prev => {
      const next = new Set(prev)
      if (next.has(pk)) {
        next.delete(pk)
      } else {
        next.add(pk)
      }
      return next
    })
  }

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Zap className="h-3.5 w-3.5 text-purple-500" />
          Device Failure
        </span>
        {hasDevices && (
          <button onClick={onClear} className="p-1 hover:bg-[var(--muted)] rounded" title="Clear all">
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Show prompt when no device selected */}
      {!hasDevices && !isLoading && (
        <div className="text-muted-foreground">
          Click devices to analyze what happens if they fail. You can select multiple devices.
        </div>
      )}

      {/* Selected devices list */}
      {hasDevices && (
        <div className="mb-3 space-y-1">
          <div className="text-muted-foreground text-[10px] uppercase tracking-wider">
            Selected Devices ({devicePKs.length})
          </div>
          <div className="flex flex-wrap gap-1">
            {devicePKs.map(pk => (
              <span
                key={pk}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 bg-purple-500/20 text-purple-500 rounded text-[11px]"
              >
                {deviceCodes.get(pk) || pk}
                <button
                  onClick={() => onRemoveDevice(pk)}
                  className="hover:bg-purple-500/30 rounded p-0.5"
                  title="Remove"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              </span>
            ))}
          </div>
        </div>
      )}

      {isLoading && (
        <div className="text-muted-foreground">Analyzing impact...</div>
      )}

      {result && !result.error && hasDevices && (
        <div className="space-y-4">
          <div className="text-muted-foreground">
            If {devicePKs.length === 1 ? (
              <span className="font-medium text-foreground">{deviceCodes.get(devicePKs[0]) || devicePKs[0]}</span>
            ) : (
              <span className="font-medium text-foreground">{devicePKs.length} devices</span>
            )} go{devicePKs.length === 1 ? 'es' : ''} down:
          </div>

          {/* Summary stats */}
          <div className="grid grid-cols-2 gap-2">
            <div className="border border-border rounded p-2">
              <div className="text-[10px] uppercase tracking-wider text-muted-foreground">Disconnected</div>
              <div className={`text-lg font-medium ${result.totalDisconnected > 0 ? 'text-red-500' : 'text-green-500'}`}>
                {result.totalDisconnected}
              </div>
            </div>
            <div className="border border-border rounded p-2">
              <div className="text-[10px] uppercase tracking-wider text-muted-foreground">Affected Paths</div>
              <div className={`text-lg font-medium ${result.totalImpact > 0 ? 'text-yellow-500' : 'text-green-500'}`}>
                {result.totalImpact}
              </div>
            </div>
          </div>

          {/* Per-device breakdown */}
          {result.items.length > 0 && (
            <div className="space-y-2">
              <div className="font-medium text-muted-foreground uppercase tracking-wider text-[10px]">
                Per-Device Impact
              </div>
              <div className="space-y-1">
                {result.items.map(item => {
                  const hasDisconnected = item.disconnectedDevices && item.disconnectedDevices.length > 0
                  const hasImpact = item.impact > 0 || item.disconnected > 0
                  const isExpanded = expandedItems.has(item.pk)

                  return (
                    <div key={item.pk} className="border border-border rounded overflow-hidden">
                      <button
                        onClick={() => hasImpact && toggleExpanded(item.pk)}
                        className={`w-full flex items-center justify-between text-[11px] px-2 py-1.5 ${hasImpact ? 'hover:bg-[var(--muted)] cursor-pointer' : 'cursor-default'}`}
                        disabled={!hasImpact}
                      >
                        <span className="flex items-center gap-1">
                          {hasImpact ? (
                            isExpanded ? (
                              <ChevronDown className="h-3 w-3 text-muted-foreground" />
                            ) : (
                              <ChevronRight className="h-3 w-3 text-muted-foreground" />
                            )
                          ) : (
                            <span className="w-3" />
                          )}
                          <span className="font-medium">{item.code}</span>
                        </span>
                        <span className="text-muted-foreground">
                          {item.disconnected > 0 && (
                            <span className="text-red-500">{item.disconnected} disconnected</span>
                          )}
                          {item.disconnected > 0 && item.impact > 0 && ' · '}
                          {item.impact > 0 && (
                            <span className="text-yellow-500">{item.impact} paths</span>
                          )}
                          {item.disconnected === 0 && item.impact === 0 && (
                            <span className="text-green-500">No impact</span>
                          )}
                        </span>
                      </button>
                      {isExpanded && hasImpact && (
                        <div className="border-t border-border bg-[var(--muted)]/30 px-2 py-1.5 space-y-2">
                          {hasDisconnected && (
                            <div>
                              <div className="text-[10px] text-muted-foreground mb-1">Disconnected devices:</div>
                              <div className="space-y-0.5 max-h-24 overflow-y-auto">
                                {item.disconnectedDevices!.map(deviceCode => (
                                  <div key={deviceCode} className="flex items-center gap-1.5 text-[11px]">
                                    <div className="w-1.5 h-1.5 rounded-full bg-red-500" />
                                    <span>{deviceCode}</span>
                                  </div>
                                ))}
                              </div>
                            </div>
                          )}
                          {item.affectedPaths && item.affectedPaths.length > 0 && (
                            <div>
                              <div className="text-[10px] text-muted-foreground mb-1">Affected paths:</div>
                              <div className="space-y-1.5 max-h-40 overflow-y-auto">
                                {item.affectedPaths.map((path, idx) => (
                                  <div key={idx} className="text-[11px]">
                                    <div className="flex items-center gap-1.5">
                                      <span>{path.source}</span>
                                      <ArrowRight className="h-2.5 w-2.5 text-muted-foreground flex-shrink-0" />
                                      <span>{path.target}</span>
                                      <span className={`text-[10px] px-1 rounded flex-shrink-0 ${
                                        path.status === 'disconnected' ? 'bg-red-500/20 text-red-500' :
                                        path.status === 'degraded' ? 'bg-yellow-500/20 text-yellow-500' :
                                        'bg-blue-500/20 text-blue-500'
                                      }`}>
                                        {path.status}
                                      </span>
                                    </div>
                                    {path.status !== 'disconnected' && (path.hopsAfter !== path.hopsBefore || path.metricAfter !== path.metricBefore) && (
                                      <div className="text-[10px] text-muted-foreground pl-1 mt-0.5">
                                        {path.hopsAfter !== path.hopsBefore && (
                                          <span>+{path.hopsAfter - path.hopsBefore} hop{Math.abs(path.hopsAfter - path.hopsBefore) !== 1 ? 's' : ''}</span>
                                        )}
                                        {path.hopsAfter !== path.hopsBefore && path.metricAfter !== path.metricBefore && ', '}
                                        {path.metricAfter !== path.metricBefore && (
                                          <span>
                                            {path.metricAfter > path.metricBefore ? '+' : ''}
                                            {((path.metricAfter - path.metricBefore) / 1000).toFixed(1)}ms
                                          </span>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                ))}
                              </div>
                              {item.affectedPaths.length >= 10 && (
                                <div className="text-[10px] text-muted-foreground mt-1 italic">
                                  Showing first 10 paths
                                </div>
                              )}
                            </div>
                          )}
                          {item.impact > 0 && (!item.affectedPaths || item.affectedPaths.length === 0) && (
                            <div className="text-[11px] text-yellow-600">
                              {item.impact} path{item.impact !== 1 ? 's' : ''} would need to be rerouted
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Affected paths (collapsible) - show if totalImpact > 0 */}
          {result.totalImpact > 0 && (
            <div className="space-y-2">
              <button
                onClick={() => setShowAffectedPaths(!showAffectedPaths)}
                className="w-full flex items-center gap-1 font-medium text-muted-foreground uppercase tracking-wider text-[10px] hover:text-foreground"
              >
                {showAffectedPaths ? (
                  <ChevronDown className="h-3 w-3" />
                ) : (
                  <ChevronRight className="h-3 w-3" />
                )}
                Affected Paths ({result.affectedPaths?.length || result.totalImpact})
              </button>
              {showAffectedPaths && (
                <div className="space-y-1 max-h-48 overflow-y-auto">
                  {result.affectedPaths && result.affectedPaths.length > 0 ? (
                    result.affectedPaths.map((path, idx) => (
                      <div key={idx} className="border border-border rounded p-2 text-[11px]">
                        <div className="flex items-center gap-1 font-medium mb-1">
                          <span>{path.source}</span>
                          <ArrowRight className="h-3 w-3 text-muted-foreground" />
                          <span>{path.target}</span>
                        </div>
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <span className="text-[10px]">{path.sourceMetro} → {path.targetMetro}</span>
                          <span className={`text-[10px] px-1 rounded ${
                            path.status === 'disconnected' ? 'bg-red-500/20 text-red-500' :
                            path.status === 'degraded' ? 'bg-yellow-500/20 text-yellow-500' :
                            'bg-blue-500/20 text-blue-500'
                          }`}>
                            {path.status}
                          </span>
                        </div>
                        {path.status !== 'disconnected' && (
                          <div className="text-[10px] text-muted-foreground mt-1">
                            {path.hopsBefore} → {path.hopsAfter} hops
                            {path.metricBefore !== path.metricAfter && (
                              <span className="ml-2">
                                {(path.metricBefore / 1000).toFixed(1)} → {(path.metricAfter / 1000).toFixed(1)}ms
                              </span>
                            )}
                          </div>
                        )}
                      </div>
                    ))
                  ) : (
                    <div className="text-muted-foreground text-[11px] italic">
                      {result.totalImpact} path{result.totalImpact !== 1 ? 's' : ''} would need to be rerouted
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Disconnected devices list */}
          {result.disconnectedList && result.disconnectedList.length > 0 && (
            <div className="space-y-2">
              <div className="font-medium text-muted-foreground uppercase tracking-wider text-[10px]">
                Unreachable Devices
              </div>
              <div className="text-red-500 flex items-center gap-1.5">
                <AlertTriangle className="h-3.5 w-3.5" />
                {result.disconnectedList.length} device{result.disconnectedList.length !== 1 ? 's' : ''} would be isolated
              </div>
              <div className="space-y-0.5 max-h-32 overflow-y-auto">
                {result.disconnectedList.map(code => (
                  <div key={code} className="flex items-center gap-1.5 pl-1 text-[11px]">
                    <div className="w-2 h-2 rounded-full bg-red-500" />
                    <span>{code}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Affected metros */}
          {result.affectedMetros && result.affectedMetros.length > 0 && (
            <div className="space-y-2">
              <div className="font-medium text-muted-foreground uppercase tracking-wider text-[10px]">
                Affected Metro Pairs
              </div>
              <div className="space-y-1 max-h-32 overflow-y-auto">
                {result.affectedMetros.map((metro, idx) => (
                  <div key={idx} className="flex items-center gap-1.5 pl-1 text-[11px]">
                    <MapPin className={`h-3 w-3 ${
                      metro.status === 'disconnected' ? 'text-red-500' :
                      metro.status === 'degraded' ? 'text-yellow-500' : 'text-muted-foreground'
                    }`} />
                    <span>{metro.sourceMetro} ↔ {metro.targetMetro}</span>
                    <span className={`text-[10px] ${
                      metro.status === 'disconnected' ? 'text-red-500' :
                      metro.status === 'degraded' ? 'text-yellow-500' : 'text-muted-foreground'
                    }`}>
                      ({metro.status})
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* No impact message */}
          {result.totalDisconnected === 0 && result.totalImpact === 0 && (
            <div className="text-green-500 flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-green-500" />
              No significant impact - network remains fully connected
            </div>
          )}
        </div>
      )}

      {result?.error && (
        <div className="text-destructive">{result.error}</div>
      )}
    </div>
  )
}
