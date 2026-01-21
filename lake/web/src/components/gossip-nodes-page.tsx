import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Radio, AlertCircle, Check } from 'lucide-react'
import { fetchGossipNodes } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

function formatStake(sol: number): string {
  if (sol === 0) return '—'
  if (sol >= 1e6) return `${(sol / 1e6).toFixed(2)}M`
  if (sol >= 1e3) return `${(sol / 1e3).toFixed(1)}K`
  return sol.toFixed(0)
}

function truncatePubkey(pubkey: string): string {
  if (!pubkey || pubkey.length <= 12) return pubkey || '—'
  return `${pubkey.slice(0, 6)}...${pubkey.slice(-4)}`
}

export function GossipNodesPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['gossip-nodes', offset],
    queryFn: () => fetchGossipNodes(PAGE_SIZE, offset),
    refetchInterval: 60000,
  })
  const nodes = response?.items
  const onDZCount = response?.on_dz_count ?? 0
  const validatorCount = response?.validator_count ?? 0

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
          <div className="text-lg font-medium mb-2">Unable to load gossip nodes</div>
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
          <Radio className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Gossip Nodes</h1>
          <span className="text-muted-foreground">
            ({response?.total || 0})
            {validatorCount > 0 && (
              <span className="ml-2">{validatorCount} validators</span>
            )}
            {onDZCount > 0 && (
              <span className="ml-2 text-green-600 dark:text-green-400">
                {onDZCount} on DZ
              </span>
            )}
          </span>
        </div>

        {/* Table */}
        <div className="border border-border rounded-lg overflow-hidden bg-card">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="text-sm text-left text-muted-foreground border-b border-border">
                  <th className="px-4 py-3 font-medium">Pubkey</th>
                  <th className="px-4 py-3 font-medium">IP</th>
                  <th className="px-4 py-3 font-medium">Version</th>
                  <th className="px-4 py-3 font-medium">Location</th>
                  <th className="px-4 py-3 font-medium text-center">Validator</th>
                  <th className="px-4 py-3 font-medium text-right">Stake</th>
                  <th className="px-4 py-3 font-medium text-center">DZ</th>
                  <th className="px-4 py-3 font-medium">Device</th>
                </tr>
              </thead>
              <tbody>
                {nodes?.map((node) => (
                  <tr
                    key={node.pubkey}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/solana/gossip-nodes/${node.pubkey}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm" title={node.pubkey}>
                        {truncatePubkey(node.pubkey)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground font-mono">
                      {node.gossip_ip ? `${node.gossip_ip}:${node.gossip_port}` : '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground font-mono">
                      {node.version || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {node.city || node.country ? (
                        <>
                          {node.city && <span>{node.city}</span>}
                          {node.city && node.country && <span>, </span>}
                          {node.country && <span>{node.country}</span>}
                        </>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="px-4 py-3 text-center">
                      {node.is_validator ? (
                        <Check className="h-4 w-4 text-blue-600 dark:text-blue-400 mx-auto" />
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {formatStake(node.stake_sol)}
                    </td>
                    <td className="px-4 py-3 text-center">
                      {node.on_dz ? (
                        <Check className="h-4 w-4 text-green-600 dark:text-green-400 mx-auto" />
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm">
                      {node.device_code ? (
                        <span className="font-mono">{node.device_code}</span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                      {node.metro_code && (
                        <span className="ml-1 text-xs text-muted-foreground">({node.metro_code})</span>
                      )}
                    </td>
                  </tr>
                ))}
                {(!nodes || nodes.length === 0) && (
                  <tr>
                    <td colSpan={8} className="px-4 py-8 text-center text-muted-foreground">
                      No gossip nodes found
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
