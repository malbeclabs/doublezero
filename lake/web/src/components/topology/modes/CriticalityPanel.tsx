import { Shield, AlertTriangle } from 'lucide-react'
import type { CriticalLinksResponse } from '@/lib/api'

interface CriticalityPanelProps {
  data: CriticalLinksResponse | null
  isLoading: boolean
}

export function CriticalityPanel({ data, isLoading }: CriticalityPanelProps) {
  return (
    <div className="p-3 text-xs">
      <div className="flex items-center gap-1.5 mb-3">
        <Shield className="h-3.5 w-3.5 text-red-500" />
        <span className="font-medium">Link Criticality</span>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Analyzing links...</div>
      )}

      {data && !data.error && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Total Links</span>
              <span className="font-medium">{data.links.length}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-red-500">Critical</span>
              <span className="font-medium text-red-500">
                {data.links.filter(l => l.criticality === 'critical').length}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-amber-500">Important</span>
              <span className="font-medium text-amber-500">
                {data.links.filter(l => l.criticality === 'important').length}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-green-500">Redundant</span>
              <span className="font-medium text-green-500">
                {data.links.filter(l => l.criticality === 'redundant').length}
              </span>
            </div>
          </div>

          {/* Critical links list */}
          {data.links.filter(l => l.criticality === 'critical').length > 0 && (
            <div className="pt-2 border-t border-[var(--border)]">
              <div className="flex items-center gap-1.5 mb-2">
                <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
                <span className="font-medium text-red-500">Single Points of Failure</span>
              </div>
              <div className="space-y-1">
                {data.links.filter(l => l.criticality === 'critical').slice(0, 5).map((link, i) => (
                  <div key={i} className="text-red-400 truncate">
                    {link.sourceCode} — {link.targetCode}
                  </div>
                ))}
                {data.links.filter(l => l.criticality === 'critical').length > 5 && (
                  <div className="text-muted-foreground">
                    +{data.links.filter(l => l.criticality === 'critical').length - 5} more
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Edge Colors</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-1 bg-red-500 rounded" />
                <span>Critical (no redundancy)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-amber-500 rounded" />
                <span>Important (limited)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-green-500 rounded opacity-60" />
                <span>Redundant (safe)</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {data?.error && (
        <div className="text-destructive">{data.error}</div>
      )}
    </div>
  )
}
