import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Cable, AlertCircle, ArrowLeft } from 'lucide-react'
import { fetchLink } from '@/lib/api'

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
  if (us >= 1000) return `${(us / 1000).toFixed(2)} ms`
  return `${us.toFixed(0)} µs`
}

const statusColors: Record<string, string> = {
  activated: 'text-green-600 dark:text-green-400',
  provisioning: 'text-blue-600 dark:text-blue-400',
  maintenance: 'text-amber-600 dark:text-amber-400',
  offline: 'text-red-600 dark:text-red-400',
}

export function LinkDetailPage() {
  const { pk } = useParams<{ pk: string }>()
  const navigate = useNavigate()

  const { data: link, isLoading, error } = useQuery({
    queryKey: ['link', pk],
    queryFn: () => fetchLink(pk!),
    enabled: !!pk,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !link) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Link not found</div>
          <button
            onClick={() => navigate('/dz/links')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to links
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
          onClick={() => navigate('/dz/links')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to links
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Cable className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{link.code}</h1>
            <div className="text-sm text-muted-foreground">{link.link_type}</div>
          </div>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {/* Endpoints */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Endpoints</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Status</dt>
                <dd className={`text-sm capitalize ${statusColors[link.status] || ''}`}>{link.status}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Side A</dt>
                <dd className="text-sm">
                  {link.side_a_pk ? (
                    <Link to={`/dz/devices/${link.side_a_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                      {link.side_a_code}
                    </Link>
                  ) : '—'}
                  {link.side_a_metro && <span className="text-muted-foreground ml-1">({link.side_a_metro})</span>}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Side Z</dt>
                <dd className="text-sm">
                  {link.side_z_pk ? (
                    <Link to={`/dz/devices/${link.side_z_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                      {link.side_z_code}
                    </Link>
                  ) : '—'}
                  {link.side_z_metro && <span className="text-muted-foreground ml-1">({link.side_z_metro})</span>}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Contributor</dt>
                <dd className="text-sm">
                  {link.contributor_pk ? (
                    <Link to={`/dz/contributors/${link.contributor_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                      {link.contributor_code}
                    </Link>
                  ) : '—'}
                </dd>
              </div>
            </dl>
          </div>

          {/* Capacity */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Capacity</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Bandwidth</dt>
                <dd className="text-sm">{formatBps(link.bandwidth_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Utilization In</dt>
                <dd className="text-sm">{link.utilization_in.toFixed(1)}%</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Utilization Out</dt>
                <dd className="text-sm">{link.utilization_out.toFixed(1)}%</dd>
              </div>
            </dl>
          </div>

          {/* Traffic */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Traffic</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Inbound</dt>
                <dd className="text-sm">{formatBps(link.in_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Outbound</dt>
                <dd className="text-sm">{formatBps(link.out_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Peak In (1h)</dt>
                <dd className="text-sm">{formatBps(link.peak_in_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Peak Out (1h)</dt>
                <dd className="text-sm">{formatBps(link.peak_out_bps)}</dd>
              </div>
            </dl>
          </div>

          {/* Performance */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Performance</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Latency</dt>
                <dd className="text-sm">{formatLatency(link.latency_us)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Jitter</dt>
                <dd className="text-sm">{formatLatency(link.jitter_us)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Packet Loss</dt>
                <dd className="text-sm">{link.loss_percent > 0 ? `${link.loss_percent.toFixed(2)}%` : '—'}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>
    </div>
  )
}
