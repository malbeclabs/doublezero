import { Users } from 'lucide-react'
import type { TopologyValidator } from '@/lib/api'

interface ValidatorsOverlayPanelProps {
  validators: TopologyValidator[]
  isLoading?: boolean
}

export function ValidatorsOverlayPanel({
  validators,
  isLoading,
}: ValidatorsOverlayPanelProps) {
  // Calculate totals
  const totalValidators = validators.length
  const totalStakeSol = validators.reduce((sum, v) => sum + (v.stake_sol ?? 0), 0)
  const totalStakeShare = validators.reduce((sum, v) => sum + (v.stake_share ?? 0), 0)

  // Group by country for distribution
  const countryDistribution = validators.reduce((acc, v) => {
    const country = v.country || 'Unknown'
    acc.set(country, (acc.get(country) || 0) + 1)
    return acc
  }, new Map<string, number>())

  // Sort countries by count
  const topCountries = Array.from(countryDistribution.entries())
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5)

  // Format stake amount
  const formatStake = (sol: number) => {
    if (sol >= 1_000_000) return `${(sol / 1_000_000).toFixed(2)}M`
    if (sol >= 1_000) return `${(sol / 1_000).toFixed(0)}K`
    return sol.toFixed(0)
  }

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center gap-1.5 mb-2">
        <Users className="h-3.5 w-3.5 text-purple-500" />
        <span className="font-medium">Validators</span>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading validator data...</div>
      )}

      {!isLoading && validators.length > 0 && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Validators</span>
              <span className="font-medium">{totalValidators.toLocaleString()}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Total Stake</span>
              <span className="font-medium">{formatStake(totalStakeSol)} SOL</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Network Share</span>
              <span className="font-medium text-purple-500">{totalStakeShare.toFixed(2)}%</span>
            </div>
          </div>

          {/* Country distribution */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Top Countries</div>
            <div className="space-y-0.5 max-h-32 overflow-y-auto">
              {topCountries.map(([country, count]) => (
                <div
                  key={country}
                  className="flex items-center justify-between gap-2 px-1.5 py-1 rounded hover:bg-[var(--muted)] transition-colors"
                >
                  <span className="truncate">{country}</span>
                  <span className="text-muted-foreground flex-shrink-0">{count}</span>
                </div>
              ))}
            </div>
          </div>

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Marker Size = Stake</div>
            <div className="space-y-1 text-muted-foreground">
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-4 rounded-full bg-purple-500" />
                <span>Validator location</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 rounded bg-purple-500" />
                <span>Link to device</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {!isLoading && validators.length === 0 && (
        <div className="text-muted-foreground">No validators on DZ network</div>
      )}
    </div>
  )
}
