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
