import { useEffect, useMemo, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Loader2, Server, AlertCircle, ChevronDown, ChevronUp, Search, X } from 'lucide-react'
import { fetchAllPaginated, fetchDevices } from '@/lib/api'
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

type SortField =
  | 'code'
  | 'type'
  | 'contributor'
  | 'metro'
  | 'status'
  | 'users'
  | 'in'
  | 'out'
  | 'peakIn'
  | 'peakOut'

type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

type UnitMap = Record<string, number>

const numericSearchFields: SortField[] = [
  'users',
  'in',
  'out',
  'peakIn',
  'peakOut',
]

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

// Valid filter fields for devices
const validFilterFields = ['code', 'type', 'contributor', 'metro', 'status', 'users', 'in', 'out', 'peakIn', 'peakOut']

// Parse a filter string into field and value
function parseFilter(filter: string): { field: string; value: string } {
  const colonIndex = filter.indexOf(':')
  if (colonIndex > 0) {
    const field = filter.slice(0, colonIndex).toLowerCase()
    const value = filter.slice(colonIndex + 1)
    if (validFilterFields.includes(field) && value) {
      return { field, value }
    }
  }
  return { field: 'all', value: filter }
}

export function DevicesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [offset, setOffset] = useState(0)

  // Get sort config from URL (default: code asc)
  const sortField = (searchParams.get('sort') || 'code') as SortField
  const sortDirection = (searchParams.get('dir') || 'asc') as SortDirection

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
    queryKey: ['devices', 'all'],
    queryFn: () => fetchAllPaginated(fetchDevices, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const devices = response?.items
  const filteredDevices = useMemo(() => {
    if (!devices) return []
    if (!activeFilterRaw) return devices

    // Parse filter inside memo to ensure fresh parsing on each recompute
    const filter = parseFilter(activeFilterRaw)
    const searchField = filter.field as SortField | 'all'
    const needle = filter.value.trim().toLowerCase()
    if (!needle) return devices

    const numericFilter = parseNumericFilter(filter.value)
    if (searchField !== 'all' && numericSearchFields.includes(searchField as SortField)) {
      const unitFilter =
        (searchField === 'in' || searchField === 'out' || searchField === 'peakIn' || searchField === 'peakOut')
          ? parseNumericFilterWithUnits(
              filter.value,
              { gbps: 1e9, mbps: 1e6, bps: 1 },
              'gbps'
            )
          : null
      const effectiveFilter = unitFilter ?? numericFilter
      if (!effectiveFilter) {
        return devices
      }
      const getNumericValue = (device: typeof devices[number]) => {
        switch (searchField) {
          case 'users':
            return device.current_users
          case 'in':
            return device.in_bps
          case 'out':
            return device.out_bps
          case 'peakIn':
            return device.peak_in_bps
          case 'peakOut':
            return device.peak_out_bps
          default:
            return 0
        }
      }
      return devices.filter(device => matchesNumericFilter(getNumericValue(device), effectiveFilter))
    }

    // Text search
    const getSearchValue = (device: typeof devices[number], field: string) => {
      switch (field) {
        case 'code':
          return device.code
        case 'type': {
          const type = device.device_type || ''
          return `${type} ${type.replace(/_/g, ' ')}`.trim()
        }
        case 'contributor':
          return device.contributor_code || ''
        case 'metro':
          return device.metro_code || ''
        case 'status':
          return device.status
        case 'users':
          return `${device.current_users} ${device.max_users}`
        default:
          return ''
      }
    }

    if (searchField === 'all') {
      // Search across all text fields
      return devices.filter(device => {
        const textFields = ['code', 'type', 'contributor', 'metro', 'status']
        return textFields.some(field => getSearchValue(device, field).toLowerCase().includes(needle))
      })
    }

    return devices.filter(device => getSearchValue(device, searchField).toLowerCase().includes(needle))
  }, [devices, activeFilterRaw])
  const sortedDevices = useMemo(() => {
    if (!filteredDevices) return []
    // Deduplicate by pk to prevent any possible duplicate rows
    const seen = new Set<string>()
    const unique = filteredDevices.filter(d => {
      if (seen.has(d.pk)) return false
      seen.add(d.pk)
      return true
    })
    const sorted = [...unique].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'code':
          cmp = a.code.localeCompare(b.code)
          break
        case 'type':
          cmp = (a.device_type || '').localeCompare(b.device_type || '')
          break
        case 'contributor':
          cmp = (a.contributor_code || '').localeCompare(b.contributor_code || '')
          break
        case 'metro':
          cmp = (a.metro_code || '').localeCompare(b.metro_code || '')
          break
        case 'status':
          cmp = a.status.localeCompare(b.status)
          break
        case 'users':
          cmp = a.current_users - b.current_users
          break
        case 'in':
          cmp = a.in_bps - b.in_bps
          break
        case 'out':
          cmp = a.out_bps - b.out_bps
          break
        case 'peakIn':
          cmp = a.peak_in_bps - b.peak_in_bps
          break
        case 'peakOut':
          cmp = a.peak_out_bps - b.peak_out_bps
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredDevices, sortField, sortDirection])
  const pagedDevices = useMemo(
    () => sortedDevices.slice(offset, offset + PAGE_SIZE),
    [sortedDevices, offset]
  )

  const handleSort = (field: SortField) => {
    setSearchParams(prev => {
      if (sortField === field) {
        prev.set('dir', sortDirection === 'asc' ? 'desc' : 'asc')
      } else {
        prev.set('sort', field)
        prev.set('dir', 'asc')
      }
      return prev
    })
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
          <div className="text-lg font-medium mb-2">Unable to load devices</div>
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
            <Server className="h-6 w-6 text-muted-foreground" />
            <h1 className="text-2xl font-medium">Devices</h1>
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('code')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('code')}>
                      Code
                      <SortIcon field="code" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('type')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('type')}>
                      Type
                      <SortIcon field="type" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('contributor')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('contributor')}>
                      Contributor
                      <SortIcon field="contributor" />
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
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('users')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('users')}>
                      Users
                      <SortIcon field="users" />
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
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('peakIn')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('peakIn')}>
                      Peak In
                      <SortIcon field="peakIn" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('peakOut')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('peakOut')}>
                      Peak Out
                      <SortIcon field="peakOut" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {pagedDevices.map((device) => (
                  <tr
                    key={device.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/devices/${device.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{device.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground capitalize">
                      {device.device_type?.replace(/_/g, ' ')}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {device.contributor_code || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {device.metro_code || '—'}
                    </td>
                    <td className={`px-4 py-3 text-sm capitalize ${statusColors[device.status] || ''}`}>
                      {device.status}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {device.current_users > 0 ? (
                        <span>
                          {device.current_users}
                          {device.max_users > 0 && (
                            <span className="text-muted-foreground">/{device.max_users}</span>
                          )}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.out_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.peak_in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(device.peak_out_bps)}
                    </td>
                  </tr>
                ))}
                {sortedDevices.length === 0 && (
                  <tr>
                    <td colSpan={10} className="px-4 py-8 text-center text-muted-foreground">
                      No devices found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={sortedDevices.length}
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
