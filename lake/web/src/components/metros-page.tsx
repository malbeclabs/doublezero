import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, MapPin, AlertCircle, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchAllPaginated, fetchMetros } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

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

export function MetrosPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('code')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [searchField, setSearchField] = useState<SortField>('code')
  const [searchText, setSearchText] = useState('')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['metros', 'all'],
    queryFn: () => fetchAllPaginated(fetchMetros, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const metros = response?.items
  const filteredMetros = useMemo(() => {
    if (!metros) return []
    const needle = searchText.trim().toLowerCase()
    if (!needle) return metros
    const numericFilter = parseNumericFilter(searchText)
    if (numericFilter && numericSearchFields.includes(searchField)) {
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
    const getSearchValue = (metro: typeof metros[number]) => {
      switch (searchField) {
        case 'code':
          return metro.code
        case 'name':
          return metro.name || ''
        case 'latitude':
          return String(metro.latitude)
        case 'longitude':
          return String(metro.longitude)
        case 'devices':
          return String(metro.device_count)
        case 'users':
          return String(metro.user_count)
      }
    }
    return metros.filter(metro => getSearchValue(metro).toLowerCase().includes(needle))
  }, [metros, searchField, searchText])
  const sortedMetros = useMemo(() => {
    if (!filteredMetros) return []
    const sorted = [...filteredMetros].sort((a, b) => {
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
          <div className="flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={searchField}
              onChange={(e) => setSearchField(e.target.value as SortField)}
            >
              <option value="code">Code</option>
              <option value="name">Name</option>
              <option value="latitude">Latitude</option>
              <option value="longitude">Longitude</option>
              <option value="devices">Devices</option>
              <option value="users">Users</option>
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
