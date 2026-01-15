import { useQuery } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { Loader2, CheckCircle2, AlertTriangle, Info } from 'lucide-react'
import { fetchLinkHistory } from '@/lib/api'
import type { LinkHistory } from '@/lib/api'
import { StatusTimeline } from './status-timeline'

interface LinkStatusTimelinesProps {
  timeRange?: string
  issueFilters?: string[]
}

function formatBandwidth(bps: number): string {
  if (bps >= 1_000_000_000) {
    return `${(bps / 1_000_000_000).toFixed(0)} Gbps`
  } else if (bps >= 1_000_000) {
    return `${(bps / 1_000_000).toFixed(0)} Mbps`
  } else if (bps >= 1_000) {
    return `${(bps / 1_000).toFixed(0)} Kbps`
  }
  return `${bps} bps`
}

function LinkInfoPopover({ link }: { link: LinkHistory }) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        className="text-muted-foreground hover:text-foreground transition-colors p-0.5 -m-0.5"
        onMouseEnter={() => setIsOpen(true)}
        onMouseLeave={() => setIsOpen(false)}
        onClick={() => setIsOpen(!isOpen)}
      >
        <Info className="h-3.5 w-3.5" />
      </button>
      {isOpen && (
        <div
          className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 min-w-[200px]"
          onMouseEnter={() => setIsOpen(true)}
          onMouseLeave={() => setIsOpen(false)}
        >
          <div className="space-y-2 text-xs">
            <div>
              <div className="text-muted-foreground">Route</div>
              <div className="font-medium">{link.side_a_metro} — {link.side_z_metro}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Devices</div>
              <div className="font-mono text-[11px]">
                <div>{link.side_a_device}</div>
                <div>{link.side_z_device}</div>
              </div>
            </div>
            <div className="flex gap-4">
              <div>
                <div className="text-muted-foreground">Type</div>
                <div className="font-medium">{link.link_type}</div>
              </div>
              {link.bandwidth_bps > 0 && (
                <div>
                  <div className="text-muted-foreground">Bandwidth</div>
                  <div className="font-medium">{formatBandwidth(link.bandwidth_bps)}</div>
                </div>
              )}
            </div>
            {link.committed_rtt_us > 0 && (
              <div>
                <div className="text-muted-foreground">Committed RTT</div>
                <div className="font-medium">{(link.committed_rtt_us / 1000).toFixed(2)} ms</div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function useBucketCount() {
  const [buckets, setBuckets] = useState(72)

  useEffect(() => {
    const updateBuckets = () => {
      const width = window.innerWidth
      if (width < 640) {
        setBuckets(24) // mobile
      } else if (width < 1024) {
        setBuckets(48) // tablet
      } else {
        setBuckets(72) // desktop
      }
    }

    updateBuckets()
    window.addEventListener('resize', updateBuckets)
    return () => window.removeEventListener('resize', updateBuckets)
  }, [])

  return buckets
}

export function LinkStatusTimelines({ timeRange = '24h', issueFilters = ['packet_loss', 'high_latency', 'disabled'] }: LinkStatusTimelinesProps) {
  const buckets = useBucketCount()

  const { data, isLoading, error } = useQuery({
    queryKey: ['link-history', timeRange, buckets],
    queryFn: () => fetchLinkHistory(timeRange, buckets),
    refetchInterval: 60_000, // Refresh every minute
    staleTime: 30_000,
  })

  // Filter links based on issue filters
  const filteredLinks = data?.links.filter(link => {
    if (!link.issue_reasons || link.issue_reasons.length === 0) return false
    return link.issue_reasons.some(reason => issueFilters.includes(reason))
  }) || []

  if (isLoading) {
    return (
      <div className="border border-border rounded-lg p-6 flex items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground mr-2" />
        <span className="text-sm text-muted-foreground">Loading link history...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <AlertTriangle className="h-8 w-8 text-amber-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">Unable to load link history</div>
      </div>
    )
  }

  if (filteredLinks.length === 0) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <CheckCircle2 className="h-8 w-8 text-green-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">
          {data?.links.length === 0
            ? 'All links healthy in the selected time range'
            : 'No links match the selected issue filters'}
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2 rounded-t-lg">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Link Status (Last {
          data?.time_range === '1h' ? 'Hour' :
          data?.time_range === '6h' ? '6 Hours' :
          data?.time_range === '12h' ? '12 Hours' :
          data?.time_range === '24h' ? '24 Hours' :
          data?.time_range === '3d' ? '3 Days' : '7 Days'
        })</h3>
        <span className="text-sm text-muted-foreground ml-auto">
          {filteredLinks.length} link{filteredLinks.length !== 1 ? 's' : ''} with issues
        </span>
      </div>

      {/* Legend */}
      <div className="px-4 py-2 border-b border-border bg-muted/30 flex items-center gap-4 text-xs text-muted-foreground">
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-green-500" />
          <span>Healthy</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-amber-500" />
          <span>Degraded</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-red-500" />
          <span>Unhealthy</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-gray-300 dark:bg-gray-600" />
          <span>No Data</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-blue-400 dark:bg-blue-500" />
          <span>Disabled</span>
        </div>
      </div>

      <div className="divide-y divide-border">
        {filteredLinks.map((link) => (
          <div key={link.code} className="px-4 py-3 hover:bg-muted/30 transition-colors">
            <div className="flex items-start gap-4">
              {/* Link info */}
              <div className="flex-shrink-0 w-48">
                <div className="flex items-center gap-1.5">
                  <div className="font-mono text-sm truncate" title={link.code}>
                    {link.code}
                  </div>
                  <LinkInfoPopover link={link} />
                </div>
                {link.contributor && (
                  <div className="text-xs text-muted-foreground">{link.contributor}</div>
                )}
                {link.issue_reasons && link.issue_reasons.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-1">
                    {link.issue_reasons.includes('packet_loss') && (
                      <span
                        className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                        style={{ backgroundColor: 'rgba(239, 68, 68, 0.15)', color: '#dc2626' }}
                      >
                        Loss
                      </span>
                    )}
                    {link.issue_reasons.includes('high_latency') && (
                      <span
                        className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                        style={{ backgroundColor: 'rgba(245, 158, 11, 0.15)', color: '#d97706' }}
                      >
                        Latency
                      </span>
                    )}
                    {link.issue_reasons.includes('disabled') && (
                      <span
                        className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                        style={{ backgroundColor: 'rgba(59, 130, 246, 0.15)', color: '#2563eb' }}
                      >
                        Disabled
                      </span>
                    )}
                  </div>
                )}
              </div>

              {/* Timeline */}
              <div className="flex-1 min-w-0">
                <StatusTimeline
                  hours={link.hours}
                  committedRttUs={link.committed_rtt_us}
                  bucketMinutes={data?.bucket_minutes}
                  timeRange={data?.time_range}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
