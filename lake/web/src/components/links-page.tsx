import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Link2, AlertCircle, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchAllPaginated, fetchLinks } from '@/lib/api'
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

function formatLatency(us: number): string {
  if (us === 0) return '—'
  if (us >= 1000) return `${(us / 1000).toFixed(1)} ms`
  return `${us.toFixed(0)} µs`
}

function formatPercent(pct: number): string {
  if (pct === 0) return '—'
  return `${pct.toFixed(1)}%`
}

function getUtilizationColor(pct: number): string {
  if (pct >= 80) return 'text-red-600 dark:text-red-400'
  if (pct >= 60) return 'text-amber-600 dark:text-amber-400'
  if (pct > 0) return 'text-green-600 dark:text-green-400'
  return 'text-muted-foreground'
}

type SortField =
  | 'code'
  | 'type'
  | 'contributor'
  | 'sideA'
  | 'sideZ'
  | 'status'
  | 'bandwidth'
  | 'in'
  | 'out'
  | 'utilIn'
  | 'utilOut'
  | 'latency'
  | 'jitter'
  | 'loss'

type SortDirection = 'asc' | 'desc'

type NumericFilter = {
  op: '>' | '>=' | '<' | '<=' | '='
  value: number
}

type UnitMap = Record<string, number>

const numericSearchFields: SortField[] = [
  'bandwidth',
  'in',
  'out',
  'utilIn',
  'utilOut',
  'latency',
  'jitter',
  'loss',
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

export function LinksPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('code')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')
  const [searchField, setSearchField] = useState<SortField>('code')
  const [searchText, setSearchText] = useState('')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['links', 'all'],
    queryFn: () => fetchAllPaginated(fetchLinks, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const links = response?.items
  const filteredLinks = useMemo(() => {
    if (!links) return []
    const needle = searchText.trim().toLowerCase()
    if (!needle) return links
    const numericFilter = parseNumericFilter(searchText)
    if (numericSearchFields.includes(searchField)) {
      const unitFilter =
        (searchField === 'bandwidth' || searchField === 'in' || searchField === 'out')
          ? parseNumericFilterWithUnits(
              searchText,
              { gbps: 1e9, mbps: 1e6, bps: 1 },
              'gbps'
            )
          : (searchField === 'latency' || searchField === 'jitter')
              ? parseNumericFilterWithUnits(searchText, { ms: 1000, us: 1 }, 'ms')
              : null
      const effectiveFilter = unitFilter ?? numericFilter
      if (!effectiveFilter) {
        return links
      }
      const getNumericValue = (link: typeof links[number]) => {
        switch (searchField) {
          case 'bandwidth':
            return link.bandwidth_bps
          case 'in':
            return link.in_bps
          case 'out':
            return link.out_bps
          case 'utilIn':
            return link.utilization_in
          case 'utilOut':
            return link.utilization_out
          case 'latency':
            return link.latency_us
          case 'jitter':
            return link.jitter_us
          case 'loss':
            return link.loss_percent
          default:
            return 0
        }
      }
      return links.filter(link => matchesNumericFilter(getNumericValue(link), effectiveFilter))
    }
    const getSearchValue = (link: typeof links[number]) => {
      switch (searchField) {
        case 'code':
          return link.code
        case 'type':
          return link.link_type
        case 'contributor':
          return link.contributor_code || ''
        case 'sideA':
          return `${link.side_a_code || ''} ${link.side_a_metro || ''}`.trim()
        case 'sideZ':
          return `${link.side_z_code || ''} ${link.side_z_metro || ''}`.trim()
        case 'status':
          return link.status
        case 'bandwidth':
          return String(link.bandwidth_bps)
        case 'in':
          return String(link.in_bps)
        case 'out':
          return String(link.out_bps)
        case 'utilIn':
          return String(link.utilization_in)
        case 'utilOut':
          return String(link.utilization_out)
        case 'latency':
          return String(link.latency_us)
        case 'jitter':
          return String(link.jitter_us)
        case 'loss':
          return String(link.loss_percent)
      }
    }
    return links.filter(link => getSearchValue(link).toLowerCase().includes(needle))
  }, [links, searchField, searchText])
  const sortedLinks = useMemo(() => {
    if (!filteredLinks) return []

    const sorted = [...filteredLinks].sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'code':
          cmp = a.code.localeCompare(b.code)
          break
        case 'type':
          cmp = a.link_type.localeCompare(b.link_type)
          break
        case 'contributor':
          cmp = (a.contributor_code || '').localeCompare(b.contributor_code || '')
          break
        case 'sideA':
          cmp = (a.side_a_code || '').localeCompare(b.side_a_code || '')
          break
        case 'sideZ':
          cmp = (a.side_z_code || '').localeCompare(b.side_z_code || '')
          break
        case 'status':
          cmp = a.status.localeCompare(b.status)
          break
        case 'bandwidth':
          cmp = a.bandwidth_bps - b.bandwidth_bps
          break
        case 'in':
          cmp = a.in_bps - b.in_bps
          break
        case 'out':
          cmp = a.out_bps - b.out_bps
          break
        case 'utilIn':
          cmp = a.utilization_in - b.utilization_in
          break
        case 'utilOut':
          cmp = a.utilization_out - b.utilization_out
          break
        case 'latency':
          cmp = a.latency_us - b.latency_us
          break
        case 'jitter':
          cmp = a.jitter_us - b.jitter_us
          break
        case 'loss':
          cmp = a.loss_percent - b.loss_percent
          break
      }
      return sortDirection === 'asc' ? cmp : -cmp
    })

    return sorted
  }, [filteredLinks, sortField, sortDirection])
  const pagedLinks = useMemo(
    () => sortedLinks.slice(offset, offset + PAGE_SIZE),
    [sortedLinks, offset]
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
          <div className="text-lg font-medium mb-2">Unable to load links</div>
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
            <Link2 className="h-6 w-6 text-muted-foreground" />
            <h1 className="text-2xl font-medium">Links</h1>
            <span className="text-muted-foreground">({response?.total || 0})</span>
          </div>
          <div className="flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={searchField}
              onChange={(e) => setSearchField(e.target.value as SortField)}
            >
              <option value="code">Code</option>
              <option value="type">Type</option>
              <option value="contributor">Contributor</option>
              <option value="sideA">Side A</option>
              <option value="sideZ">Side Z</option>
              <option value="status">Status</option>
              <option value="bandwidth">Bandwidth</option>
              <option value="in">In</option>
              <option value="out">Out</option>
              <option value="utilIn">Util In</option>
              <option value="utilOut">Util Out</option>
              <option value="latency">Latency</option>
              <option value="jitter">Jitter</option>
              <option value="loss">Loss</option>
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
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('sideA')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('sideA')}>
                      Side A
                      <SortIcon field="sideA" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('sideZ')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('sideZ')}>
                      Side Z
                      <SortIcon field="sideZ" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium" aria-sort={sortAria('status')}>
                    <button className="inline-flex items-center gap-1" type="button" onClick={() => handleSort('status')}>
                      Status
                      <SortIcon field="status" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('bandwidth')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('bandwidth')}>
                      Bandwidth
                      <SortIcon field="bandwidth" />
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
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('utilIn')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('utilIn')}>
                      Util In
                      <SortIcon field="utilIn" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('utilOut')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('utilOut')}>
                      Util Out
                      <SortIcon field="utilOut" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('latency')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('latency')}>
                      Latency
                      <SortIcon field="latency" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('jitter')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('jitter')}>
                      Jitter
                      <SortIcon field="jitter" />
                    </button>
                  </th>
                  <th className="px-4 py-3 font-medium text-right" aria-sort={sortAria('loss')}>
                    <button className="inline-flex items-center gap-1 justify-end w-full" type="button" onClick={() => handleSort('loss')}>
                      Loss
                      <SortIcon field="loss" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {pagedLinks.map((link) => (
                  <tr
                    key={link.pk}
                    className="border-b border-border last:border-b-0 hover:bg-muted/50 cursor-pointer transition-colors"
                    onClick={(e) => handleRowClick(e, `/dz/links/${link.pk}`, navigate)}
                  >
                    <td className="px-4 py-3">
                      <span className="font-mono text-sm">{link.code}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {link.link_type}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      {link.contributor_code || '—'}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <span className="font-mono">{link.side_a_code || '—'}</span>
                      {link.side_a_metro && (
                        <span className="ml-1 text-xs">({link.side_a_metro})</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-muted-foreground">
                      <span className="font-mono">{link.side_z_code || '—'}</span>
                      {link.side_z_metro && (
                        <span className="ml-1 text-xs">({link.side_z_metro})</span>
                      )}
                    </td>
                    <td className={`px-4 py-3 text-sm capitalize ${statusColors[link.status] || ''}`}>
                      {link.status}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.bandwidth_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.in_bps)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatBps(link.out_bps)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${getUtilizationColor(link.utilization_in)}`}>
                      {formatPercent(link.utilization_in)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${getUtilizationColor(link.utilization_out)}`}>
                      {formatPercent(link.utilization_out)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatLatency(link.latency_us)}
                    </td>
                    <td className="px-4 py-3 text-sm tabular-nums text-right text-muted-foreground">
                      {formatLatency(link.jitter_us)}
                    </td>
                    <td className={`px-4 py-3 text-sm tabular-nums text-right ${link.loss_percent > 0 ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground'}`}>
                      {formatPercent(link.loss_percent)}
                    </td>
                  </tr>
                ))}
                {sortedLinks.length === 0 && (
                  <tr>
                    <td colSpan={14} className="px-4 py-8 text-center text-muted-foreground">
                      No links found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {response && (
            <Pagination
              total={sortedLinks.length}
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
