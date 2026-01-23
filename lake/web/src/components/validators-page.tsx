import { useEffect, useState } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Landmark, AlertCircle, Check, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchValidators } from '@/lib/api'
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
  | 'city'
  | 'country'
  | 'in'
  | 'out'
  | 'skip'
  | 'version'

type SortDirection = 'asc' | 'desc'

// Hook for debounced value
function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value)

  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedValue(value)
    }, delay)

    return () => {
      clearTimeout(handler)
    }
  }, [value, delay])

  return debouncedValue
}

export function ValidatorsPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('stake')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')
  const [searchField, setSearchField] = useState<SortField>('vote')
  const [searchText, setSearchText] = useState('')

  // Debounce filter value to avoid too many requests
  const debouncedSearchText = useDebounce(searchText, 300)

  const { data: response, isLoading, isFetching, error } = useQuery({
    queryKey: ['validators', offset, sortField, sortDirection, searchField, debouncedSearchText],
    queryFn: () => fetchValidators(
      PAGE_SIZE,
      offset,
      sortField,
      sortDirection,
      debouncedSearchText ? searchField : undefined,
      debouncedSearchText || undefined
    ),
    refetchInterval: 60000,
    placeholderData: keepPreviousData,
  })
  const validators = response?.items ?? []
  const onDZCount = response?.on_dz_count ?? 0

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(current => current === 'asc' ? 'desc' : 'asc')
      return
    }
    setSortField(field)
    setSortDirection('desc')
    setOffset(0)
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

  // Reset to first page when filter changes
  useEffect(() => {
    setOffset(0)
  }, [searchField, debouncedSearchText])

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
        <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
          <div className="flex items-center gap-3">
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
          <div className="flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={searchField}
              onChange={(e) => setSearchField(e.target.value as SortField)}
            >
              <option value="vote">Vote Account</option>
              <option value="node">Node</option>
              <option value="stake">Stake</option>
              <option value="share">Share</option>
              <option value="commission">Comm.</option>
              <option value="dz">DZ</option>
              <option value="device">Device</option>
              <option value="city">City</option>
              <option value="country">Country</option>
              <option value="in">In</option>
              <option value="out">Out</option>
              <option value="skip">Skip</option>
              <option value="version">Version</option>
            </select>
            <div className="relative">
              <input
                className="h-9 w-48 sm:w-64 rounded-md border border-border bg-background px-3 pr-8 text-sm"
                value={searchText}
                onChange={(e) => setSearchText(e.target.value)}
                placeholder="Filter"
                aria-label="Filter"
              />
              {searchText && (
                <button
                  type="button"
                  className="absolute inset-y-0 right-2 text-muted-foreground hover:text-foreground"
                  onClick={() => setSearchText('')}
                  aria-label="Clear filter"
                >
                  ×
                </button>
              )}
            </div>
          </div>
        </div>

        {/* Table */}
        <div className={`border border-border rounded-lg overflow-hidden bg-card transition-opacity ${isFetching ? 'opacity-60' : ''}`}>
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('city')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('city')}>
                      City
                      <SortIcon field="city" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('country')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('country')}>
                      Country
                      <SortIcon field="country" />
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
                {validators.map((validator) => (
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
                      {validator.city || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {validator.country || '—'}
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
                {validators.length === 0 && (
                  <tr>
                    <td colSpan={13} className="px-4 py-8 text-center text-muted-foreground">
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
