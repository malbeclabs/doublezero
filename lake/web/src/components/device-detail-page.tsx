import { useParams, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Server, AlertCircle, ArrowLeft } from 'lucide-react'
import { fetchDevice } from '@/lib/api'
import { DeviceInfoContent, deviceDetailToInfo } from '@/components/shared/DeviceInfoContent'

function formatBps(bps: number): string {
  if (bps === 0) return '—'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
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

  const deviceInfo = deviceDetailToInfo(device)

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
            <div className={`text-sm capitalize ${statusColors[device.status] || 'text-muted-foreground'}`}>
              {device.status}
            </div>
          </div>
        </div>

        {/* Additional device-specific info not in topology panel */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Public IP</div>
            <div className="text-sm font-mono">{device.public_ip || '—'}</div>
          </div>
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Current Traffic</div>
            <div className="text-sm">
              <span className="text-muted-foreground">In:</span> {formatBps(device.in_bps)}
              <span className="mx-2">|</span>
              <span className="text-muted-foreground">Out:</span> {formatBps(device.out_bps)}
            </div>
          </div>
          <div className="border border-border rounded-lg p-4 bg-card">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Capacity</div>
            <div className="text-sm">
              {device.current_users} / {device.max_users} users
              {device.max_users > 0 && (
                <span className="text-muted-foreground ml-1">
                  ({((device.current_users / device.max_users) * 100).toFixed(0)}%)
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Shared device info content (same as topology panel) */}
        <div className="border border-border rounded-lg p-4 bg-card">
          <DeviceInfoContent device={deviceInfo} />
        </div>
      </div>
    </div>
  )
}
