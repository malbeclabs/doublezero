import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Landmark, AlertCircle, ArrowLeft, Check } from 'lucide-react'
import { fetchValidator } from '@/lib/api'

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

export function ValidatorDetailPage() {
  const { vote_pubkey } = useParams<{ vote_pubkey: string }>()
  const navigate = useNavigate()

  const { data: validator, isLoading, error } = useQuery({
    queryKey: ['validator', vote_pubkey],
    queryFn: () => fetchValidator(vote_pubkey!),
    enabled: !!vote_pubkey,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !validator) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Validator not found</div>
          <button
            onClick={() => navigate('/solana/validators')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to validators
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
          onClick={() => navigate('/solana/validators')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to validators
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Landmark className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{validator.vote_pubkey.slice(0, 8)}...{validator.vote_pubkey.slice(-4)}</h1>
            <div className="text-sm text-muted-foreground">
              {validator.city && validator.country
                ? `${validator.city}, ${validator.country}`
                : validator.city || validator.country || 'Unknown location'}
            </div>
          </div>
          {validator.on_dz && (
            <span className="ml-4 flex items-center gap-1 text-green-600 dark:text-green-400">
              <Check className="h-4 w-4" />
              On DZ
            </span>
          )}
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {/* Identity */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Identity</h3>
            <dl className="space-y-2">
              <div>
                <dt className="text-sm text-muted-foreground">Vote Pubkey</dt>
                <dd className="text-sm font-mono break-all">{validator.vote_pubkey}</dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Node Pubkey</dt>
                <dd className="text-sm font-mono break-all">
                  <Link to={`/solana/gossip-nodes/${validator.node_pubkey}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                    {validator.node_pubkey}
                  </Link>
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Version</dt>
                <dd className="text-sm font-mono">{validator.version || '—'}</dd>
              </div>
            </dl>
          </div>

          {/* Stake */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Stake</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Stake</dt>
                <dd className="text-sm">{formatStake(validator.stake_sol)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Share</dt>
                <dd className="text-sm">{validator.stake_share.toFixed(4)}%</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Commission</dt>
                <dd className="text-sm">{validator.commission}%</dd>
              </div>
            </dl>
          </div>

          {/* Performance */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Performance</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Skip Rate</dt>
                <dd className="text-sm">{validator.skip_rate > 0 ? `${validator.skip_rate.toFixed(1)}%` : '—'}</dd>
              </div>
            </dl>
          </div>

          {/* Network */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Network</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Gossip IP</dt>
                <dd className="text-sm font-mono">{validator.gossip_ip || '—'}:{validator.gossip_port || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">City</dt>
                <dd className="text-sm">{validator.city || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Country</dt>
                <dd className="text-sm">{validator.country || '—'}</dd>
              </div>
            </dl>
          </div>

          {/* DZ Info */}
          {validator.on_dz && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-muted-foreground mb-3">DoubleZero</h3>
              <dl className="space-y-2">
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Device</dt>
                  <dd className="text-sm">
                    {validator.device_pk ? (
                      <Link to={`/dz/devices/${validator.device_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                        {validator.device_code}
                      </Link>
                    ) : '—'}
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Metro</dt>
                  <dd className="text-sm">
                    {validator.metro_pk ? (
                      <Link to={`/dz/metros/${validator.metro_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                        {validator.metro_code}
                      </Link>
                    ) : '—'}
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Inbound</dt>
                  <dd className="text-sm">{formatBps(validator.in_bps)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Outbound</dt>
                  <dd className="text-sm">{formatBps(validator.out_bps)}</dd>
                </div>
              </dl>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
