import type { ValidatorInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { TrafficCharts } from '../TrafficCharts'

interface ValidatorDetailsProps {
  validator: ValidatorInfo
}

export function ValidatorDetails({ validator }: ValidatorDetailsProps) {
  const stats = [
    { label: 'Location', value: `${validator.city}, ${validator.country}` },
    {
      label: 'Device',
      value: validator.devicePk
        ? <EntityLink to={`/dz/devices/${validator.devicePk}`} className="font-mono">{validator.deviceCode}</EntityLink>
        : validator.deviceCode,
    },
    {
      label: 'Metro',
      value: validator.metroPk
        ? <EntityLink to={`/dz/metros/${validator.metroPk}`}>{validator.metroName}</EntityLink>
        : validator.metroName,
    },
    { label: 'Stake', value: `${validator.stakeSol} SOL` },
    { label: 'DZ Stake Share', value: validator.stakeShare },
    { label: 'Commission', value: `${validator.commission}%` },
    { label: 'Version', value: validator.version || '—' },
    { label: 'Current In', value: validator.inRate },
    { label: 'Current Out', value: validator.outRate },
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

      {/* Validator identity section */}
      <div className="border-t border-[var(--border)] pt-4 space-y-2">
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Identity</div>
        <div className="space-y-1.5 text-xs">
          <div>
            <div className="text-muted-foreground mb-0.5">Vote Pubkey</div>
            <EntityLink
              to={`/solana/validators/${validator.votePubkey}`}
              className="font-mono truncate block"
              title={validator.votePubkey}
            >
              {validator.votePubkey}
            </EntityLink>
          </div>
          <div>
            <div className="text-muted-foreground mb-0.5">Node Pubkey</div>
            <EntityLink
              to={`/solana/gossip-nodes/${validator.nodePubkey}`}
              className="font-mono truncate block"
              title={validator.nodePubkey}
            >
              {validator.nodePubkey}
            </EntityLink>
          </div>
        </div>
        <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2 mt-4">Network</div>
        <div className="space-y-1.5 text-xs">
          <div className="flex justify-between">
            <span className="text-muted-foreground">Gossip</span>
            <span className="font-mono">{validator.gossipIp ? `${validator.gossipIp}:${validator.gossipPort}` : '—'}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-muted-foreground">TPU QUIC</span>
            <span className="font-mono">{validator.tpuQuicIp ? `${validator.tpuQuicIp}:${validator.tpuQuicPort}` : '—'}</span>
          </div>
        </div>
      </div>

      {/* Traffic charts */}
      <TrafficCharts entityType="validator" entityPk={String(validator.tunnelId)} />

      {/* External link */}
      <div className="pt-2 border-t border-[var(--border)]">
        <a
          href={`https://www.validators.app/validators/${validator.nodePubkey}?locale=en&network=mainnet`}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs text-muted-foreground hover:text-blue-500 hover:underline"
        >
          View on validators.app →
        </a>
      </div>
    </div>
  )
}

// Header content for the panel
export function ValidatorDetailsHeader({ validator }: ValidatorDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        validator
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink
          to={`/solana/validators/${validator.votePubkey}`}
          className="font-mono block truncate"
          title={validator.votePubkey}
        >
          {validator.votePubkey}
        </EntityLink>
      </div>
    </>
  )
}
