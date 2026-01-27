import { Link } from 'react-router-dom'
import type { DeviceInterface, DeviceDetail, TopologyDevice } from '@/lib/api'
import { TrafficCharts } from '@/components/topology/TrafficCharts'
import { InterfaceHealthCharts } from '@/components/topology/InterfaceHealthCharts'

// Shared device info type that both topology and device page can use
export interface DeviceInfoData {
  pk: string
  code: string
  deviceType: string
  status: string
  metroPk: string
  metroName: string
  contributorPk: string
  contributorCode: string
  userCount: number
  validatorCount: number
  stakeSol: number
  stakeShare: number
  interfaces: DeviceInterface[]
}

interface DeviceInfoContentProps {
  device: DeviceInfoData
  /** Compact mode for sidebar panels */
  compact?: boolean
}

function formatStake(sol: number): string {
  if (sol === 0) return '—'
  if (sol >= 1e6) return `${(sol / 1e6).toFixed(2)}M SOL`
  if (sol >= 1e3) return `${(sol / 1e3).toFixed(1)}K SOL`
  return `${sol.toFixed(0)} SOL`
}

function formatStakeShare(share: number): string {
  if (share === 0) return '—'
  return `${share.toFixed(2)}%`
}

/**
 * Shared component for displaying device information.
 * Used by both the topology panel and the device detail page.
 */
export function DeviceInfoContent({ device, compact = false }: DeviceInfoContentProps) {
  const stats = [
    { label: 'Type', value: device.deviceType },
    {
      label: 'Contributor',
      value: device.contributorPk ? (
        <Link to={`/dz/contributors/${device.contributorPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
          {device.contributorCode}
        </Link>
      ) : (
        device.contributorCode || '—'
      ),
    },
    {
      label: 'Metro',
      value: device.metroPk ? (
        <Link to={`/dz/metros/${device.metroPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
          {device.metroName}
        </Link>
      ) : (
        device.metroName || '—'
      ),
    },
    { label: 'Users', value: String(device.userCount) },
    { label: 'Validators', value: String(device.validatorCount) },
    { label: 'Stake', value: formatStake(device.stakeSol) },
    { label: 'Stake Share', value: formatStakeShare(device.stakeShare) },
  ]

  // Sort interfaces: activated first, then by name
  const sortedInterfaces = [...(device.interfaces || [])].sort((a, b) => {
    if (a.status === 'activated' && b.status !== 'activated') return -1
    if (a.status !== 'activated' && b.status === 'activated') return 1
    return a.name.localeCompare(b.name)
  })

  // Compact mode: optimized for sidebar panels
  if (compact) {
    return (
      <div className="space-y-4">
        {/* Stats grid - 2 columns for sidebar */}
        <div className="grid grid-cols-2 gap-2">
          {stats.map((stat, i) => (
            <div key={i} className="text-center p-2 bg-muted/30 rounded-lg">
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
                  className="flex items-center justify-between p-2 bg-muted/30 rounded text-xs font-mono"
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

        {/* Interface health charts */}
        <InterfaceHealthCharts devicePk={device.pk} />
      </div>
    )
  }

  // Wide mode: optimized for full-page view on desktop
  return (
    <div className="space-y-6">
      {/* Stats and Interfaces row - side by side on large screens */}
      <div className="grid grid-cols-1 lg:grid-cols-[1fr,auto] gap-6">
        {/* Stats grid - responsive columns */}
        <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-2">
          {stats.map((stat, i) => (
            <div key={i} className="text-center p-3 bg-muted/30 rounded-lg">
              <div className="text-base font-medium tabular-nums tracking-tight">
                {stat.value}
              </div>
              <div className="text-xs text-muted-foreground">{stat.label}</div>
            </div>
          ))}
        </div>

        {/* Interfaces - fixed width on large screens */}
        {sortedInterfaces.length > 0 && (
          <div className="lg:w-72">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">
              Interfaces ({sortedInterfaces.length})
            </div>
            <div className="space-y-1 max-h-48 overflow-y-auto">
              {sortedInterfaces.map((iface, i) => (
                <div
                  key={i}
                  className="flex items-center justify-between p-2 bg-muted/30 rounded text-xs font-mono"
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
      </div>

      {/* Charts row - side by side on large screens */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div>
          <TrafficCharts entityType="device" entityPk={device.pk} />
        </div>
        <div>
          <InterfaceHealthCharts devicePk={device.pk} />
        </div>
      </div>
    </div>
  )
}

/**
 * Convert DeviceDetail (from /api/dz/devices/:pk) to shared DeviceInfoData
 */
export function deviceDetailToInfo(device: DeviceDetail, metroName?: string): DeviceInfoData {
  return {
    pk: device.pk,
    code: device.code,
    deviceType: device.device_type,
    status: device.status,
    metroPk: device.metro_pk,
    metroName: metroName || device.metro_name || device.metro_code,
    contributorPk: device.contributor_pk,
    contributorCode: device.contributor_code,
    userCount: device.current_users,
    validatorCount: device.validator_count,
    stakeSol: device.stake_sol,
    stakeShare: device.stake_share,
    interfaces: device.interfaces || [],
  }
}

/**
 * Convert TopologyDevice (from /api/topology) to shared DeviceInfoData
 */
export function topologyDeviceToInfo(device: TopologyDevice, metroName: string): DeviceInfoData {
  return {
    pk: device.pk,
    code: device.code,
    deviceType: device.device_type,
    status: device.status,
    metroPk: device.metro_pk,
    metroName: metroName,
    contributorPk: device.contributor_pk,
    contributorCode: device.contributor_code,
    userCount: device.user_count,
    validatorCount: device.validator_count,
    stakeSol: device.stake_sol,
    stakeShare: device.stake_share,
    interfaces: device.interfaces || [],
  }
}
