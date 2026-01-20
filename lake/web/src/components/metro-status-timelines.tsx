import { useState, useMemo } from 'react'
import { useNavigate, useLocation, Link } from 'react-router-dom'
import { Loader2, CheckCircle2, History, Info, AlertTriangle } from 'lucide-react'
import type { LinkHistory, CriticalLinksResponse } from '@/lib/api'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'
type MetroHealthFilter = 'healthy' | 'degraded' | 'unhealthy'
type MetroIssueFilter = 'has_issues' | 'has_spof' | 'no_issues'

interface LinkHistoryResponse {
  links: LinkHistory[]
  bucket_minutes: number
  time_range: string
}

interface MetroStatusTimelinesProps {
  linkHistory: LinkHistoryResponse | undefined
  criticalLinks: CriticalLinksResponse | undefined
  isLoading: boolean
  timeRange?: TimeRange
  onTimeRangeChange?: (range: TimeRange) => void
  healthFilters?: MetroHealthFilter[]
  issueFilters?: MetroIssueFilter[]
  metroNames?: Map<string, string>
}

interface MetroBucket {
  hour: string
  status: 'healthy' | 'degraded' | 'unhealthy' | 'disabled' | 'no_data'
  breakdown: {
    healthy: number
    degraded: number
    unhealthy: number
    disabled: number
    no_data: number
  }
}

interface SpofLink {
  code: string
  pk: string
  status: 'healthy' | 'degraded' | 'unhealthy' | 'disabled' | 'no_data'
}

interface MetroData {
  code: string
  linkCount: number
  healthyLinkCount: number
  spofLinks: SpofLink[]
  hasIssues: boolean
  currentHealth: 'healthy' | 'degraded' | 'unhealthy'
  buckets: MetroBucket[]
}

const statusColors: Record<string, string> = {
  healthy: 'bg-green-500',
  degraded: 'bg-amber-500',
  unhealthy: 'bg-red-500',
  no_data: 'bg-transparent border border-gray-200 dark:border-gray-700',
  disabled: 'bg-gray-500 dark:bg-gray-700',
}

const statusLabels: Record<string, string> = {
  healthy: 'Operational',
  degraded: 'Some Issues',
  unhealthy: 'Significant Issues',
  no_data: 'No Data',
  disabled: 'Disabled',
}

// Determine metro status based on proportion of healthy links
// Excludes no_data (unknown) and disabled (intentionally out of service) from calculation
// - Healthy: >= 80% of active links are working
// - Degraded: 20-80% of active links are working
// - Unhealthy: < 20% of active links are working
function getMetroStatus(breakdown: { healthy: number; degraded: number; unhealthy: number; disabled: number; no_data: number }): 'healthy' | 'degraded' | 'unhealthy' {
  // Only count links that are actively in service (exclude disabled and no_data)
  const activeLinks = breakdown.healthy + breakdown.degraded + breakdown.unhealthy
  if (activeLinks === 0) return 'healthy'

  // Count "working" links (healthy or degraded still pass traffic)
  const working = breakdown.healthy + breakdown.degraded
  const workingPct = (working / activeLinks) * 100

  if (workingPct >= 80) return 'healthy'
  if (workingPct >= 20) return 'degraded'
  return 'unhealthy'
}

function formatTimeRange(isoString: string, bucketMinutes: number = 60): string {
  const start = new Date(isoString)
  const end = new Date(start.getTime() + bucketMinutes * 60 * 1000)
  const startTime = start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const endTime = end.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const date = start.toLocaleDateString([], { month: 'short', day: 'numeric' })
  if (start.getDate() !== end.getDate()) {
    const endDate = end.toLocaleDateString([], { month: 'short', day: 'numeric' })
    return `${date} ${startTime} — ${endDate} ${endTime}`
  }
  return `${date} ${startTime} — ${endTime}`
}

