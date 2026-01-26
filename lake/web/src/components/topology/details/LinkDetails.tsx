import type { LinkInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { TrafficCharts } from '../TrafficCharts'
import { LatencyCharts } from '../LatencyCharts'

interface LinkDetailsProps {
  link: LinkInfo
}

export function LinkDetails({ link }: LinkDetailsProps) {
  // Check if we have directional latency data
  const hasDirectionalData = link.latencyAtoZMs !== 'N/A' || link.latencyZtoAMs !== 'N/A'

  const stats = [
    { label: 'Packet Loss', value: link.lossPercent },
    { label: 'Bandwidth', value: link.bandwidth },
    { label: 'Current In', value: link.inRate },
    { label: 'Current Out', value: link.outRate },
    {
      label: 'Contributor',
      value: link.contributorPk
        ? <EntityLink to={`/dz/contributors/${link.contributorPk}`}>{link.contributorCode}</EntityLink>
        : link.contributorCode || '—',
    },
  ]

  return (
    <div className="p-4 space-y-4">
      {/* Endpoints with per-direction latency */}
      <div className="grid grid-cols-2 gap-3">
        <div className="p-2 bg-[var(--muted)]/30 rounded-lg">
          <div className="text-xs text-muted-foreground mb-1">A-Side</div>
          <div className="text-sm font-medium">
            <EntityLink to={`/dz/devices/${link.deviceAPk}`}>{link.deviceACode}</EntityLink>
          </div>
          {link.interfaceAName && (
            <div className="text-xs text-muted-foreground font-mono mt-0.5">{link.interfaceAName}</div>
          )}
          {link.interfaceAIP && (
            <div className="text-xs text-muted-foreground font-mono">{link.interfaceAIP}</div>
          )}
          {hasDirectionalData && (
            <div className="mt-2 pt-2 border-t border-[var(--muted)]/50">
              <div className="text-xs text-muted-foreground">RTT from A</div>
              <div className="text-sm font-medium tabular-nums">{link.latencyAtoZMs}</div>
              <div className="text-xs text-muted-foreground mt-1">Jitter from A</div>
              <div className="text-sm font-medium tabular-nums">{link.jitterAtoZMs}</div>
            </div>
          )}
        </div>
        <div className="p-2 bg-[var(--muted)]/30 rounded-lg">
          <div className="text-xs text-muted-foreground mb-1">Z-Side</div>
          <div className="text-sm font-medium">
            <EntityLink to={`/dz/devices/${link.deviceZPk}`}>{link.deviceZCode}</EntityLink>
          </div>
          {link.interfaceZName && (
            <div className="text-xs text-muted-foreground font-mono mt-0.5">{link.interfaceZName}</div>
          )}
          {link.interfaceZIP && (
            <div className="text-xs text-muted-foreground font-mono">{link.interfaceZIP}</div>
          )}
          {hasDirectionalData && (
            <div className="mt-2 pt-2 border-t border-[var(--muted)]/50">
              <div className="text-xs text-muted-foreground">RTT from Z</div>
              <div className="text-sm font-medium tabular-nums">{link.latencyZtoAMs}</div>
              <div className="text-xs text-muted-foreground mt-1">Jitter from Z</div>
              <div className="text-sm font-medium tabular-nums">{link.jitterZtoAMs}</div>
            </div>
          )}
        </div>
      </div>

      {/* Combined latency (average of both directions) - shown when no directional data */}
      {!hasDirectionalData && (
        <div className="grid grid-cols-2 gap-2">
          <div className="text-center p-2 bg-[var(--muted)]/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{link.latencyMs}</div>
            <div className="text-xs text-muted-foreground">Latency</div>
          </div>
          <div className="text-center p-2 bg-[var(--muted)]/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{link.jitterMs}</div>
            <div className="text-xs text-muted-foreground">Jitter</div>
          </div>
        </div>
      )}

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

      {/* Latency charts */}
      <LatencyCharts linkPk={link.pk} />
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
