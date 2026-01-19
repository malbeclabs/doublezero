import type { LinkInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { TrafficCharts } from '../TrafficCharts'

interface LinkDetailsProps {
  link: LinkInfo
}

export function LinkDetails({ link }: LinkDetailsProps) {
  const stats = [
    {
      label: 'Contributor',
      value: link.contributorPk
        ? <EntityLink to={`/dz/contributors/${link.contributorPk}`}>{link.contributorCode}</EntityLink>
        : link.contributorCode || '—',
    },
    { label: 'Bandwidth', value: link.bandwidth },
    { label: 'Latency', value: link.latencyMs },
    { label: 'Jitter', value: link.jitterMs },
    { label: 'Loss', value: link.lossPercent },
    { label: 'Current In', value: link.inRate },
    { label: 'Current Out', value: link.outRate },
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

      {/* Traffic charts */}
      <TrafficCharts entityType="link" entityPk={link.pk} />
    </div>
  )
}

// Header content for the panel
export function LinkDetailsHeader({ link }: LinkDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        link
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink to={`/dz/links/${link.pk}`}>
          {link.code}
        </EntityLink>
      </div>
      <div className="text-xs text-muted-foreground mt-0.5">
        <EntityLink to={`/dz/devices/${link.deviceAPk}`}>{link.deviceACode}</EntityLink>
        {' ↔ '}
        <EntityLink to={`/dz/devices/${link.deviceZPk}`}>{link.deviceZCode}</EntityLink>
      </div>
    </>
  )
}
