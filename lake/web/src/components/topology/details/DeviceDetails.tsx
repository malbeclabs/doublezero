import type { DeviceInfo } from '../types'
import { EntityLink } from '../EntityLink'
import { TrafficCharts } from '../TrafficCharts'

interface DeviceDetailsProps {
  device: DeviceInfo
}

export function DeviceDetails({ device }: DeviceDetailsProps) {
  const stats = [
    { label: 'Type', value: device.deviceType },
    {
      label: 'Contributor',
      value: device.contributorPk
        ? <EntityLink to={`/dz/contributors/${device.contributorPk}`}>{device.contributorCode}</EntityLink>
        : device.contributorCode || '—',
    },
    {
      label: 'Metro',
      value: device.metroPk
        ? <EntityLink to={`/dz/metros/${device.metroPk}`}>{device.metroName}</EntityLink>
        : device.metroName,
    },
    { label: 'Users', value: String(device.userCount) },
    { label: 'Validators', value: String(device.validatorCount) },
    { label: 'Stake', value: `${device.stakeSol} SOL` },
    { label: 'Stake Share', value: device.stakeShare },
  ]

  // Sort interfaces: activated first, then by name
  const sortedInterfaces = [...(device.interfaces || [])].sort((a, b) => {
    if (a.status === 'activated' && b.status !== 'activated') return -1
    if (a.status !== 'activated' && b.status === 'activated') return 1
    return a.name.localeCompare(b.name)
  })

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

      {/* Interfaces */}
      {sortedInterfaces.length > 0 && (
        <div>
          <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
            Interfaces ({sortedInterfaces.length})
          </div>
          <div className="space-y-1 max-h-48 overflow-y-auto">
            {sortedInterfaces.map((iface, i) => (
              <div
                key={i}
                className="flex items-center justify-between p-2 bg-[var(--muted)]/30 rounded text-xs font-mono"
              >
                <span className="truncate flex-1 mr-2" title={iface.name}>
                  {iface.name}
                </span>
                <span className="text-muted-foreground whitespace-nowrap">
                  {iface.ip || '—'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Traffic charts */}
      <TrafficCharts entityType="device" entityPk={device.pk} />
    </div>
  )
}

// Header content for the panel
export function DeviceDetailsHeader({ device }: DeviceDetailsProps) {
  return (
    <>
      <div className="text-xs text-muted-foreground uppercase tracking-wider">
        device
      </div>
      <div className="text-sm font-medium min-w-0 flex-1">
        <EntityLink to={`/dz/devices/${device.pk}`}>
          {device.code}
        </EntityLink>
      </div>
    </>
  )
}
