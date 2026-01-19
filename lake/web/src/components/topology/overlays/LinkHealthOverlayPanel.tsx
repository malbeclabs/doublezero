import { Activity, AlertTriangle, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'
import type { LinkHealthResponse } from '@/lib/api'

interface LinkHealthOverlayPanelProps {
  linkHealthData: LinkHealthResponse | null | undefined
  isLoading?: boolean
}

export function LinkHealthOverlayPanel({
  linkHealthData,
  isLoading,
}: LinkHealthOverlayPanelProps) {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Activity className="h-3.5 w-3.5 text-green-500" />
          Link Health (SLA)
        </span>
        <button
          onClick={() => toggleOverlay('linkHealth')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading health data...</div>
      )}

      {linkHealthData && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Total Links</span>
              <span className="font-medium">{linkHealthData.total_links}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-green-500">Healthy</span>
              <span className="font-medium text-green-500">{linkHealthData.healthy_count}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-yellow-500">Warning</span>
              <span className="font-medium text-yellow-500">{linkHealthData.warning_count}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-red-500">Critical</span>
              <span className="font-medium text-red-500">{linkHealthData.critical_count}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Unknown</span>
              <span className="font-medium text-muted-foreground">{linkHealthData.unknown_count}</span>
            </div>
          </div>

          {/* Critical links list */}
          {linkHealthData.critical_count > 0 && (
            <div className="pt-2 border-t border-[var(--border)]">
              <div className="flex items-center gap-1.5 mb-2">
                <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
                <span className="font-medium text-red-500">SLA Violations</span>
              </div>
              <div className="space-y-1 max-h-24 overflow-y-auto">
                {linkHealthData.links
                  .filter(l => l.sla_status === 'critical')
                  .slice(0, 5)
                  .map(link => (
                    <div key={link.link_pk} className="text-red-400 truncate text-[10px]">
                      {link.side_a_code} — {link.side_z_code}
                      <span className="text-muted-foreground ml-1">
                        ({(link.avg_rtt_us / 1000).toFixed(1)}ms vs {(link.committed_rtt_ns / 1_000_000).toFixed(1)}ms SLA)
                      </span>
                    </div>
                  ))}
                {linkHealthData.critical_count > 5 && (
                  <div className="text-muted-foreground">+{linkHealthData.critical_count - 5} more</div>
                )}
              </div>
            </div>
          )}

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Link Colors</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-green-500 rounded" />
                <span>Healthy (&lt;80% of SLA)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-yellow-500 rounded" />
                <span>Warning (80-100% of SLA)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-1 bg-red-500 rounded" />
                <span>Critical (exceeds SLA)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-gray-400 rounded opacity-50" />
                <span>Unknown (no data)</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
