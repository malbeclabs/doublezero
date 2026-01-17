import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Server, AlertCircle } from 'lucide-react'
import { fetchDevices } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

const statusColors: Record<string, string> = {
  activated: 'text-green-600 dark:text-green-400',
  provisioning: 'text-blue-600 dark:text-blue-400',
  'soft-drained': 'text-amber-600 dark:text-amber-400',
  drained: 'text-amber-600 dark:text-amber-400',
  suspended: 'text-red-600 dark:text-red-400',
  pending: 'text-amber-600 dark:text-amber-400',
}

function formatBps(bps: number): string {
  if (bps === 0) return '—'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

export function DevicesPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['devices', offset],
    queryFn: () => fetchDevices(PAGE_SIZE, offset),
    refetchInterval: 30000,
  })
  const devices = response?.items

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Unable to load devices</div>
          <div className="text-sm text-muted-foreground">{error?.message || 'Unknown error'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-[1600px] mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center gap-3 mb-6">
          <Server className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Devices</h1>
          <span className="text-muted-foreground">({response?.total || 0})</span>
        </div>

        {/* Table */}
        <div className="border border-border rounded-lg overflow-hidden bg-card">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="text-sm text-left text-muted-foreground border-b border-border">
                  <th className="px-4 py-3 font-medium">Code</th>
                  <th className="px-4 py-3 font-medium">Type</th>
                  <th className="px-4 py-3 font-medium">Contributor</th>
                  <th className="px-4 py-3 font-medium">Metro</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium text-right">Users</th>
                  <th className="px-4 py-3 font-medium text-right">In</th>
                  <th className="px-4 py-3 font-medium text-right">Out</th>
                  <th className="px-4 py-3 font-medium text-right">Peak In</th>
                  <th className="px-4 py-3 font-medium text-right">Peak Out</th>
                </tr>
              </thead>
              <tbody>
                {devices?.map((device) => (
                  <tr
                    key={device.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/devices/${device.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{device.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground capitalize">
                      {device.device_type?.replace(/_/g, ' ')}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {device.contributor_code || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {device.metro_code || '—'}
                    </td>
                    <td className={`px-4 py-3 text-sm capitalize ${statusColors[device.status] || ''}`}>
                      {device.status}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {device.current_users > 0 ? (
                        <span>
                          {device.current_users}
                          {device.max_users > 0 && (
                            <span className="text-muted-foreground">/{device.max_users}</span>
                          )}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.out_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.peak_in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.peak_out_bps)}
                    </td>
                  </tr>
                ))}
                {(!devices || devices.length === 0) && (
                  <tr>
                    <td colSpan={10} className="px-4 py-8 text-center text-muted-foreground">
                      No devices found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={response.total}
              limit={response.limit}
              offset={response.offset}
              onOffsetChange={setOffset}
            />
          )}
        </div>
      </div>
    </div>
  )
}
