import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Building2, AlertCircle, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchAllPaginated, fetchContributors } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

type SortField = 'code' | 'name' | 'devices' | 'sideA' | 'sideZ' | 'links'
type SortDirection = 'asc' | 'desc'

export function ContributorsPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)
  const [sortField, setSortField] = useState<SortField>('code')
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['contributors', 'all'],
    queryFn: () => fetchAllPaginated(fetchContributors, PAGE_SIZE),
    refetchInterval: 30000,
  })
  const contributors = response?.items
  const sortedContributors = useMemo(() => {
    if (!contributors) return []
    const sorted = [...contributors].sort((a, b) => {
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
  }, [contributors, sortField, sortDirection])
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
        <div className="flex items-center gap-3 mb-6">
          <Building2 className="h-6 w-6 text-muted-foreground" />
          <h1 className="text-2xl font-medium">Contributors</h1>
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
