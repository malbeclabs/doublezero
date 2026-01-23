import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Radio, AlertCircle, Check, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchAllPaginated, fetchGossipNodes } from '@/lib/api'
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
  | 'location'
  | 'validator'
  | 'stake'
  | 'dz'
  | 'device'

type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

type UnitMap = Record<string, number>

const numericSearchFields: SortField[] = ['stake']

function parseNumericFilter(input: string): NumericFilter | null {
  const match = input.trim().match(/^(>=|<=|>|<|==|=)\s*(-?\d+(?:\.\d+)?)$/)
  if (!match) return null
  const op = match[1] === '==' ? '=' : (match[1] as NumericFilter['op'])
  return { op, value: Number(match[2]) }
}

function parseNumericFilterWithUnits(
  input: string,
  unitMap: UnitMap,
  defaultUnit: string
): NumericFilter | null {
  const match = input.trim().match(/^(>=|<=|>|<|==|=)\s*(-?\d+(?:\.\d+)?)([a-zA-Z]+)?$/)
  if (!match) return null
  const op = match[1] === '==' ? '=' : (match[1] as NumericFilter['op'])
  const unitRaw = match[3]?.toLowerCase()
  const unit = unitRaw ?? defaultUnit
  const multiplier = unitMap[unit]
  if (!multiplier) return null
  return { op, value: Number(match[2]) * multiplier }
}

function matchesNumericFilter(value: number, filter: NumericFilter): boolean {
  switch (filter.op) {
    case '>':
      return value > filter.value
    case '>=':
      return value >= filter.value
    case '<':
      return value < filter.value
    case '<=':
      return value <= filter.value
    case '=':
      return value === filter.value
  }
}

export function GossipNodesPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('pubkey')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [searchField, setSearchField] = useState<SortField>('pubkey')
  const [searchText, setSearchText] = useState('')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['gossip-nodes', 'all'],
    queryFn: () => fetchAllPaginated(fetchGossipNodes, PAGE_SIZE),
    refetchInterval: 60000,
  })
  const nodes = response?.items
  const onDZCount = response?.on_dz_count ?? 0
  const validatorCount = response?.validator_count ?? 0
  const filteredNodes = useMemo(() => {
    if (!nodes) return []
    const needle = searchText.trim().toLowerCase()
    if (!needle) return nodes
    const numericFilter = parseNumericFilter(searchText)
    if (numericSearchFields.includes(searchField)) {
      const unitFilter =
        searchField === 'stake'
          ? parseNumericFilterWithUnits(searchText, { '': 1, k: 1e3, m: 1e6 }, '')
          : null
      const effectiveFilter = unitFilter ?? numericFilter
      if (!effectiveFilter) {
        return nodes
      }
      const getNumericValue = (node: typeof nodes[number]) => {
        switch (searchField) {
          case 'stake':
            return node.stake_sol
          default:
            return 0
        }
      }
      return nodes.filter(node => matchesNumericFilter(getNumericValue(node), effectiveFilter))
    }
    const getSearchValue = (node: typeof nodes[number]) => {
      switch (searchField) {
        case 'pubkey':
          return node.pubkey
        case 'ip':
          return node.gossip_ip ? `${node.gossip_ip}:${node.gossip_port}` : ''
        case 'version':
          return node.version || ''
        case 'location':
          return `${node.city || ''} ${node.country || ''}`.trim()
        case 'validator':
          return node.is_validator ? 'yes' : 'no'
        case 'stake':
          return String(node.stake_sol)
        case 'dz':
          return node.on_dz ? 'yes' : 'no'
        case 'device':
          return `${node.device_code || ''} ${node.metro_code || ''}`.trim()
      }
    }
    return nodes.filter(node => getSearchValue(node).toLowerCase().includes(needle))
  }, [nodes, searchField, searchText])
  const sortedNodes = useMemo(() => {
    if (!filteredNodes) return []
    const sorted = [...filteredNodes].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'pubkey':
          cmp = a.pubkey.localeCompare(b.pubkey)
          break
        case 'ip': {
          const aIp = a.gossip_ip ? `${a.gossip_ip}:${a.gossip_port}` : ''
          const bIp = b.gossip_ip ? `${b.gossip_ip}:${b.gossip_port}` : ''
          cmp = aIp.localeCompare(bIp)
          break
        }
        case 'version':
          cmp = (a.version || '').localeCompare(b.version || '')
          break
        case 'location': {
          const aLoc = `${a.city || ''} ${a.country || ''}`.trim()
          const bLoc = `${b.city || ''} ${b.country || ''}`.trim()
          cmp = aLoc.localeCompare(bLoc)
          break
        }
        case 'validator':
          cmp = Number(a.is_validator) - Number(b.is_validator)
          break
        case 'stake':
          cmp = a.stake_sol - b.stake_sol
          break
        case 'dz':
          cmp = Number(a.on_dz) - Number(b.on_dz)
          break
        case 'device':
          cmp = (a.device_code || '').localeCompare(b.device_code || '')
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredNodes, sortField, sortDirection])
  const pagedNodes = useMemo(
    () => sortedNodes.slice(offset, offset + PAGE_SIZE),
    [sortedNodes, offset]
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

  useEffect(() => {
    setOffset(0)
  }, [searchField, searchText])

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
          <div className="flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={searchField}
              onChange={(e) => setSearchField(e.target.value as SortField)}
            >
              <option value="pubkey">Pubkey</option>
              <option value="ip">IP</option>
              <option value="version">Version</option>
              <option value="location">Location</option>
              <option value="validator">Validator</option>
              <option value="stake">Stake</option>
              <option value="dz">DZ</option>
              <option value="device">Device</option>
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
        <div className="border border-border rounded-lg overflow-hidden bg-card">
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('location')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('location')}>
                      Location
                      <SortIcon field="location" />
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
                {pagedNodes.map((node) => (
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
                {sortedNodes.length === 0 && (
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
              total={sortedNodes.length}
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
