import { useEffect, useMemo, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Loader2, Building2, AlertCircle, ChevronDown, ChevronUp, Search, X } from 'lucide-react'
import { fetchAllPaginated, fetchContributors } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

type SortField = 'code' | 'name' | 'devices' | 'sideA' | 'sideZ' | 'links'
type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

const numericSearchFields: SortField[] = ['devices', 'sideA', 'sideZ', 'links']

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

// Valid filter fields for contributors
const validFilterFields = ['code', 'name', 'devices', 'sideA', 'sideZ', 'links']

// Parse a filter string into field and value
function parseFilter(filter: string): { field: string; value: string } {
  const colonIndex = filter.indexOf(':')
  if (colonIndex > 0) {
    const field = filter.slice(0, colonIndex).toLowerCase()
    const value = filter.slice(colonIndex + 1)
    // Handle camelCase fields
    const normalizedField = field === 'sidea' ? 'sideA' : field === 'sidez' ? 'sideZ' : field
    if (validFilterFields.includes(normalizedField) && value) {
      return { field: normalizedField, value }
    }
  }
  return { field: 'all', value: filter }
}

export function ContributorsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('code')
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
    queryKey: ['contributors', 'all'],
    queryFn: () => fetchAllPaginated(fetchContributors, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const contributors = response?.items
  const filteredContributors = useMemo(() => {
    if (!contributors) return []
    if (!activeFilterRaw) return contributors

    // Parse filter inside memo to ensure fresh parsing on each recompute
    const filter = parseFilter(activeFilterRaw)
    const searchField = filter.field as SortField | 'all'
    const needle = filter.value.trim().toLowerCase()
    if (!needle) return contributors

    const numericFilter = parseNumericFilter(filter.value)
    if (searchField !== 'all' && numericFilter && numericSearchFields.includes(searchField as SortField)) {
      const getNumericValue = (contributor: typeof contributors[number]) => {
        switch (searchField) {
          case 'devices':
            return contributor.device_count
          case 'sideA':
            return contributor.side_a_devices
          case 'sideZ':
            return contributor.side_z_devices
          case 'links':
            return contributor.link_count
          default:
            return 0
        }
      }
      return contributors.filter(contributor => matchesNumericFilter(getNumericValue(contributor), numericFilter))
    }

    // Text search
    const getSearchValue = (contributor: typeof contributors[number], field: string) => {
      switch (field) {
        case 'code':
          return contributor.code
        case 'name':
          return contributor.name || ''
        default:
          return ''
      }
    }

    if (searchField === 'all') {
      // Search across all text fields
      return contributors.filter(contributor => {
        const textFields = ['code', 'name']
        return textFields.some(field => getSearchValue(contributor, field).toLowerCase().includes(needle))
      })
    }

    return contributors.filter(contributor => getSearchValue(contributor, searchField).toLowerCase().includes(needle))
  }, [contributors, activeFilterRaw])
  const sortedContributors = useMemo(() => {
    if (!filteredContributors) return []
    // Deduplicate by pk to prevent any possible duplicate rows
    const seen = new Set<string>()
    const unique = filteredContributors.filter(c => {
      if (seen.has(c.pk)) return false
      seen.add(c.pk)
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
        case 'devices':
          cmp = a.device_count - b.device_count
          break
        case 'sideA':
          cmp = a.side_a_devices - b.side_a_devices
          break
        case 'sideZ':
          cmp = a.side_z_devices - b.side_z_devices
          break
        case 'links':
          cmp = a.link_count - b.link_count
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredContributors, sortField, sortDirection])
  const pagedContributors = useMemo(
    () => sortedContributors.slice(offset, offset + PAGE_SIZE),
    [sortedContributors, offset]
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
          <div className="text-lg font-medium mb-2">Unable to load contributors</div>
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
            <Building2 className="h-6 w-6 text-muted-foreground" />
            <h1 className="text-2xl font-medium">Contributors</h1>
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('name')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('name')}>
                      Name
                      <SortIcon field="name" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('devices')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('devices')}>
                      Devices
                      <SortIcon field="devices" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('sideA')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('sideA')}>
                      Side A
                      <SortIcon field="sideA" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('sideZ')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('sideZ')}>
                      Side Z
                      <SortIcon field="sideZ" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('links')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('links')}>
                      Links
                      <SortIcon field="links" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {pagedContributors.map((contributor) => (
                  <tr
                    key={contributor.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/contributors/${contributor.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{contributor.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm">
                      {contributor.name || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {contributor.device_count > 0 ? contributor.device_count : <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {contributor.side_a_devices > 0 ? contributor.side_a_devices : '—'}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {contributor.side_z_devices > 0 ? contributor.side_z_devices : '—'}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right">
                      {contributor.link_count > 0 ? contributor.link_count : <span className="text-muted-foreground">—</span>}
                    </td>
                  </tr>
                ))}
                {sortedContributors.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-muted-foreground">
                      No contributors found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={sortedContributors.length}
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
