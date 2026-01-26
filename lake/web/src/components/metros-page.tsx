import { useEffect, useMemo, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Loader2, MapPin, AlertCircle, ChevronDown, ChevronUp, X } from 'lucide-react'
import { fetchAllPaginated, fetchMetros } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'
import { InlineFilter } from './inline-filter'

const PAGE_SIZE = 100

type SortField = 'code' | 'name' | 'latitude' | 'longitude' | 'devices' | 'users'
type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

const numericSearchFields: SortField[] = ['latitude', 'longitude', 'devices', 'users']

function parseNumericFilter(input: string): NumericFilter | null {
  const match = input.trim().match(/^(>=|<=|>|<|==|=)\s*(-?\d+(?:\.\d+)?)$/)
  if (!match) return null
  const op = match[1] === '==' ? '=' : (match[1] as NumericFilter['op'])
  return { op, value: Number(match[2]) }
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

// Valid filter fields for metros
const validFilterFields = ['code', 'name', 'latitude', 'longitude', 'devices', 'users']

// Field prefixes for inline filter
const metroFieldPrefixes = [
  { prefix: 'code:', description: 'Filter by metro code' },
  { prefix: 'name:', description: 'Filter by metro name' },
  { prefix: 'devices:', description: 'Filter by device count (e.g., >5)' },
  { prefix: 'users:', description: 'Filter by user count (e.g., >10)' },
]

// Fields that support autocomplete (none for metros)
const metroAutocompleteFields: string[] = []

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

export function MetrosPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [offset, setOffset] = useState(0)
  const [liveFilter, setLiveFilter] = useState('')

  // Get sort config from URL (default: code asc)
  const sortField = (searchParams.get('sort') || 'code') as SortField
  const sortDirection = (searchParams.get('dir') || 'asc') as SortDirection

  // Get search filters from URL
  const searchParam = searchParams.get('search') || ''
  const searchFilters = parseSearchFilters(searchParam)

  // Combine committed filters with live filter
  const activeFilterRaw = liveFilter || searchFilters[0] || ''

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

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['metros', 'all'],
    queryFn: () => fetchAllPaginated(fetchMetros, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const metros = response?.items
  const filteredMetros = useMemo(() => {
    if (!metros) return []
    if (!activeFilterRaw) return metros

    // Parse filter inside memo to ensure fresh parsing on each recompute
    const filter = parseFilter(activeFilterRaw)
    const searchField = filter.field as SortField | 'all'
    const needle = filter.value.trim().toLowerCase()
    if (!needle) return metros

    const numericFilter = parseNumericFilter(filter.value)
    if (searchField !== 'all' && numericFilter && numericSearchFields.includes(searchField as SortField)) {
      const getNumericValue = (metro: typeof metros[number]) => {
        switch (searchField) {
          case 'latitude':
            return metro.latitude
          case 'longitude':
            return metro.longitude
          case 'devices':
            return metro.device_count
          case 'users':
            return metro.user_count
          default:
            return 0
        }
      }
      return metros.filter(metro => matchesNumericFilter(getNumericValue(metro), numericFilter))
    }

    // Text search
    const getSearchValue = (metro: typeof metros[number], field: string) => {
      switch (field) {
        case 'code':
          return metro.code
        case 'name':
          return metro.name || ''
        default:
          return ''
      }
    }

    if (searchField === 'all') {
      // Search across all text fields
      return metros.filter(metro => {
        const textFields = ['code', 'name']
        return textFields.some(field => getSearchValue(metro, field).toLowerCase().includes(needle))
      })
    }

    return metros.filter(metro => getSearchValue(metro, searchField).toLowerCase().includes(needle))
  }, [metros, activeFilterRaw])
  const sortedMetros = useMemo(() => {
    if (!filteredMetros) return []
    // Deduplicate by pk to prevent any possible duplicate rows
    const seen = new Set<string>()
    const unique = filteredMetros.filter(m => {
      if (seen.has(m.pk)) return false
      seen.add(m.pk)
      return true
    })
    const sorted = [...unique].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'code':
          cmp = a.code.localeCompare(b.code)
          break
        case 'name':
          cmp = (a.name || '').localeCompare(b.name || '')
          break
        case 'latitude':
          cmp = a.latitude - b.latitude
          break
        case 'longitude':
          cmp = a.longitude - b.longitude
          break
        case 'devices':
          cmp = a.device_count - b.device_count
          break
        case 'users':
          cmp = a.user_count - b.user_count
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredMetros, sortField, sortDirection])
  const pagedMetros = useMemo(
    () => sortedMetros.slice(offset, offset + PAGE_SIZE),
    [sortedMetros, offset]
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
          <div className="text-lg font-medium mb-2">Unable to load metros</div>
          <div className="text-sm text-muted-foreground">{error?.message || 'Unknown error'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-7xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-3 mb-6">
          <div className="flex items-center gap-3">
            <MapPin className="h-6 w-6 text-muted-foreground" />
            <h1 className="text-2xl font-medium">Metros</h1>
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

            {/* Inline filter */}
            <InlineFilter
              fieldPrefixes={metroFieldPrefixes}
              entity="metros"
              autocompleteFields={metroAutocompleteFields}
              placeholder="Filter metros..."
              onLiveFilterChange={setLiveFilter}
            />
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('name')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('name')}>
                      Name
                      <SortIcon field="name" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('latitude')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('latitude')}>
                      Latitude
                      <SortIcon field="latitude" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('longitude')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('longitude')}>
                      Longitude
                      <SortIcon field="longitude" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('devices')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('devices')}>
                      Devices
                      <SortIcon field="devices" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('users')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('users')}>
                      Users
                      <SortIcon field="users" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {pagedMetros.map((metro) => (
                  <tr
                    key={metro.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/metros/${metro.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{metro.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm">
                      {metro.name || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {metro.latitude.toFixed(4)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {metro.longitude.toFixed(4)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {metro.device_count > 0 ? metro.device_count : <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {metro.user_count > 0 ? metro.user_count : <span className="text-muted-foreground">—</span>}
                    </td>
                  </tr>
                ))}
                {sortedMetros.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                      No metros found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={sortedMetros.length}
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
