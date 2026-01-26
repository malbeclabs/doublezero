import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Loader2, CheckCircle2, AlertTriangle, History, Info } from 'lucide-react'
import { fetchLinkHistory } from '@/lib/api'
import type { LinkHistory } from '@/lib/api'
import { StatusTimeline } from './status-timeline'
import { CriticalityBadge } from './criticality-badge'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'

interface LinkStatusTimelinesProps {
  timeRange?: string
  onTimeRangeChange?: (range: TimeRange) => void
  issueFilters?: string[]
  healthFilters?: string[]
  linksWithIssues?: Map<string, string[]>  // Map of link code -> issue reasons (from filter time range)
  linksWithHealth?: Map<string, string>    // Map of link code -> health status (from filter time range)
  criticalityMap?: Map<string, 'critical' | 'important' | 'redundant'>  // Map of link code -> criticality level
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

function LinkInfoPopover({ link, criticality }: { link: LinkHistory; criticality?: 'critical' | 'important' | 'redundant' }) {
  const [isOpen, setIsOpen] = useState(false)

  const criticalityInfo = {
    critical: {
      label: 'Single Point of Failure',
      description: 'One endpoint has no other connections.',
      className: 'text-red-500',
    },
    important: {
      label: 'Limited Redundancy',
      description: 'Each endpoint has only 2 connections.',
      className: 'text-amber-500',
    },
    redundant: {
      label: 'Well Connected',
      description: 'Both endpoints have 3+ connections.',
      className: 'text-green-500',
    },
  }

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
          className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 min-w-[220px]"
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
            {criticality && (
              <div className="pt-2 mt-2 border-t border-border">
                <div className="text-muted-foreground">Redundancy</div>
                <div className={`font-medium ${criticalityInfo[criticality].className}`}>
                  {criticalityInfo[criticality].label}
                </div>
                <div className="text-muted-foreground mt-1">
                  {criticalityInfo[criticality].description}
                </div>
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

export function LinkStatusTimelines({
  timeRange = '24h',
  onTimeRangeChange,
  issueFilters = ['packet_loss', 'high_latency', 'extended_loss', 'drained', 'interface_errors', 'discards', 'carrier_transitions'],
  healthFilters = ['healthy', 'degraded', 'unhealthy', 'disabled'],
  linksWithIssues,
  linksWithHealth,
  criticalityMap,
}: LinkStatusTimelinesProps) {
  const timeRangeOptions: { value: TimeRange; label: string }[] = [
    { value: '3h', label: '3h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '3d', label: '3d' },
    { value: '7d', label: '7d' },
  ]
  const buckets = useBucketCount()

  const { data, isLoading, error } = useQuery({
    queryKey: ['link-history', timeRange, buckets],
    queryFn: () => fetchLinkHistory(timeRange, buckets),
    refetchInterval: 60_000, // Refresh every minute
    staleTime: 30_000,
  })

  // Helper to check if a link matches health filters
  // Uses linksWithHealth (from filter time range) if provided, otherwise falls back to link's own hours
  const linkMatchesHealthFilters = (link: LinkHistory): boolean => {
    // If we have health data from the filter time range, use it
    if (linksWithHealth) {
      const health = linksWithHealth.get(link.code)
      if (health) {
        // Map no_data to unhealthy for filter matching (no_data is a status, not a filter option)
        const filterHealth = health === 'no_data' ? 'unhealthy' : health
        return healthFilters.includes(filterHealth as any)
      }
      // Link not in filter data - check if it exists in history
      return false
    }

    // Fallback: check link's own hours data
    if (!link.hours || link.hours.length === 0) return false
    return link.hours.some(hour => {
      const status = hour.status
      if (status === 'healthy' && healthFilters.includes('healthy')) return true
      if (status === 'degraded' && healthFilters.includes('degraded')) return true
      if (status === 'unhealthy' && healthFilters.includes('unhealthy')) return true
      if (status === 'disabled' && healthFilters.includes('disabled')) return true
      if (status === 'no_data' && healthFilters.includes('unhealthy')) return true // no_data maps to unhealthy
      return false
    })
  }

  // Check which issue filters are selected
  const issueTypesSelected = issueFilters.filter(f => f !== 'no_issues')
  const noIssuesSelected = issueFilters.includes('no_issues')

  // Filter and sort links by recency of issues
  const filteredLinks = useMemo(() => {
    if (!data?.links) return []

    // Filter by issue reasons (from filter time range if provided) AND health status
    const filtered = data.links.filter(link => {
      // Use linksWithIssues if provided - if link not in map, it has no issues in the filter time range
      // Only fall back to link.issue_reasons if linksWithIssues is not provided at all
      const issueReasons = linksWithIssues
        ? (linksWithIssues.get(link.code) ?? [])
        : (link.issue_reasons ?? [])
      const hasIssues = issueReasons.length > 0

      // Check if link matches issue filters
      let matchesIssue = false
      if (hasIssues) {
        // Link has issues - check if any match the selected issue types
        matchesIssue = issueReasons.some(reason => issueTypesSelected.includes(reason))
      } else {
        // Link has no issues - include if "no_issues" filter is selected
        matchesIssue = noIssuesSelected
      }

      // Must match at least one health filter
      const matchesHealth = linkMatchesHealthFilters(link)

      return matchesIssue && matchesHealth
    })

    // Sort by most recent issue (higher index in hours = more recent)
    // Issues are: unhealthy, degraded, disabled
    return filtered.sort((a, b) => {
      const getLatestIssueIndex = (link: LinkHistory): number => {
        if (!link.hours) return -1
        for (let i = link.hours.length - 1; i >= 0; i--) {
          const status = link.hours[i].status
          if (status === 'unhealthy' || status === 'degraded' || status === 'disabled') {
            return i
          }
        }
        return -1
      }

      const aIndex = getLatestIssueIndex(a)
      const bIndex = getLatestIssueIndex(b)

      // Higher index = more recent = should come first
      return bIndex - aIndex
    })
  }, [data?.links, issueFilters, healthFilters, noIssuesSelected, issueTypesSelected, linksWithIssues, linksWithHealth])

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
            ? 'No links available in the selected time range'
            : 'No links match the selected filters'}
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg">
      <div className="px-4 py-2.5 bg-muted/50 border-b border-border flex items-center gap-2 rounded-t-lg">
        <History className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">
          Link Status History
          <span className="text-sm text-muted-foreground font-normal ml-1">
            ({filteredLinks.length} link{filteredLinks.length !== 1 ? 's' : ''})
          </span>
        </h3>
        {onTimeRangeChange && (
          <div className="inline-flex rounded-lg border border-border bg-background/50 p-0.5 ml-auto">
            {timeRangeOptions.map((opt) => (
              <button
                key={opt.value}
                onClick={() => onTimeRangeChange(opt.value)}
                className={`px-2.5 py-0.5 text-xs rounded-md transition-colors ${
                  timeRange === opt.value
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        )}
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
          <div className="w-2.5 h-2.5 rounded-sm bg-transparent border border-gray-200 dark:border-gray-700" />
          <span>No Data</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="w-2.5 h-2.5 rounded-sm bg-gray-500 dark:bg-gray-700" />
          <span>Disabled</span>
        </div>
      </div>

      <div className="divide-y divide-border">
        {filteredLinks.map((link) => (
          <div key={link.code} className="px-4 py-3 hover:bg-muted/30 transition-colors">
            <div className="flex items-start gap-4">
              {/* Link info */}
              <div className="flex-shrink-0 w-56 sm:w-64 lg:w-72">
                <div className="flex items-center gap-1.5">
                  <Link to={`/dz/links/${link.pk}`} className="font-mono text-sm truncate hover:underline" title={link.code}>
                    {link.code}
                  </Link>
                  <LinkInfoPopover link={link} criticality={criticalityMap?.get(link.code)} />
                  {criticalityMap?.get(link.code) && criticalityMap.get(link.code) !== 'redundant' && (
                    <CriticalityBadge criticality={criticalityMap.get(link.code)!} />
                  )}
                </div>
                <div className="text-xs text-muted-foreground">
                  {link.link_type}{link.contributor && ` · ${link.contributor}`}
                </div>
                {(() => {
                  const issueReasons = linksWithIssues
                    ? (linksWithIssues.get(link.code) ?? [])
                    : (link.issue_reasons ?? [])
                  return issueReasons.length > 0 && (
                    <div className="flex flex-wrap gap-1 mt-1">
                      {issueReasons.includes('packet_loss') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(168, 85, 247, 0.15)', color: '#9333ea' }}
                        >
                          Loss
                        </span>
                      )}
                      {issueReasons.includes('high_latency') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(59, 130, 246, 0.15)', color: '#2563eb' }}
                        >
                          High Latency
                        </span>
                      )}
                      {issueReasons.includes('extended_loss') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(249, 115, 22, 0.15)', color: '#ea580c' }}
                        >
                          Extended Loss
                        </span>
                      )}
                      {issueReasons.includes('drained') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(100, 116, 139, 0.15)', color: '#475569' }}
                        >
                          Drained
                        </span>
                      )}
                      {issueReasons.includes('no_data') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(236, 72, 153, 0.15)', color: '#db2777' }}
                        >
                          No Data
                        </span>
                      )}
                      {issueReasons.includes('interface_errors') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(239, 68, 68, 0.15)', color: '#dc2626' }}
                        >
                          Errors
                        </span>
                      )}
                      {issueReasons.includes('discards') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(245, 158, 11, 0.15)', color: '#d97706' }}
                        >
                          Discards
                        </span>
                      )}
                      {issueReasons.includes('carrier_transitions') && (
                        <span
                          className="text-[10px] px-1.5 py-0.5 rounded font-medium"
                          style={{ backgroundColor: 'rgba(147, 51, 234, 0.15)', color: '#7c3aed' }}
                        >
                          Carrier
                        </span>
                      )}
                    </div>
                  )
                })()}
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
