import { useEffect, useState, useCallback } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Loader2, Radio, AlertCircle, Check, ChevronDown, ChevronUp, Search, X } from 'lucide-react'
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

type SortField =
  | 'pubkey'
  | 'ip'
  | 'version'
  | 'city'
  | 'country'
  | 'validator'
  | 'stake'
  | 'dz'
  | 'device'

type SortDirection = 'asc' | 'desc'

// Parse search filters from URL param
function parseSearchFilters(searchParam: string): string[] {
  if (!searchParam) return []
  return searchParam.split(',').map(f => f.trim()).filter(Boolean)
}

// Valid filter fields for gossip nodes
const validFilterFields = ['pubkey', 'ip', 'version', 'city', 'country', 'validator', 'stake', 'dz', 'device']

// Parse a filter string into field and value
// Supports "field:value" syntax or plain "value" for keyword search
function parseFilter(filter: string): { field: string; value: string } {
  const colonIndex = filter.indexOf(':')
  if (colonIndex > 0) {
    const field = filter.slice(0, colonIndex).toLowerCase()
    const value = filter.slice(colonIndex + 1)
    if (validFilterFields.includes(field) && value) {
      return { field, value }
    }
  }
  // Plain keyword search
  return { field: 'all', value: filter }
}

export function GossipNodesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [offset, setOffset] = useState(0)

  // Get sort config from URL (default: stake desc)
  const sortField = (searchParams.get('sort') || 'stake') as SortField
  const sortDirection = (searchParams.get('dir') || 'desc') as SortDirection

  // Get search filters from URL
  const searchParam = searchParams.get('search') || ''
  const searchFilters = parseSearchFilters(searchParam)

  // Use first filter for API (single filter supported currently)
  const activeFilterRaw = searchFilters[0] || ''
  const activeFilter = activeFilterRaw ? parseFilter(activeFilterRaw) : null

  const { data: response, isLoading, isFetching, error } = useQuery({
    queryKey: ['gossip-nodes', offset, sortField, sortDirection, activeFilterRaw],
    queryFn: () => fetchGossipNodes(
      PAGE_SIZE,
      offset,
      sortField,
      sortDirection,
      activeFilter?.field,
      activeFilter?.value
    ),
    refetchInterval: 60000,
    placeholderData: keepPreviousData,
  })
  const nodes = response?.items ?? []
  const onDZCount = response?.on_dz_count ?? 0
  const validatorCount = response?.validator_count ?? 0

  const removeFilter = useCallback((filterToRemove: string) => {
    const newFilters = searchFilters.filter(f => f !== filterToRemove)
    setSearchParams(prev => {
      if (newFilters.length === 0) {
        prev.delete('search')
      } else {
        prev.set('search', newFilters.join(','))
      }
      return prev
    })
  }, [searchFilters, setSearchParams])

  const clearAllFilters = useCallback(() => {
    setSearchParams(prev => {
      prev.delete('search')
      return prev
    })
  }, [setSearchParams])

  const openSearch = useCallback(() => {
    window.dispatchEvent(new CustomEvent('open-search'))
  }, [])

  const handleSort = (field: SortField) => {
    setSearchParams(prev => {
      if (sortField === field) {
        prev.set('dir', sortDirection === 'asc' ? 'desc' : 'asc')
      } else {
        prev.set('sort', field)
        prev.set('dir', 'desc')
      }
      return prev
    })
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
  }, [activeFilterRaw])

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
        <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
          <div className="flex items-center gap-3">
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
          <div className="flex items-center gap-2 flex-wrap">
            {/* Filter tags */}
            {searchFilters.map((filter, idx) => (
              <button
                key={`${filter}-${idx}`}
                onClick={() => removeFilter(filter)}
                className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 hover:bg-blue-500/20 transition-colors"
              >
                {filter}
                <X className="h-3 w-3" />
              </button>
            ))}

            {/* Clear all */}
            {searchFilters.length > 1 && (
              <button
                onClick={clearAllFilters}
                className="text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                Clear all
              </button>
            )}

            {/* Search button */}
            <button
              onClick={openSearch}
              className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground hover:text-foreground border border-border rounded-md bg-background hover:bg-muted transition-colors"
              title="Search (Cmd+K)"
            >
              <Search className="h-3 w-3" />
              <span>Filter</span>
              <kbd className="ml-0.5 font-mono text-[10px] text-muted-foreground/70">⌘K</kbd>
            </button>
          </div>
        </div>

        {/* Table */}
        <div className={`border border-border rounded-lg overflow-hidden bg-card transition-opacity ${isFetching ? 'opacity-60' : ''}`}>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="text-sm text-left text-muted-foreground border-b border-border">
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('pubkey')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('pubkey')}>
                      Pubkey
                      <SortIcon field="pubkey" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('ip')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('ip')}>
                      IP
                      <SortIcon field="ip" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('version')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('version')}>
                      Version
                      <SortIcon field="version" />
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
                  <th className="px-4 py-3 font-medium text-center" aria-sort={sortAria('validator')}>
                    <button className="inline-flex items-center gap-1 justify-center w-full" type="button" onClick={() => handleSort('validator')}>
                      Validator
                      <SortIcon field="validator" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('stake')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('stake')}>
                      Stake
                      <SortIcon field="stake" />
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
                </tr>
              </thead>
              <tbody>
                {nodes.map((node) => (
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
                      {node.city || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {node.country || '—'}
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
                {nodes.length === 0 && (
                  <tr>
                    <td colSpan={9} className="px-4 py-8 text-center text-muted-foreground">
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
