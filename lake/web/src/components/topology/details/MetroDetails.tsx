import type { MetroInfo } from '../types'
import { EntityLink } from '../EntityLink'

interface MetroDetailsProps {
  metro: MetroInfo
}

export function MetroDetails({ metro }: MetroDetailsProps) {
  const stats = [
    { label: 'Code', value: metro.code },
    { label: 'Devices', value: String(metro.deviceCount) },
  ]

  return (
    <div className="p-4 space-y-4">
      {/* Stats grid */}
      <div className="grid grid-cols-2 gap-2">
        {stats.map((stat, i) => (
          <div key={i} className="text-center p-2 bg-[var(--muted)]/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">
              {stat.value}
            </div>
            <div className="text-xs text-muted-foreground">{stat.label}</div>
          </div>
        ))}
      </div>

      {/* Note about no traffic data for metros */}
      <div className="text-sm text-muted-foreground text-center py-4">
        No traffic data available for metros
      </div>
    </div>
  )
}

// Header content for the panel
export function MetroDetailsHeader({ metro }: MetroDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        metro
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink to={`/dz/metros/${metro.pk}`}>
          {metro.name}
        </EntityLink>
      </div>
    </>
  )
}
