import { useMemo, useState } from 'react'
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

export function LinksPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('code')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['links', 'all'],
    queryFn: () => fetchAllPaginated(fetchLinks, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const links = response?.items
  const sortedLinks = useMemo(() => {
    if (!links) return []
    if (!sortField) return links

    const sorted = [...links].sort((a, b) => {
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
  }, [links, sortField, sortDirection])
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
        <div className="flex items-center gap-3 mb-6">
          <Link2 className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Links</h1>
          <span className="text-muted-foreground">({response?.total || 0})</span>
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