function MetroInfoPopover({ metro }: { metro: MetroData }) {
  const [isOpen, setIsOpen] = useState(false)

  const spofStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'text-green-500'
      case 'degraded': return 'text-amber-500'
      case 'unhealthy': return 'text-red-500'
      case 'disabled': return 'text-gray-500'
      default: return 'text-muted-foreground'
    }
  }

  const spofStatusDot = (status: string) => {
    switch (status) {
      case 'healthy': return 'bg-green-500'
      case 'degraded': return 'bg-amber-500'
      case 'unhealthy': return 'bg-red-500'
      case 'disabled': return 'bg-gray-500'
      default: return 'bg-gray-400'
    }
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
              <div className="text-muted-foreground">Links</div>
              <div className="font-medium">{metro.linkCount} links touching this metro</div>
            </div>
            {metro.spofLinks.length > 0 && (
              <div>
                <div className="text-muted-foreground mb-1">Single Points of Failure</div>
                <div className="space-y-1">
                  {metro.spofLinks.map((spof) => (
                    <div key={spof.code} className="flex items-center gap-2">
                      <div className={`w-2 h-2 rounded-full ${spofStatusDot(spof.status)}`} />
                      <Link
                        to={`/dz/links/${spof.pk}`}
                        className={`font-mono text-[11px] hover:underline ${spofStatusColor(spof.status)}`}
                      >
                        {spof.code}
                      </Link>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <div>
              <div className="text-muted-foreground">Current Status</div>
              <div className={`font-medium ${
                metro.currentHealth === 'healthy' ? 'text-green-500' :
                metro.currentHealth === 'degraded' ? 'text-amber-500' :
                'text-red-500'
              }`}>
                {metro.currentHealth.charAt(0).toUpperCase() + metro.currentHealth.slice(1)}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function MetroTimeline({
  metro,
  bucketMinutes,
}: {
  metro: MetroData
  bucketMinutes: number
}) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)

  return (
    <div className="relative">
      <div className="flex gap-[2px]">
        {metro.buckets.map((bucket, index) => (
          <div
            key={bucket.hour}
            className="relative flex-1 min-w-0"
            onMouseEnter={() => setHoveredIndex(index)}
            onMouseLeave={() => setHoveredIndex(null)}
          >
            <div
              className={`w-full h-6 rounded-sm ${statusColors[bucket.status]} cursor-pointer transition-opacity hover:opacity-80`}
            />

            {/* Tooltip */}
            {hoveredIndex === index && (
              <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-50">
                <div className="bg-popover border border-border rounded-lg shadow-lg p-3 whitespace-nowrap text-sm">
                  <div className="font-medium mb-1">
                    {formatTimeRange(bucket.hour, bucketMinutes)}
                  </div>
                  <div className={`text-xs mb-2 ${
                    bucket.status === 'healthy' ? 'text-green-600 dark:text-green-400' :
                    bucket.status === 'degraded' ? 'text-amber-600 dark:text-amber-400' :
                    bucket.status === 'unhealthy' ? 'text-red-600 dark:text-red-400' :
                    'text-muted-foreground'
                  }`}>
                    {statusLabels[bucket.status]}
                  </div>
                  <div className="space-y-1 text-muted-foreground text-xs">
                    {bucket.breakdown.healthy > 0 && (
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2 h-2 rounded-full bg-green-500" />
                          <span>Healthy</span>
                        </div>
                        <span className="font-medium">{bucket.breakdown.healthy} {bucket.breakdown.healthy === 1 ? 'link' : 'links'}</span>
                      </div>
                    )}
                    {bucket.breakdown.degraded > 0 && (
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2 h-2 rounded-full bg-amber-500" />
                          <span>Degraded</span>
                        </div>
                        <span className="font-medium">{bucket.breakdown.degraded} {bucket.breakdown.degraded === 1 ? 'link' : 'links'}</span>
                      </div>
                    )}
                    {bucket.breakdown.unhealthy > 0 && (
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2 h-2 rounded-full bg-red-500" />
                          <span>Unhealthy</span>
                        </div>
                        <span className="font-medium">{bucket.breakdown.unhealthy} {bucket.breakdown.unhealthy === 1 ? 'link' : 'links'}</span>
                      </div>
                    )}
                    {bucket.breakdown.disabled > 0 && (
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2 h-2 rounded-full bg-gray-500" />
                          <span>Disabled</span>
                        </div>
                        <span className="font-medium">{bucket.breakdown.disabled} {bucket.breakdown.disabled === 1 ? 'link' : 'links'}</span>
                      </div>
                    )}
                    {bucket.breakdown.no_data > 0 && (
                      <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-1.5">
                          <div className="w-2 h-2 rounded-full border border-gray-400" />
                          <span>No Data</span>
                        </div>
                        <span className="font-medium">{bucket.breakdown.no_data} {bucket.breakdown.no_data === 1 ? 'link' : 'links'}</span>
                      </div>
                    )}
                  </div>
                </div>
                <div className="absolute top-full left-1/2 -translate-x-1/2 -mt-[1px]">
                  <div className="border-8 border-transparent border-t-border" />
                  <div className="absolute top-0 left-1/2 -translate-x-1/2 border-[7px] border-transparent border-t-popover" />
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

export function MetroStatusTimelines({
  linkHistory,
  criticalLinks,
  isLoading,
  timeRange = '24h',
  onTimeRangeChange,
  healthFilters = ['healthy', 'degraded', 'unhealthy'],
  issueFilters = ['has_issues', 'has_spof', 'no_issues'],
  metroNames,
}: MetroStatusTimelinesProps) {
  const navigate = useNavigate()
  const location = useLocation()

  const timeRangeOptions: { value: TimeRange; label: string }[] = [
    { value: '3h', label: '3h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '3d', label: '3d' },
    { value: '7d', label: '7d' },
  ]

  const timeLabels: Record<string, string> = {
    '3h': '3h ago',
    '6h': '6h ago',
    '12h': '12h ago',
    '24h': '24h ago',
    '3d': '3d ago',
    '7d': '7d ago',
  }

  // Build critical link set
  const criticalLinkSet = useMemo(() => {
    if (!criticalLinks?.links) return new Set<string>()
    const linkSet = new Set<string>()
    for (const link of criticalLinks.links) {
      if (link.criticality === 'critical') {
        linkSet.add(`${link.sourceCode}--${link.targetCode}`)
        linkSet.add(`${link.targetCode}--${link.sourceCode}`)
      }
    }
    return linkSet
  }, [criticalLinks])

  // Aggregate metro data
  const metroData = useMemo((): MetroData[] => {
    if (!linkHistory?.links || linkHistory.links.length === 0) return []

    const firstLink = linkHistory.links[0]
    if (!firstLink.hours || firstLink.hours.length === 0) return []

    const bucketCount = firstLink.hours.length

    const metroMap = new Map<string, {
      linkCount: number
      healthyLinkCount: number
      spofLinks: SpofLink[]
      buckets: Map<number, { statuses: string[]; hour: string }>
    }>()

    const initMetro = (code: string) => {
      if (!metroMap.has(code)) {
        const buckets = new Map<number, { statuses: string[]; hour: string }>()
        for (let i = 0; i < bucketCount; i++) {
          buckets.set(i, { statuses: [], hour: firstLink.hours[i].hour })
        }
        metroMap.set(code, { linkCount: 0, healthyLinkCount: 0, spofLinks: [], buckets })
      }
    }

    for (const link of linkHistory.links) {
      if (!link.hours) continue

      const deviceKey = `${link.side_a_device}--${link.side_z_device}`
      const isSpof = criticalLinkSet.has(deviceKey)

      // Check current status from last bucket
      // If last bucket is no_data (still collecting), use the previous bucket
      const lastBucket = link.hours[link.hours.length - 1]
      const prevBucket = link.hours.length > 1 ? link.hours[link.hours.length - 2] : null
      const currentStatus = (lastBucket?.status === 'no_data' && prevBucket)
        ? (prevBucket.status || 'healthy')
        : (lastBucket?.status || 'healthy') as SpofLink['status']
      const isCurrentlyHealthy = currentStatus === 'healthy' || currentStatus === 'degraded'

      for (const metroCode of [link.side_a_metro, link.side_z_metro]) {
        if (!metroCode) continue
        initMetro(metroCode)
        const metro = metroMap.get(metroCode)!
        metro.linkCount++
        if (isCurrentlyHealthy) metro.healthyLinkCount++
        if (isSpof) {
          // Only add if not already in the list (link touches both metros)
          if (!metro.spofLinks.some(s => s.code === link.code)) {
            metro.spofLinks.push({
              code: link.code,
              pk: link.pk,
              status: currentStatus,
            })
          }
        }

        for (let i = 0; i < link.hours.length && i < bucketCount; i++) {
          const bucket = metro.buckets.get(i)!
          bucket.statuses.push(link.hours[i].status || 'healthy')
        }
      }
    }

    const metros: MetroData[] = []

    for (const [code, data] of metroMap.entries()) {
      const buckets: MetroBucket[] = []
      let hasAnyIssues = false

      for (let i = 0; i < bucketCount; i++) {
        const bucketData = data.buckets.get(i)!
        const breakdown = { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, no_data: 0 }

        for (const status of bucketData.statuses) {
          if (status in breakdown) {
            breakdown[status as keyof typeof breakdown]++
          }
        }

        // For the last bucket (most recent, still collecting), if ALL links have no_data
        // show it as no_data. Otherwise calculate from the links that do have data.
        const isLastBucket = i === bucketCount - 1
        const totalLinks = breakdown.healthy + breakdown.degraded + breakdown.unhealthy + breakdown.disabled + breakdown.no_data
        const allNoData = breakdown.no_data === totalLinks
        let bucketStatus: 'healthy' | 'degraded' | 'unhealthy' | 'no_data'
        if (isLastBucket && allNoData) {
          bucketStatus = 'no_data'
        } else {
          bucketStatus = getMetroStatus(breakdown)
        }
        if (bucketStatus !== 'healthy' && bucketStatus !== 'no_data') hasAnyIssues = true

        buckets.push({
          hour: bucketData.hour,
          status: bucketStatus,
          breakdown,
        })
      }

      // Current health from last bucket (or previous if last is no_data)
      const lastBucket = buckets[buckets.length - 1]
      const prevBucket = buckets.length > 1 ? buckets[buckets.length - 2] : null
      const healthBucket = (lastBucket.status === 'no_data' && prevBucket) ? prevBucket : lastBucket
      let currentHealth: 'healthy' | 'degraded' | 'unhealthy' = 'healthy'
      if (healthBucket.status === 'unhealthy') {
        currentHealth = 'unhealthy'
      } else if (healthBucket.status === 'degraded' || healthBucket.status === 'disabled') {
        currentHealth = 'degraded'
      }

      // Factor in SPOF status: if any SPOF is unhealthy, metro is unhealthy
      // If any SPOF is degraded, metro is at least degraded
      const hasUnhealthySpof = data.spofLinks.some(s => s.status === 'unhealthy')
      const hasDegradedSpof = data.spofLinks.some(s => s.status === 'degraded')
      if (hasUnhealthySpof) {
        currentHealth = 'unhealthy'
        hasAnyIssues = true
      } else if (hasDegradedSpof && currentHealth === 'healthy') {
        currentHealth = 'degraded'
        hasAnyIssues = true
      }

      metros.push({
        code,
        linkCount: data.linkCount,
        healthyLinkCount: data.healthyLinkCount,
        spofLinks: data.spofLinks,
        hasIssues: hasAnyIssues,
        currentHealth,
        buckets,
      })
    }

    // Sort by recent issues, then SPOF risk
    return metros.sort((a, b) => {
      const recentBuckets = 6
      const getScore = (m: MetroData) => {
        let score = 0
        const start = Math.max(0, m.buckets.length - recentBuckets)
        for (let i = start; i < m.buckets.length; i++) {
          const s = m.buckets[i].status
          if (s === 'unhealthy') score += 3
          else if (s === 'disabled') score += 2
          else if (s === 'degraded') score += 1
        }
        // Add score for SPOF issues
        for (const spof of m.spofLinks) {
          if (spof.status === 'unhealthy') score += 10
          else if (spof.status === 'degraded') score += 5
        }
        return score
      }

      const diff = getScore(b) - getScore(a)
      if (diff !== 0) return diff
      if (a.spofLinks.length !== b.spofLinks.length) return b.spofLinks.length - a.spofLinks.length
      return a.code.localeCompare(b.code)
    })
  }, [linkHistory, criticalLinkSet])

  // Apply filters
  const filteredMetros = useMemo(() => {
    return metroData.filter(metro => {
      // Health filter
      const matchesHealth = healthFilters.includes(metro.currentHealth)

      // Issue filter
      let matchesIssue = false
      if (issueFilters.includes('has_spof') && metro.spofLinks.length > 0) matchesIssue = true
      if (issueFilters.includes('has_issues') && metro.hasIssues) matchesIssue = true
      if (issueFilters.includes('no_issues') && !metro.hasIssues && metro.spofLinks.length === 0) matchesIssue = true

      return matchesHealth && matchesIssue
    })
  }, [metroData, healthFilters, issueFilters])

  const handleMetroClick = (metroCode: string) => {
    const params = new URLSearchParams(location.search)
    params.set('filter', `metro:${metroCode}`)
    navigate(`/status/links?${params.toString()}`)
  }

  if (isLoading) {
    return (
      <div className="border border-border rounded-lg p-6 flex items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground mr-2" />
        <span className="text-sm text-muted-foreground">Loading metro health...</span>
      </div>
    )
  }

  if (filteredMetros.length === 0) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <CheckCircle2 className="h-8 w-8 text-green-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">
          {metroData.length === 0
            ? 'No metros available'
            : 'No metros match the selected filters'}
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg">
      <div className="px-4 py-2.5 bg-muted/50 border-b border-border flex items-center gap-2 rounded-t-lg">
        <History className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">
          Metro Status History
          <span className="text-sm text-muted-foreground font-normal ml-1">
            ({filteredMetros.length} metro{filteredMetros.length !== 1 ? 's' : ''})
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
        {filteredMetros.map((metro) => (
          <div key={metro.code} className="px-4 py-3 hover:bg-muted/30 transition-colors">
            <div className="flex items-start gap-4">
              {/* Metro info */}
              <div className="flex-shrink-0 w-44">
                <div className="flex items-center gap-1.5">
                  <button
                    onClick={() => handleMetroClick(metro.code)}
                    className="text-sm font-medium hover:underline truncate"
                    title={metroNames?.get(metro.code) || metro.code}
                  >
                    {metroNames?.get(metro.code) || metro.code}
                  </button>
                  <MetroInfoPopover metro={metro} />
                </div>
                <div className="text-xs text-muted-foreground">
                  <span className="font-mono">{metro.code}</span>
                  <span className="mx-1">·</span>
                  <span>{metro.linkCount} {metro.linkCount === 1 ? 'link' : 'links'}</span>
                </div>
                {metro.spofLinks.length > 0 && (() => {
                  const hasAtRiskSpof = metro.spofLinks.some(s => s.status === 'unhealthy' || s.status === 'degraded')
                  return (
                    <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium mt-1 inline-flex items-center gap-1 ${
                      hasAtRiskSpof
                        ? 'bg-red-500/20 text-red-600 dark:text-red-400'
                        : 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
                    }`}>
                      {hasAtRiskSpof && <AlertTriangle className="h-3 w-3" />}
                      {metro.spofLinks.length} SPOF
                    </span>
                  )
                })()}
              </div>

              {/* Timeline */}
              <div className="flex-1 min-w-0">
                <MetroTimeline
                  metro={metro}
                  bucketMinutes={linkHistory?.bucket_minutes || 20}
                />
                <div className="flex justify-between mt-1 text-[10px] text-muted-foreground">
                  <span>{timeLabels[timeRange]}</span>
                  <span>Now</span>
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export type { MetroHealthFilter, MetroIssueFilter }
