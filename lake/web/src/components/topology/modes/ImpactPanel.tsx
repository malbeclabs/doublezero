import { Zap, X } from 'lucide-react'
import type { FailureImpactResponse } from '@/lib/api'

interface ImpactPanelProps {
  devicePK: string | null
  result: FailureImpactResponse | null
  isLoading: boolean
  onClose: () => void
}

export function ImpactPanel({ devicePK, result, isLoading, onClose }: ImpactPanelProps) {
  if (!devicePK && !isLoading) return null

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Zap className="h-3.5 w-3.5 text-purple-500" />
          Failure Impact
        </span>
        <button onClick={onClose} className="p-1 hover:bg-[var(--muted)] rounded" title="Close">
          <X className="h-3 w-3" />
        </button>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Analyzing impact...</div>
      )}

      {result && !result.error && (
        <div className="space-y-2">
          <div className="text-muted-foreground">
            If <span className="font-medium text-foreground">{result.deviceCode}</span> goes down:
          </div>

          {result.unreachableCount === 0 ? (
            <div className="text-green-500 flex items-center gap-1.5">
              <div className="w-2 h-2 rounded-full bg-green-500" />
              No devices would become unreachable
            </div>
          ) : (
            <div className="space-y-2">
              <div className="text-red-500 font-medium">
                {result.unreachableCount} device{result.unreachableCount !== 1 ? 's' : ''} would become unreachable
              </div>
              <div className="space-y-0.5 max-h-32 overflow-y-auto">
                {result.unreachableDevices.map(device => (
                  <div key={device.pk} className="flex items-center gap-1.5">
                    <div className={`w-2 h-2 rounded-full ${device.status === 'active' ? 'bg-green-500' : 'bg-red-500'}`} />
                    <span>{device.code}</span>
                  </div>
                ))}
              </div>
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
