import { useEffect, useMemo, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Loader2, Users, AlertCircle, ChevronDown, ChevronUp, Search, X } from 'lucide-react'
import { fetchAllPaginated, fetchUsers } from '@/lib/api'
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

function truncatePubkey(pubkey: string): string {
  if (!pubkey || pubkey.length <= 12) return pubkey || '—'
  return `${pubkey.slice(0, 6)}...${pubkey.slice(-4)}`
}

type SortField =
  | 'owner'
  | 'kind'
  | 'dzIp'
  | 'device'
  | 'metro'
  | 'status'
  | 'in'
  | 'out'

type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

type UnitMap = Record<string, number>

const numericSearchFields: SortField[] = ['in', 'out']

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

// Parse search filters from URL param
function parseSearchFilters(searchParam: string): string[] {
  if (!searchParam) return []
  return searchParam.split(',').map(f => f.trim()).filter(Boolean)
}

// Valid filter fields for users
const validFilterFields = ['owner', 'kind', 'ip', 'dzIp', 'device', 'metro', 'status', 'in', 'out']

// Parse a filter string into field and value
function parseFilter(filter: string): { field: string; value: string } {
  const colonIndex = filter.indexOf(':')
  if (colonIndex > 0) {
    const field = filter.slice(0, colonIndex).toLowerCase()
    const value = filter.slice(colonIndex + 1)
    // Handle camelCase and alias fields
    const normalizedField = field === 'dzip' ? 'dzIp' : field === 'ip' ? 'dzIp' : field
    if (validFilterFields.includes(normalizedField) && value) {
      return { field: normalizedField, value }
    }
  }
  return { field: 'all', value: filter }
}

export function UsersPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('owner')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  // Get search filters from URL
  const searchParam = searchParams.get('search') || ''
  const searchFilters = parseSearchFilters(searchParam)

  // Use first filter for filtering (single filter supported currently)
  const activeFilterRaw = searchFilters[0] || ''

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

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['users', 'all'],
    queryFn: () => fetchAllPaginated(fetchUsers, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const users = response?.items
  const filteredUsers = useMemo(() => {
    if (!users) return []
    if (!activeFilterRaw) return users

    // Parse filter inside memo to ensure fresh parsing on each recompute
    const filter = parseFilter(activeFilterRaw)
    const searchField = filter.field as SortField | 'all'
    const needle = filter.value.trim().toLowerCase()
    if (!needle) return users

    const numericFilter = parseNumericFilter(filter.value)
    if (searchField !== 'all' && numericSearchFields.includes(searchField as SortField)) {
      const unitFilter = parseNumericFilterWithUnits(
        filter.value,
        { gbps: 1e9, mbps: 1e6, bps: 1 },
        'gbps'
      )
      const effectiveFilter = unitFilter ?? numericFilter
      if (!effectiveFilter) {
        return users
      }
      const getNumericValue = (user: typeof users[number]) => {
        switch (searchField) {
          case 'in':
            return user.in_bps
          case 'out':
            return user.out_bps
          default:
            return 0
        }
      }
      return users.filter(user => matchesNumericFilter(getNumericValue(user), effectiveFilter))
    }

    // Text search
    const getSearchValue = (user: typeof users[number], field: string) => {
      switch (field) {
        case 'owner':
          return user.owner_pubkey || ''
        case 'kind':
          return user.kind || ''
        case 'dzIp':
          return user.dz_ip || ''
        case 'device':
          return user.device_code || ''
        case 'metro':
          return `${user.metro_name || ''} ${user.metro_code || ''}`.trim()
        case 'status':
          return user.status
        default:
          return ''
      }
    }

    if (searchField === 'all') {
      // Search across all text fields
      return users.filter(user => {
        const textFields = ['owner', 'kind', 'dzIp', 'device', 'metro', 'status']
        return textFields.some(field => getSearchValue(user, field).toLowerCase().includes(needle))
      })
    }

    return users.filter(user => getSearchValue(user, searchField).toLowerCase().includes(needle))
  }, [users, activeFilterRaw])
  const sortedUsers = useMemo(() => {
    if (!filteredUsers) return []
    // Deduplicate by pk to prevent any possible duplicate rows
    const seen = new Set<string>()
    const uniqueUsers = filteredUsers.filter(user => {
      if (seen.has(user.pk)) return false
      seen.add(user.pk)
      return true
    })
    const sorted = [...uniqueUsers].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'owner':
          cmp = (a.owner_pubkey || '').localeCompare(b.owner_pubkey || '')
          break
        case 'kind':
          cmp = (a.kind || '').localeCompare(b.kind || '')
          break
        case 'dzIp':
          cmp = (a.dz_ip || '').localeCompare(b.dz_ip || '')
          break
        case 'device':
          cmp = (a.device_code || '').localeCompare(b.device_code || '')
          break
        case 'metro': {
          const aMetro = a.metro_name || a.metro_code || ''
          const bMetro = b.metro_name || b.metro_code || ''
          cmp = aMetro.localeCompare(bMetro)
          break
        }
        case 'status':
          cmp = a.status.localeCompare(b.status)
          break
        case 'in':
          cmp = a.in_bps - b.in_bps
          break
        case 'out':
          cmp = a.out_bps - b.out_bps
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredUsers, sortField, sortDirection])
  const pagedUsers = useMemo(
    () => sortedUsers.slice(offset, offset + PAGE_SIZE),
    [sortedUsers, offset]
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
          <div className="text-lg font-medium mb-2">Unable to load users</div>
          <div className="text-sm text-muted-foreground">{error?.message || 'Unknown error'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-[1400px] mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
          <div className="flex items-center gap-3">
            <Users className="h-6 w-6 text-muted-foreground" />
            <h1 className="text-2xl font-medium">Users</h1>
            <span className="text-muted-foreground">({response?.total || 0})</span>
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
        <div className="border border-border rounded-lg overflow-hidden bg-card">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="text-sm text-left text-muted-foreground border-b border-border">
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('owner')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('owner')}>
                      Owner
                      <SortIcon field="owner" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('kind')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('kind')}>
                      Kind
                      <SortIcon field="kind" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('dzIp')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('dzIp')}>
                      DZ IP
                      <SortIcon field="dzIp" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('device')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('device')}>
                      Device
                      <SortIcon field="device" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('metro')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('metro')}>
                      Metro
                      <SortIcon field="metro" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('status')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('status')}>
                      Status
                      <SortIcon field="status" />
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
                </tr>
              </thead>
              <tbody>
                {pagedUsers.map((user) => (
                  <tr
                    key={user.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/users/${user.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm" title={user.owner_pubkey}>
                        {truncatePubkey(user.owner_pubkey)}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {user.kind || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm">
                      <span className="font-mono">{user.dz_ip || '—'}</span>
                    </td>
                    <td className="px-4 py-3 text-sm">
                      <span className="font-mono">{user.device_code || '—'}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {user.metro_name || user.metro_code || '—'}
                    </td>
                    <td className={`px-4 py-3 text-sm capitalize ${statusColors[user.status] || ''}`}>
                      {user.status}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(user.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(user.out_bps)}
                    </td>
                  </tr>
                ))}
                {sortedUsers.length === 0 && (
                  <tr>
                    <td colSpan={8} className="px-4 py-8 text-center text-muted-foreground">
                      No users found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={sortedUsers.length}
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
