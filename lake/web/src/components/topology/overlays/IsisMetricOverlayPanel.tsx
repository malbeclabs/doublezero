import { Route, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'

export function IsisMetricOverlayPanel() {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Route className="h-3.5 w-3.5 text-green-500" />
          ISIS
        </span>
        <button
          onClick={() => toggleOverlay('isisHealth')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      <div className="text-muted-foreground mb-3">
        Link color shows health status, thickness shows ISIS metric (latency). Lower metric values indicate better paths.
      </div>

      {/* Legend */}
      <div className="space-y-1.5">
        <div className="text-muted-foreground mb-1">Link Quality</div>
        <div className="flex items-center gap-2">
          <div className="w-5 h-1 bg-green-500 rounded" />
          <span>&lt;1ms (excellent)</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-4 h-0.5 bg-lime-500 rounded" />
          <span>1-5ms (good)</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-3 h-0.5 bg-yellow-500 rounded" />
          <span>5-20ms (moderate)</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-3 h-0.5 bg-orange-500 rounded" />
          <span>&gt;20ms (high latency)</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-2 h-0.5 bg-gray-400 rounded opacity-50" />
          <span className="text-muted-foreground">No data</span>
        </div>
      </div>
    </div>
  )
}
