import { Activity, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'

export function BandwidthOverlayPanel() {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Activity className="h-3.5 w-3.5 text-blue-500" />
          Link Bandwidth
        </span>
        <button
          onClick={() => toggleOverlay('bandwidth')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      <div className="text-muted-foreground mb-3">
        Links colored by capacity. Thicker lines indicate higher bandwidth.
      </div>

      {/* Legend */}
      <div className="space-y-1.5">
        <div className="text-muted-foreground mb-1">Link Capacity</div>
        <div className="flex items-center gap-2">
          <div className="w-6 h-1.5 bg-blue-500 rounded" />
          <span>100+ Gbps</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-5 h-1 bg-blue-400 rounded" />
          <span>10-100 Gbps</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-4 h-0.5 bg-blue-300 rounded" />
          <span>1-10 Gbps</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-3 h-0.5 bg-blue-200 rounded" />
          <span>&lt;1 Gbps</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-2 h-0.5 bg-gray-400 rounded opacity-50" />
          <span className="text-muted-foreground">Unknown</span>
        </div>
      </div>
    </div>
  )
}
