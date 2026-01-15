import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Link2, AlertCircle } from 'lucide-react'
import { fetchLinks } from '@/lib/api'
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

function formatLatency(us: number): string {
  if (us === 0) return '—'
  if (us >= 1000) return `${(us / 1000).toFixed(1)} ms`
  return `${us.toFixed(0)} µs`
}

function formatPercent(pct: number): string {
  if (pct === 0) return '—'
  return `${pct.toFixed(1)}%`
}

function getUtilizationColor(pct: number): string {
  if (pct >= 80) return 'text-red-600 dark:text-red-400'
  if (pct >= 60) return 'text-amber-600 dark:text-amber-400'
  if (pct > 0) return 'text-green-600 dark:text-green-400'
  return 'text-muted-foreground'
}

export function LinksPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['links', offset],
    queryFn: () => fetchLinks(PAGE_SIZE, offset),
    refetchInterval: 30000,
  })
  const links = response?.items

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
          <div className="text-lg font-medium mb-2">Unable to load links</div>
          <div className="text-sm text-muted-foreground">{error?.message || 'Unknown error'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-[1800px] mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex items-center gap-3 mb-6">
          <Link2 className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Links</h1>
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
                  <th className="px-4 py-3 font-medium">Side A</th>
                  <th className="px-4 py-3 font-medium">Side Z</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium text-right">Bandwidth</th>
                  <th className="px-4 py-3 font-medium text-right">In</th>
                  <th className="px-4 py-3 font-medium text-right">Out</th>
                  <th className="px-4 py-3 font-medium text-right">Util In</th>
                  <th className="px-4 py-3 font-medium text-right">Util Out</th>
                  <th className="px-4 py-3 font-medium text-right">Latency</th>
                  <th className="px-4 py-3 font-medium text-right">Jitter</th>
                  <th className="px-4 py-3 font-medium text-right">Loss</th>
                </tr>
              </thead>
              <tbody>
                {links?.map((link) => (
                  <tr
                    key={link.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/links/${link.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{link.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {link.link_type}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {link.contributor_code || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <span className="font-mono">{link.side_a_code || '—'}</span>
                      {link.side_a_metro && (
                        <span className="ml-1 text-xs">({link.side_a_metro})</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <span className="font-mono">{link.side_z_code || '—'}</span>
                      {link.side_z_metro && (
                        <span className="ml-1 text-xs">({link.side_z_metro})</span>
                      )}
                    </td>
                    <td className={`px-4 py-3 text-sm capitalize ${statusColors[link.status] || ''}`}>
                      {link.status}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.bandwidth_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.out_bps)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${getUtilizationColor(link.utilization_in)}`}>
                      {formatPercent(link.utilization_in)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${getUtilizationColor(link.utilization_out)}`}>
                      {formatPercent(link.utilization_out)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatLatency(link.latency_us)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatLatency(link.jitter_us)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${link.loss_percent > 0 ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground'}`}>
                      {formatPercent(link.loss_percent)}
                    </td>
                  </tr>
                ))}
                {(!links || links.length === 0) && (
                  <tr>
                    <td colSpan={14} className="px-4 py-8 text-center text-muted-foreground">
                      No links found
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
