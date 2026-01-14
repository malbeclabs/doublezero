import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Radio, AlertCircle, ArrowLeft, Check } from 'lucide-react'
import { fetchGossipNode } from '@/lib/api'

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

export function GossipNodeDetailPage() {
  const { pubkey } = useParams<{ pubkey: string }>()
  const navigate = useNavigate()

  const { data: node, isLoading, error } = useQuery({
    queryKey: ['gossip-node', pubkey],
    queryFn: () => fetchGossipNode(pubkey!),
    enabled: !!pubkey,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !node) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Gossip node not found</div>
          <button
            onClick={() => navigate('/solana/gossip-nodes')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to gossip nodes
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
          onClick={() => navigate('/solana/gossip-nodes')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to gossip nodes
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Radio className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{node.pubkey.slice(0, 8)}...{node.pubkey.slice(-4)}</h1>
            <div className="text-sm text-muted-foreground">
              {node.city && node.country
                ? `${node.city}, ${node.country}`
                : node.city || node.country || 'Unknown location'}
            </div>
          </div>
          <div className="ml-4 flex items-center gap-3">
            {node.is_validator && (
              <span className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
                <Check className="h-4 w-4" />
                Validator
              </span>
            )}
            {node.on_dz && (
              <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
                <Check className="h-4 w-4" />
                On DZ
              </span>
            )}
          </div>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {/* Identity */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Identity</h3>
            <dl className="space-y-2">
              <div>
                <dt className="text-sm text-muted-foreground">Pubkey</dt>
                <dd className="text-sm font-mono break-all">{node.pubkey}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Version</dt>
                <dd className="text-sm font-mono">{node.version || '—'}</dd>
              </div>
            </dl>
          </div>

          {/* Network */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Network</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Gossip IP</dt>
                <dd className="text-sm font-mono">{node.gossip_ip || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Port</dt>
                <dd className="text-sm font-mono">{node.gossip_port || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">City</dt>
                <dd className="text-sm">{node.city || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Country</dt>
                <dd className="text-sm">{node.country || '—'}</dd>
              </div>
            </dl>
          </div>

          {/* Validator Info */}
          {node.is_validator && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-muted-foreground mb-3">Validator</h3>
              <dl className="space-y-2">
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Vote Account</dt>
                  <dd className="text-sm">
                    <Link to={`/solana/validators/${node.vote_pubkey}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                      {node.vote_pubkey.slice(0, 6)}...{node.vote_pubkey.slice(-4)}
                    </Link>
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Stake</dt>
                  <dd className="text-sm">{formatStake(node.stake_sol)}</dd>
                </div>
              </dl>
            </div>
          )}

          {/* DZ Info */}
          {node.on_dz && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-muted-foreground mb-3">DoubleZero</h3>
              <dl className="space-y-2">
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Device</dt>
                  <dd className="text-sm">
                    {node.device_pk ? (
                      <Link to={`/dz/devices/${node.device_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                        {node.device_code}
                      </Link>
                    ) : '—'}
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Metro</dt>
                  <dd className="text-sm">
                    {node.metro_pk ? (
                      <Link to={`/dz/metros/${node.metro_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                        {node.metro_code}
                      </Link>
                    ) : '—'}
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Inbound</dt>
                  <dd className="text-sm">{formatBps(node.in_bps)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Outbound</dt>
                  <dd className="text-sm">{formatBps(node.out_bps)}</dd>
                </div>
              </dl>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
