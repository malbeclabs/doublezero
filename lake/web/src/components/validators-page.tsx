import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Landmark, AlertCircle, Check, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchAllPaginated, fetchValidators } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

function formatBps(bps: number): string {
  if (bps === 0) return '—'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

function formatStake(sol: number): string {
  if (sol >= 1e6) return `${(sol / 1e6).toFixed(2)}M`
  if (sol >= 1e3) return `${(sol / 1e3).toFixed(1)}K`
  return sol.toFixed(0)
}

function truncatePubkey(pubkey: string): string {
  if (!pubkey || pubkey.length <= 12) return pubkey || '—'
  return `${pubkey.slice(0, 6)}...${pubkey.slice(-4)}`
}

function getSkipRateColor(rate: number): string {
  if (rate >= 20) return 'text-red-600 dark:text-red-400'
  if (rate >= 10) return 'text-amber-600 dark:text-amber-400'
  if (rate > 0) return 'text-green-600 dark:text-green-400'
  return 'text-muted-foreground'
}

type SortField =
  | 'vote'
  | 'node'
  | 'stake'
  | 'share'
  | 'commission'
  | 'dz'
  | 'device'
  | 'location'
  | 'in'
  | 'out'
  | 'skip'
  | 'version'

type SortDirection = 'asc' | 'desc'

export function ValidatorsPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('vote')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['validators', 'all'],
    queryFn: () => fetchAllPaginated(fetchValidators, PAGE_SIZE),
    refetchInterval: 60000,
  })
  const validators = response?.items
  const onDZCount = response?.on_dz_count ?? 0
  const sortedValidators = useMemo(() => {
    if (!validators) return []
    const sorted = [...validators].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'vote':
          cmp = a.vote_pubkey.localeCompare(b.vote_pubkey)
          break
        case 'node':
          cmp = (a.node_pubkey || '').localeCompare(b.node_pubkey || '')
          break
        case 'stake':
          cmp = a.stake_sol - b.stake_sol
          break
        case 'share':
          cmp = a.stake_share - b.stake_share
          break
        case 'commission':
          cmp = a.commission - b.commission
          break
        case 'dz':
          cmp = Number(a.on_dz) - Number(b.on_dz)
          break
        case 'device':
          cmp = (a.device_code || '').localeCompare(b.device_code || '')
          break
        case 'location': {
          const aLoc = `${a.city || ''} ${a.country || ''}`.trim()
          const bLoc = `${b.city || ''} ${b.country || ''}`.trim()
          cmp = aLoc.localeCompare(bLoc)
          break
        }
        case 'in':
          cmp = a.in_bps - b.in_bps
          break
        case 'out':
          cmp = a.out_bps - b.out_bps
          break
        case 'skip':
          cmp = a.skip_rate - b.skip_rate
          break
        case 'version':
          cmp = (a.version || '').localeCompare(b.version || '')
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [validators, sortField, sortDirection])
  const pagedValidators = useMemo(
    () => sortedValidators.slice(offset, offset + PAGE_SIZE),
    [sortedValidators, offset]
  )

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(current => current === 'asc' ? 'desc' : 'asc')
      return
    }
    setSortField(field)
    setSortDirection('asc')
  }

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return null
    return sortDirection === 'asc'
      ? <ChevronUp className="h-3 w-3" />
      : <ChevronDown className="h-3 w-3" />
  }

  const sortAria = (field: SortField) => {
    if (sortField !== field) return 'none'
    return sortDirection === 'asc' ? 'ascending' : 'descending'
  }

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
          <div className="text-lg font-medium mb-2">Unable to load validators</div>
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
          <Landmark className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Validators</h1>
          <span className="text-muted-foreground">
            ({response?.total || 0})
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('vote')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('vote')}>
                      Vote Account
                      <SortIcon field="vote" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('node')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('node')}>
                      Node
                      <SortIcon field="node" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('stake')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('stake')}>
                      Stake
                      <SortIcon field="stake" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('share')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('share')}>
                      Share
                      <SortIcon field="share" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('commission')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('commission')}>
                      Comm.
                      <SortIcon field="commission" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-center" aria-sort={sortAria('dz')}>
                    <button className="inline-flex items-center gap-1 justify-center w-full" type="button" onClick={() => handleSort('dz')}>
                      DZ
                      <SortIcon field="dz" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('device')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('device')}>
                      Device
                      <SortIcon field="device" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('location')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('location')}>
                      Location
                      <SortIcon field="location" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('in')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('in')}>
                      In
                      <SortIcon field="in" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('out')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('out')}>
                      Out
                      <SortIcon field="out" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('skip')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('skip')}>
                      Skip
                      <SortIcon field="skip" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('version')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('version')}>
                      Version
                      <SortIcon field="version" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {pagedValidators.map((validator) => (
                  <tr
                    key={validator.vote_pubkey}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/solana/validators/${validator.vote_pubkey}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm" title={validator.vote_pubkey}>
                        {truncatePubkey(validator.vote_pubkey)}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm text-muted-foreground" title={validator.node_pubkey}>
                        {truncatePubkey(validator.node_pubkey)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {formatStake(validator.stake_sol)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {validator.stake_share.toFixed(2)}%
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {validator.commission}%
                    </td>
                    <td className="px-4 py-3 text-center">
                      {validator.on_dz ? (
                        <Check className="h-4 w-4 text-green-600 dark:text-green-400 mx-auto" />
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm">
                      {validator.device_code ? (
                        <span className="font-mono">{validator.device_code}</span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                      {validator.metro_code && (
                        <span className="ml-1 text-xs text-muted-foreground">({validator.metro_code})</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {validator.city || validator.country ? (
                        <>
                          {validator.city && <span>{validator.city}</span>}
                          {validator.city && validator.country && <span>, </span>}
                          {validator.country && <span>{validator.country}</span>}
                        </>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(validator.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(validator.out_bps)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${getSkipRateColor(validator.skip_rate)}`}>
                      {validator.skip_rate > 0 ? `${validator.skip_rate.toFixed(1)}%` : '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground font-mono">
                      {validator.version || '—'}
                    </td>
                  </tr>
                ))}
                {sortedValidators.length === 0 && (
                  <tr>
                    <td colSpan={12} className="px-4 py-8 text-center text-muted-foreground">
                      No validators found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={response.total}
              limit={PAGE_SIZE}
              offset={offset}
              onOffsetChange={setOffset}
            />
          )}
        </div>
      </div>
    </div>
  )
}
