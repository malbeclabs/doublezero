import { BarChart3, AlertTriangle, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'
import type { TopologyLink } from '@/lib/api'

// Format bits per second to human readable
function formatBps(bps: number): string {
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)}Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)}Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)}Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(0)}Kbps`
  return `${bps.toFixed(0)}bps`
}

interface TrafficInfo {
  inBps: number
  outBps: number
  utilization: number
}

interface TrafficFlowOverlayPanelProps {
  edgeTrafficMap: Map<string, TrafficInfo>
  links: TopologyLink[] | undefined
  isLoading?: boolean
}

export function TrafficFlowOverlayPanel({
  edgeTrafficMap,
  links,
  isLoading,
}: TrafficFlowOverlayPanelProps) {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <BarChart3 className="h-3.5 w-3.5 text-cyan-500" />
          Traffic Flow
        </span>
        <button
          onClick={() => toggleOverlay('trafficFlow')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading traffic data...</div>
      )}

      {!isLoading && edgeTrafficMap.size > 0 && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Links with traffic</span>
              <span className="font-medium">{edgeTrafficMap.size / 2}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-red-500">Critical (≥80%)</span>
              <span className="font-medium text-red-500">
                {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 80).length}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-yellow-500">High (50-80%)</span>
              <span className="font-medium text-yellow-500">
                {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 50 && v.utilization < 80).length}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-lime-500">Medium (20-50%)</span>
              <span className="font-medium text-lime-500">
                {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization >= 20 && v.utilization < 50).length}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-green-500">Low (&lt;20%)</span>
              <span className="font-medium text-green-500">
                {Array.from(edgeTrafficMap.values()).filter((v, i) => i % 2 === 0 && v.utilization > 0 && v.utilization < 20).length}
              </span>
            </div>
          </div>

          {/* High utilization links */}
          {links && (() => {
            const highUtilLinks = links
              .filter(l => {
                const totalBps = (l.in_bps ?? 0) + (l.out_bps ?? 0)
                const util = l.bandwidth_bps > 0 ? (totalBps / l.bandwidth_bps) * 100 : 0
                return util >= 50
              })
              .sort((a, b) => {
                const utilA = a.bandwidth_bps > 0 ? ((a.in_bps ?? 0) + (a.out_bps ?? 0)) / a.bandwidth_bps : 0
                const utilB = b.bandwidth_bps > 0 ? ((b.in_bps ?? 0) + (b.out_bps ?? 0)) / b.bandwidth_bps : 0
                return utilB - utilA
              })
              .slice(0, 5)

            if (highUtilLinks.length === 0) return null

            return (
              <div className="pt-2 border-t border-[var(--border)]">
                <div className="flex items-center gap-1.5 mb-2">
                  <AlertTriangle className="h-3.5 w-3.5 text-yellow-500" />
                  <span className="font-medium text-yellow-500">High Utilization</span>
                </div>
                <div className="space-y-1">
                  {highUtilLinks.map(link => {
                    const totalBps = (link.in_bps ?? 0) + (link.out_bps ?? 0)
                    const util = link.bandwidth_bps > 0 ? (totalBps / link.bandwidth_bps) * 100 : 0
                    const color = util >= 80 ? 'text-red-400' : 'text-yellow-400'
                    return (
                      <div key={link.pk} className={`${color} truncate text-[10px]`}>
                        {link.code}
                        <span className="text-muted-foreground ml-1">
                          ({util.toFixed(0)}% - {formatBps(totalBps)})
                        </span>
                      </div>
                    )
                  })}
                </div>
              </div>
            )
          })()}

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Link Colors (by utilization)</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-green-500 rounded" />
                <span>Low (&lt;20%)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-lime-500 rounded" />
                <span>Medium (20-50%)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-1 bg-yellow-500 rounded" />
                <span>High (50-80%)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-1.5 bg-red-500 rounded" />
                <span>Critical (≥80%)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-gray-400 rounded opacity-40" />
                <span>Idle (no traffic)</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
