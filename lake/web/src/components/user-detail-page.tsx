import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Users, AlertCircle, ArrowLeft, Check } from 'lucide-react'
import { fetchUser } from '@/lib/api'

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
  'soft-drained': 'text-amber-600 dark:text-amber-400',
  drained: 'text-amber-600 dark:text-amber-400',
  suspended: 'text-red-600 dark:text-red-400',
  pending: 'text-amber-600 dark:text-amber-400',
}

export function UserDetailPage() {
  const { pk } = useParams<{ pk: string }>()
  const navigate = useNavigate()

  const { data: user, isLoading, error } = useQuery({
    queryKey: ['user', pk],
    queryFn: () => fetchUser(pk!),
    enabled: !!pk,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !user) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">User not found</div>
          <button
            onClick={() => navigate('/dz/users')}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Back to users
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
          onClick={() => navigate('/dz/users')}
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to users
        </button>

        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <Users className="h-8 w-8 text-muted-foreground" />
          <div>
            <h1 className="text-2xl font-medium font-mono">{user.owner_pubkey.slice(0, 8)}...{user.owner_pubkey.slice(-4)}</h1>
            <div className="text-sm text-muted-foreground">{user.kind || 'Unknown type'}</div>
          </div>
          <span className={`ml-4 capitalize ${statusColors[user.status] || ''}`}>
            {user.status}
          </span>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {/* Identity */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Identity</h3>
            <dl className="space-y-2">
              <div>
                <dt className="text-sm text-muted-foreground">Owner Pubkey</dt>
                <dd className="text-sm font-mono break-all">{user.owner_pubkey}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Kind</dt>
                <dd className="text-sm">{user.kind || '—'}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">DZ IP</dt>
                <dd className="text-sm font-mono">{user.dz_ip || '—'}</dd>
              </div>
            </dl>
          </div>

          {/* Location */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Location</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Device</dt>
                <dd className="text-sm">
                  {user.device_pk ? (
                    <Link to={`/dz/devices/${user.device_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                      {user.device_code}
                    </Link>
                  ) : '—'}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Metro</dt>
                <dd className="text-sm">
                  {user.metro_pk ? (
                    <Link to={`/dz/metros/${user.metro_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                      {user.metro_name || user.metro_code}
                    </Link>
                  ) : '—'}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Contributor</dt>
                <dd className="text-sm">
                  {user.contributor_pk ? (
                    <Link to={`/dz/contributors/${user.contributor_pk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                      {user.contributor_code}
                    </Link>
                  ) : '—'}
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
                <dd className="text-sm">{formatBps(user.in_bps)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Outbound</dt>
                <dd className="text-sm">{formatBps(user.out_bps)}</dd>
              </div>
            </dl>
          </div>

          {/* Validator Info */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-muted-foreground mb-3">Validator</h3>
            <dl className="space-y-2">
              <div className="flex justify-between items-center">
                <dt className="text-sm text-muted-foreground">Is Validator</dt>
                <dd className="text-sm">
                  {user.is_validator ? (
                    <Check className="h-4 w-4 text-green-600 dark:text-green-400" />
                  ) : '—'}
                </dd>
              </div>
              {user.is_validator && (
                <>
                  <div className="flex justify-between">
                    <dt className="text-sm text-muted-foreground">Vote Account</dt>
                    <dd className="text-sm">
                      <Link to={`/solana/validators/${user.vote_pubkey}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                        {user.vote_pubkey.slice(0, 6)}...{user.vote_pubkey.slice(-4)}
                      </Link>
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-sm text-muted-foreground">Stake</dt>
                    <dd className="text-sm">{formatStake(user.stake_sol)}</dd>
                  </div>
                </>
              )}
            </dl>
          </div>
        </div>
      </div>
    </div>
  )
}
