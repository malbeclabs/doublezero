import { GitCompare, AlertTriangle } from 'lucide-react'
import type { TopologyCompareResponse } from '@/lib/api'

interface ComparePanelProps {
  data: TopologyCompareResponse | null
  isLoading: boolean
}

export function ComparePanel({ data, isLoading }: ComparePanelProps) {
  return (
    <div className="p-3 text-xs">
      <div className="flex items-center gap-1.5 mb-3">
        <GitCompare className="h-3.5 w-3.5 text-blue-500" />
        <span className="font-medium">ISIS Health</span>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading comparison...</div>
      )}

      {data && !data.error && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Configured Links</span>
              <span className="font-medium">{data.configuredLinks}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">ISIS Adjacencies</span>
              <span className="font-medium">{data.isisAdjacencies}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Matched</span>
              <span className="font-medium text-green-500">{data.matchedLinks}</span>
            </div>
          </div>

          {/* Discrepancy summary */}
          {data.discrepancies.length > 0 && (
            <div className="pt-2 border-t border-[var(--border)]">
              <div className="flex items-center gap-1.5 mb-2">
                <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                <span className="font-medium">{data.discrepancies.length} Issues</span>
              </div>
              <div className="space-y-1">
                {data.discrepancies.filter(d => d.type === 'missing_isis').length > 0 && (
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-0.5 bg-red-500" style={{ borderStyle: 'dashed', borderWidth: '1px', borderColor: '#ef4444' }} />
                    <span className="text-red-500">{data.discrepancies.filter(d => d.type === 'missing_isis').length} missing ISIS</span>
                  </div>
                )}
                {data.discrepancies.filter(d => d.type === 'extra_isis').length > 0 && (
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-0.5 bg-amber-500" />
                    <span className="text-amber-500">{data.discrepancies.filter(d => d.type === 'extra_isis').length} extra adjacencies</span>
                  </div>
                )}
                {data.discrepancies.filter(d => d.type === 'metric_mismatch').length > 0 && (
                  <div className="flex items-center gap-1.5">
                    <div className="w-3 h-0.5 bg-yellow-500" />
                    <span className="text-yellow-500">{data.discrepancies.filter(d => d.type === 'metric_mismatch').length} metric mismatches</span>
                  </div>
                )}
              </div>
            </div>
          )}

          {data.discrepancies.length === 0 && (
            <div className="pt-2 border-t border-[var(--border)] text-green-500 flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-green-500" />
              All links healthy
            </div>
          )}

          {/* Edge legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Edge Colors</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-green-500" />
                <span>Matched</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-red-500" style={{ borderTop: '2px dashed #ef4444' }} />
                <span>Missing ISIS</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-amber-500" />
                <span>Extra adjacency</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 bg-yellow-500" />
                <span>Metric mismatch</span>
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
