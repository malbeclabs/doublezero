import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Loader2, Building2, AlertCircle } from 'lucide-react'
import { fetchContributors } from '@/lib/api'
import { handleRowClick } from '@/lib/utils'
import { Pagination } from './pagination'

const PAGE_SIZE = 100

export function ContributorsPage() {
  const navigate = useNavigate()
  const [offset, setOffset] = useState(0)

  const { data: response, isLoading, error } = useQuery({
    queryKey: ['contributors', offset],
    queryFn: () => fetchContributors(PAGE_SIZE, offset),
    refetchInterval: 30000,
  })
  const contributors = response?.items

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
                  <th className="px-4 py-3 font-medium">Code</th>
                  <th className="px-4 py-3 font-medium">Name</th>
                  <th className="px-4 py-3 font-medium text-right">Devices</th>
                  <th className="px-4 py-3 font-medium text-right">Side A</th>
                  <th className="px-4 py-3 font-medium text-right">Side Z</th>
                  <th className="px-4 py-3 font-medium text-right">Links</th>
                </tr>
              </thead>
              <tbody>
                {contributors?.map((contributor) => (
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
                {(!contributors || contributors.length === 0) && (
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
              limit={response.limit}
              offset={response.offset}
              onOffsetChange={setOffset}
            />
          )}
        </div>
      </div>
    </div>
  )
}
