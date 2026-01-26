import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Server, AlertCircle, ArrowLeft } from 'lucide-react'
import { fetchDevice } from '@/lib/api'

function formatBps(bps: number): string {
  if (bps === 0) return '—'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

function formatStake(sol: number): string {
  if (sol === 0) return '—'
  if (sol >= 1e6) return `${(sol / 1e6).toFixed(2)}M SOL`
  if (sol >= 1e3) return `${(sol / 1e3).toFixed(1)}K SOL`
  return `${sol.toFixed(0)} SOL`
}

const statusColors: Record<string, string> = {
  activated: 'text-green-600 dark:text-green-400',
  provisioning: 'text-blue-600 dark:text-blue-400',
  maintenance: 'text-amber-600 dark:text-amber-400',
  offline: 'text-red-600 dark:text-red-400',
}

export function DeviceDetailPage() {
  const { pk } = useParams<{ pk: string }>()
  const navigate = useNavigate()

  const { data: device, isLoading, error } = useQuery({
    queryKey: ['device', pk],
    queryFn: () => fetchDevice(pk!),
    enabled: !!pk,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !device) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Device not found</div>
          <button
            onClick={() => navigate('/dz/devices')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to devices
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-[1200px] mx-auto px-4 sm:px-8 py-8">
        {/* Back button */}
        <button
          onClick={() => navigate('/dz/devices')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to devices
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Server className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{device.code}</h1>
            <div className="text-sm text-muted-foreground">{device.device_type}</div>
          </div>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {/* Basic Info */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Basic Info</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Status</dt>
                <dd className={`text-sm capitalize ${statusColors[device.status] || ''}`}>{device.status}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Type</dt>
                <dd className="text-sm">{device.device_type}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Public IP</dt>
                <dd className="text-sm font-mono">{device.public_ip || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Metro</dt>
                <dd className="text-sm">
                  {device.metro_pk ? (
                    <Link to={`/dz/metros/${device.metro_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                      {device.metro_name || device.metro_code}
                    </Link>
                  ) : '—'}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Contributor</dt>
                <dd className="text-sm">
                  {device.contributor_pk ? (
                    <Link to={`/dz/contributors/${device.contributor_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                      {device.contributor_code}
                    </Link>
                  ) : '—'}
                </dd>
              </div>
            </dl>
          </div>

          {/* Users */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Users</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Current Users</dt>
                <dd className="text-sm">{device.current_users}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Max Users</dt>
                <dd className="text-sm">{device.max_users}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Utilization</dt>
                <dd className="text-sm">
                  {device.max_users > 0
                    ? `${((device.current_users / device.max_users) * 100).toFixed(1)}%`
                    : '—'}
                </dd>
              </div>
            </dl>
          </div>

          {/* Traffic */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Traffic</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Inbound</dt>
                <dd className="text-sm">{formatBps(device.in_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Outbound</dt>
                <dd className="text-sm">{formatBps(device.out_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Peak In (1h)</dt>
                <dd className="text-sm">{formatBps(device.peak_in_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Peak Out (1h)</dt>
                <dd className="text-sm">{formatBps(device.peak_out_bps)}</dd>
              </div>
            </dl>
          </div>

          {/* Validators */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Validators</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Count</dt>
                <dd className="text-sm">{device.validator_count}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Total Stake</dt>
                <dd className="text-sm">{formatStake(device.stake_sol)}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>
    </div>
  )
}
