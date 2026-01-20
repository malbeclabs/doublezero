import { Building2 } from 'lucide-react'

interface ContributorInfo {
  code: string
  name: string
}

interface ContributorsOverlayPanelProps {
  contributorInfoMap: Map<string, ContributorInfo>
  getContributorColor: (pk: string) => string
  getDeviceCountForContributor: (pk: string) => number
  getLinkCountForContributor: (pk: string) => number
  totalDeviceCount: number
  totalLinkCount: number
  isLoading?: boolean
}

export function ContributorsOverlayPanel({
  contributorInfoMap,
  getContributorColor,
  getDeviceCountForContributor,
  getLinkCountForContributor,
  totalDeviceCount,
  totalLinkCount,
  isLoading,
}: ContributorsOverlayPanelProps) {
  // Sort contributors by device count descending
  const sortedContributors = Array.from(contributorInfoMap.entries())
    .map(([pk, info]) => ({
      pk,
      ...info,
      deviceCount: getDeviceCountForContributor(pk),
      linkCount: getLinkCountForContributor(pk),
    }))
    .filter(c => c.deviceCount > 0 || c.linkCount > 0)
    .sort((a, b) => b.deviceCount - a.deviceCount)

  return (
    <div className="p-3 text-xs">
      <div className="flex items-center gap-1.5 mb-2">
        <Building2 className="h-3.5 w-3.5 text-purple-500" />
        <span className="font-medium">Contributors</span>
      </div>

      {isLoading && (
        <div className="text-muted-foreground">Loading contributor data...</div>
      )}

      {!isLoading && contributorInfoMap.size > 0 && (
        <div className="space-y-3">
          {/* Summary stats */}
          <div className="space-y-1.5">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Contributors</span>
              <span className="font-medium">{sortedContributors.length}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Devices</span>
              <span className="font-medium">{totalDeviceCount}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Links</span>
              <span className="font-medium">{totalLinkCount}</span>
            </div>
          </div>

          {/* Contributor list with colors */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">By device count</div>
            <div className="space-y-0.5">
              {sortedContributors.map((contributor) => (
                <div
                  key={contributor.pk}
                  className="flex items-center justify-between gap-2 px-1.5 py-1 rounded hover:bg-[var(--muted)] transition-colors"
                >
                  <div className="flex items-center gap-1.5 min-w-0">
                    <div
                      className="w-3 h-3 rounded-full flex-shrink-0"
                      style={{ backgroundColor: getContributorColor(contributor.pk) }}
                    />
                    <span className="truncate" title={contributor.name || contributor.code}>
                      {contributor.code}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-muted-foreground flex-shrink-0">
                    <span title="Devices">{contributor.deviceCount}d</span>
                    <span title="Links">{contributor.linkCount}l</span>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Legend */}
          <div className="pt-2 border-t border-[var(--border)]">
            <div className="text-muted-foreground mb-1.5">Coloring</div>
            <div className="space-y-1 text-muted-foreground">
              <div className="flex items-center gap-1.5">
                <div className="w-3 h-3 rounded-full bg-purple-500" />
                <span>Devices colored by contributor</span>
              </div>
              <div className="flex items-center gap-1.5">
                <div className="w-4 h-0.5 rounded bg-purple-500" />
                <span>Links colored by contributor</span>
              </div>
            </div>
          </div>

        </div>
      )}

      {!isLoading && contributorInfoMap.size === 0 && (
        <div className="text-muted-foreground">No contributor data available</div>
      )}
    </div>
  )
}
