import { Coins, X } from 'lucide-react'
import { useTopology } from '../TopologyContext'

interface StakeInfo {
  stakeSol: number
  validatorCount: number
}

interface StakeOverlayPanelProps {
  deviceStakeMap: Map<string, StakeInfo>
  getStakeColor: (ratio: number) => string
  getDeviceLabel?: (pk: string) => string
  isLoading?: boolean
}

export function StakeOverlayPanel({
  deviceStakeMap,
  getStakeColor,
  getDeviceLabel,
  isLoading,
}: StakeOverlayPanelProps) {
  const { toggleOverlay } = useTopology()

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium flex items-center gap-1.5">
          <Coins className="h-3.5 w-3.5 text-amber-500" />
          Stake Distribution
        </span>
        <button
          onClick={() => toggleOverlay('stake')}
          className="p-1 hover:bg-[var(--muted)] rounded"
          title="Close"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading stake data...</div>
      )}

      {!isLoading && deviceStakeMap.size > 0 && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            {(() => {
              const devicesWithStake = Array.from(deviceStakeMap.entries()).filter(([, s]) => s.stakeSol > 0)
              const totalStake = devicesWithStake.reduce((sum, [, s]) => sum + s.stakeSol, 0)
              const totalValidators = devicesWithStake.reduce((sum, [, s]) => sum + s.validatorCount, 0)
              return (
                <>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Devices w/ Stake</span>
                    <span className="font-medium">{devicesWithStake.length}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Total Validators</span>
                    <span className="font-medium">{totalValidators.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Total Stake</span>
                    <span className="font-medium">{(totalStake / 1_000_000).toFixed(1)}M SOL</span>
                  </div>
                </>
              )
            })()}
          </div>

          {/* Top devices by stake */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Top by Stake</div>
            <div className="space-y-1 max-h-24 overflow-y-auto">
              {Array.from(deviceStakeMap.entries())
                .filter(([, s]) => s.stakeSol > 0)
                .sort((a, b) => b[1].stakeSol - a[1].stakeSol)
                .slice(0, 5)
                .map(([pk, stake]) => {
                  const label = getDeviceLabel?.(pk) || pk.substring(0, 8)
                  return (
                    <div key={pk} className="flex items-center justify-between gap-2">
                      <span className="truncate">{label}</span>
                      <span className="text-amber-500 font-medium whitespace-nowrap">
                        {stake.stakeSol >= 1_000_000
                          ? `${(stake.stakeSol / 1_000_000).toFixed(1)}M`
                          : `${(stake.stakeSol / 1_000).toFixed(0)}K`}
                      </span>
                    </div>
                  )
                })}
            </div>
          </div>

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Node Size = Stake Amount</div>
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <div className="w-5 h-5 rounded-full" style={{ backgroundColor: getStakeColor(1.0) }} />
                <span>High stake (&gt;1% share)</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-4 rounded-full" style={{ backgroundColor: getStakeColor(0.3) }} />
                <span>Medium stake</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: getStakeColor(0) }} />
                <span>No validators</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
