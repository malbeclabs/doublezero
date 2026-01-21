import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { CheckCircle2, AlertTriangle, XCircle, ArrowUpDown, Cpu, ChevronDown, ChevronUp } from 'lucide-react'
import { fetchStatus, fetchLinkHistory, fetchDeviceHistory, fetchInterfaceIssues, fetchCriticalLinks, fetchMetros, type StatusResponse, type InterfaceIssue, type NonActivatedLink, type LinkHistory, type DeviceHistory, type LinkMetric, type DeviceUtilization, type CriticalLinksResponse, type LinkIssue } from '@/lib/api'
import { StatusFilters, useStatusFilters, type StatusFilter } from '@/components/status-search-bar'
import { StatCard } from '@/components/stat-card'
import { LinkStatusTimelines } from '@/components/link-status-timelines'
import { DeviceStatusTimelines } from '@/components/device-status-timelines'
import { MetroStatusTimelines, type MetroHealthFilter, type MetroIssueFilter } from '@/components/metro-status-timelines'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'
type IssueFilter = 'packet_loss' | 'high_latency' | 'extended_loss' | 'drained' | 'no_data' | 'no_issues'
type DeviceIssueFilter = 'interface_errors' | 'discards' | 'carrier_transitions' | 'drained' | 'no_issues'
type HealthFilter = 'healthy' | 'degraded' | 'unhealthy' | 'disabled'

const timeRangeLabels: Record<TimeRange, string> = {
  '3h': 'Last 3 hours',
  '6h': 'Last 6 hours',
  '12h': 'Last 12 hours',
  '24h': 'Last 24 hours',
  '3d': 'Last 3 days',
  '7d': 'Last 7 days',
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

function StatusPageSkeleton() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        <div className="mb-8">
          <Skeleton className="h-[72px] rounded-lg" />
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-x-8 gap-y-6 mb-8">
          {Array.from({ length: 10 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-24" />
          ))}
        </div>
        <div className="mb-6">
          <Skeleton className="h-10 w-48" />
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          <Skeleton className="h-[200px] rounded-lg" />
          <Skeleton className="h-[200px] rounded-lg" />
        </div>
        <Skeleton className="h-[300px] rounded-lg" />
      </div>
    </div>
  )
}

interface IssueCounts {
  packet_loss: number
  high_latency: number
  extended_loss: number
  drained: number
  no_data: number
  no_issues: number
  total: number
}

function formatTimeAgo(isoString: string): string {
  const date = new Date(isoString)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSecs = Math.floor(diffMs / 1000)

  if (diffSecs < 60) return `${diffSecs}s ago`
  if (diffSecs < 3600) return `${Math.floor(diffSecs / 60)}m ago`
  if (diffSecs < 86400) return `${Math.floor(diffSecs / 3600)}h ago`
  return `${Math.floor(diffSecs / 86400)}d ago`
}

function getStatusReasons(status: StatusResponse): string[] {
  const reasons: string[] = []

  if (status.links.disabled > 0) {
    reasons.push(`${status.links.disabled} link${status.links.disabled > 1 ? 's' : ''} down`)
  }
  if (status.links.unhealthy > 0) {
    reasons.push(`${status.links.unhealthy} link${status.links.unhealthy > 1 ? 's' : ''} with critical issues`)
  }
  if (status.links.degraded > 0) {
    reasons.push(`${status.links.degraded} link${status.links.degraded > 1 ? 's' : ''} with degraded performance`)
  }

  if (status.performance.avg_loss_percent >= 1.0) {
    reasons.push(`${status.performance.avg_loss_percent.toFixed(1)}% average packet loss`)
  } else if (status.performance.avg_loss_percent >= 0.1) {
    reasons.push(`${status.performance.avg_loss_percent.toFixed(2)}% packet loss detected`)
  }

  const nonActivatedDevices = Object.entries(status.network.devices_by_status)
    .filter(([s]) => s !== 'activated')
    .reduce((sum, [, count]) => sum + count, 0)
  const nonActivatedLinks = Object.entries(status.network.links_by_status)
    .filter(([s]) => s !== 'activated')
    .reduce((sum, [, count]) => sum + count, 0)

  if (nonActivatedDevices > 0) {
    reasons.push(`${nonActivatedDevices} device${nonActivatedDevices > 1 ? 's' : ''} not activated`)
  }
  if (nonActivatedLinks > 0) {
    reasons.push(`${nonActivatedLinks} link${nonActivatedLinks > 1 ? 's' : ''} not activated`)
  }

  return reasons
}

function formatRelativeTime(timestamp: string): string {
  const now = Date.now()
  const then = new Date(timestamp).getTime()
  const seconds = Math.floor((now - then) / 1000)

  if (seconds < 10) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ago`
}

type IssueSeverity = 'down' | 'critical' | 'degraded'

function classifyIssueSeverity(issue: LinkIssue): IssueSeverity {
  if (issue.issue === 'packet_loss') {
    if (issue.value >= 95) return 'down'
    if (issue.value >= 10) return 'critical'
    return 'degraded'
  } else {
    // high_latency
    if (issue.value >= 50) return 'critical'
    return 'degraded'
  }
}

function formatDuration(since: string): string {
  if (!since) return ''
  const start = new Date(since).getTime()
  const now = Date.now()
  const diffMs = Math.max(0, now - start)
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMins / 60)
  const diffDays = Math.floor(diffHours / 24)

  if (diffDays > 0) {
    const remainingHours = diffHours % 24
    return remainingHours > 0 ? `${diffDays}d ${remainingHours}h` : `${diffDays}d`
  }
  if (diffHours > 0) {
    const remainingMins = diffMins % 60
    return remainingMins > 0 ? `${diffHours}h ${remainingMins}m` : `${diffHours}h`
  }
  return `${diffMins}m`
}

function IssueDetails({ issues, onIssueClick }: { issues: LinkIssue[]; onIssueClick: () => void }) {
  const grouped = issues.reduce(
    (acc, issue) => {
      const severity = classifyIssueSeverity(issue)
      acc[severity].push(issue)
      return acc
    },
    { down: [] as LinkIssue[], critical: [] as LinkIssue[], degraded: [] as LinkIssue[] }
  )

  const sections: { key: IssueSeverity; label: string; icon: typeof XCircle; iconColor: string; valueColor: string }[] = [
    { key: 'down', label: 'Links Down', icon: XCircle, iconColor: 'text-gray-500', valueColor: 'text-gray-600 dark:text-gray-400' },
    { key: 'critical', label: 'Critical Issues', icon: XCircle, iconColor: 'text-red-500', valueColor: 'text-red-500' },
    { key: 'degraded', label: 'Degraded Performance', icon: AlertTriangle, iconColor: 'text-orange-500', valueColor: 'text-orange-500' },
  ]

  return (
    <div className="border-t border-border px-6 py-4 space-y-4">
      {sections.map(({ key, label, icon: Icon, iconColor, valueColor }) => {
        const sectionIssues = grouped[key]
        if (sectionIssues.length === 0) return null
        return (
          <div key={key}>
            <div className="text-sm font-medium text-muted-foreground mb-2">{label}</div>
            <div className="space-y-2">
              {sectionIssues.map((issue, idx) => (
                <button
                  key={idx}
                  onClick={onIssueClick}
                  className="flex items-center justify-between w-full py-2 px-3 rounded-md bg-muted/50 hover:bg-muted transition-colors text-left"
                >
                  <div className="flex items-center gap-3">
                    <Icon className={`h-4 w-4 ${iconColor}`} />
                    <div>
                      <div className="font-medium text-sm">{issue.code}</div>
                      <div className="text-xs text-muted-foreground">
                        {issue.side_a_metro} → {issue.side_z_metro} · {issue.link_type}{issue.contributor && ` · ${issue.contributor}`}
                      </div>
                    </div>
                  </div>
                  <div className="text-right">
                    <div className={`text-sm font-medium ${valueColor}`}>
                      {issue.issue === 'packet_loss'
                        ? `${issue.value.toFixed(1)}% loss`
                        : `${issue.value.toFixed(0)}% over SLA`}
                    </div>
                    {issue.since && (
                      <div
                        className="text-xs text-muted-foreground"
                        title={new Date(issue.since).toLocaleString()}
                      >
                        for {formatDuration(issue.since)}
                      </div>
                    )}
                  </div>
                </button>
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function StatusIndicator({ statusData }: { statusData: StatusResponse }) {
  const status = statusData.status
  const reasons = getStatusReasons(statusData)
  const [, forceUpdate] = useState(0)
  const [expanded, setExpanded] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()

  useEffect(() => {
    const interval = setInterval(() => forceUpdate(n => n + 1), 10000)
    return () => clearInterval(interval)
  }, [])

  const scrollToLinkHistory = () => {
    // If not on links tab, navigate there first
    if (!location.pathname.includes('/status/links')) {
      navigate('/status/links')
      // Wait for navigation then scroll
      setTimeout(() => {
        document.getElementById('link-status-history')?.scrollIntoView({ behavior: 'smooth' })
      }, 100)
    } else {
      document.getElementById('link-status-history')?.scrollIntoView({ behavior: 'smooth' })
    }
  }

  const config = {
    healthy: {
      icon: CheckCircle2,
      label: 'All Systems Operational',
      className: 'text-green-700 dark:text-green-400',
      borderClassName: 'border-l-green-500',
    },
    degraded: {
      icon: AlertTriangle,
      label: 'Some Issues Detected',
      className: 'text-orange-600 dark:text-orange-400',
      borderClassName: 'border-l-orange-500',
    },
    unhealthy: {
      icon: XCircle,
      label: 'System Issues Detected',
      className: 'text-red-700 dark:text-red-400',
      borderClassName: 'border-l-red-500',
    },
  }

  const { icon: Icon, label, className, borderClassName } = config[status]

  // Check if there are link issues to show
  const linkIssues = statusData.links.issues || []
  const hasExpandableContent = linkIssues.length > 0

  return (
    <div className={`rounded-lg bg-card border border-border border-l-4 ${borderClassName}`}>
      <div
        className={`flex items-center gap-3 px-6 py-4 ${hasExpandableContent ? 'cursor-pointer hover:bg-muted/30 transition-colors' : ''}`}
        onClick={hasExpandableContent ? () => setExpanded(!expanded) : undefined}
      >
        <Icon className={`h-8 w-8 ${className}`} />
        <div className="flex-1">
          <div className={`text-lg font-medium ${className}`}>{label}</div>
          {reasons.length > 0 && (
            <div className="text-sm text-muted-foreground">{reasons.slice(0, 2).join(' · ')}</div>
          )}
        </div>
        <div className="flex items-center gap-3">
          <div className="text-xs text-muted-foreground/60">
            Updated {formatRelativeTime(statusData.timestamp)}
          </div>
          {hasExpandableContent && (
            <div className="text-muted-foreground">
              {expanded ? <ChevronUp className="h-5 w-5" /> : <ChevronDown className="h-5 w-5" />}
            </div>
          )}
        </div>
      </div>
      {expanded && linkIssues.length > 0 && (
        <IssueDetails issues={linkIssues} onIssueClick={scrollToLinkHistory} />
      )}
    </div>
  )
}

function TabNavigation({ activeTab }: { activeTab: 'links' | 'devices' | 'metros' }) {
  const navigate = useNavigate()
  const location = useLocation()

  // Preserve query params when switching tabs
  const navigateWithParams = (path: string) => {
    navigate(path + location.search)
  }

  return (
    <div className="flex items-center justify-between border-b border-border mb-6">
      <div className="flex gap-1">
        <button
          onClick={() => navigateWithParams('/status/links')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeTab === 'links'
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          Links
        </button>
        <button
          onClick={() => navigateWithParams('/status/devices')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeTab === 'devices'
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          Devices
        </button>
        <button
          onClick={() => navigateWithParams('/status/metros')}
          className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
            activeTab === 'metros'
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          Metros
        </button>
      </div>
      <StatusFilters className="pb-2" />
    </div>
  )
}

interface HealthIssueBreakdown {
  packet_loss: number
  high_latency: number
  extended_loss: number
  drained: number
  no_data: number
}

interface IssueHealthBreakdown {
  healthy: number
  degraded: number
  unhealthy: number
  disabled: number
}

function HealthFilterItem({
  color,
  label,
  count,
  description,
  selected,
  onClick,
  issueBreakdown,
  healthBreakdown,
}: {
  color: string
  label: string
  count: number
  description: string
  selected: boolean
  onClick: () => void
  issueBreakdown?: HealthIssueBreakdown
  healthBreakdown?: IssueHealthBreakdown
}) {
  const [showTooltip, setShowTooltip] = useState(false)

  const issueLabels: { key: keyof HealthIssueBreakdown; label: string; color: string }[] = [
    { key: 'packet_loss', label: 'Packet Loss', color: 'bg-purple-500' },
    { key: 'high_latency', label: 'High Latency', color: 'bg-blue-500' },
    { key: 'extended_loss', label: 'Extended Loss', color: 'bg-orange-500' },
    { key: 'drained', label: 'Drained', color: 'bg-slate-500' },
    { key: 'no_data', label: 'No Data', color: 'bg-pink-500' },
  ]

  const healthLabels: { key: keyof IssueHealthBreakdown; label: string; color: string }[] = [
    { key: 'healthy', label: 'Healthy', color: 'bg-green-500' },
    { key: 'degraded', label: 'Degraded', color: 'bg-amber-500' },
    { key: 'unhealthy', label: 'Unhealthy', color: 'bg-red-500' },
    { key: 'disabled', label: 'Disabled', color: 'bg-gray-500' },
  ]

  const hasIssues = issueBreakdown && Object.values(issueBreakdown).some(v => v > 0)
  const hasHealth = healthBreakdown && Object.values(healthBreakdown).some(v => v > 0)

  return (
    <button
      onClick={onClick}
      className="flex items-center justify-between relative w-full text-left rounded px-1.5 py-0.5 -mx-1.5 transition-colors hover:bg-muted/50"
    >
      <div
        className="flex items-center gap-1.5"
        onMouseEnter={() => setShowTooltip(true)}
        onMouseLeave={() => setShowTooltip(false)}
      >
        <div className={`h-2.5 w-2.5 rounded-full ${color} transition-opacity ${!selected ? 'opacity-25' : ''}`} />
        <span className={`transition-colors ${selected ? 'text-foreground' : 'text-muted-foreground/50'}`}>{label}</span>
        {showTooltip && (
          <div className="absolute left-0 bottom-full mb-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-2 text-xs w-52">
            <div className="mb-1">{description}</div>
            {hasIssues && (
              <div className="mt-2 pt-2 border-t border-border space-y-1">
                {issueLabels.map(({ key, label, color }) => {
                  const issueCount = issueBreakdown[key]
                  if (issueCount === 0) return null
                  return (
                    <div key={key} className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5">
                        <div className={`h-2 w-2 rounded-full ${color}`} />
                        <span className="text-muted-foreground">{label}</span>
                      </div>
                      <span className="font-medium tabular-nums">{issueCount}</span>
                    </div>
                  )
                })}
              </div>
            )}
            {hasHealth && (
              <div className="mt-2 pt-2 border-t border-border space-y-1">
                {healthLabels.map(({ key, label, color }) => {
                  const healthCount = healthBreakdown[key]
                  if (healthCount === 0) return null
                  return (
                    <div key={key} className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5">
                        <div className={`h-2 w-2 rounded-full ${color}`} />
                        <span className="text-muted-foreground">{label}</span>
                      </div>
                      <span className="font-medium tabular-nums">{healthCount}</span>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </div>
      <span className={`font-medium tabular-nums transition-colors ${!selected ? 'text-muted-foreground/50' : ''}`}>{count}</span>
    </button>
  )
}

interface IssuesByHealth {
  healthy: HealthIssueBreakdown
  degraded: HealthIssueBreakdown
  unhealthy: HealthIssueBreakdown
  disabled: HealthIssueBreakdown
}

interface HealthByIssue {
  packet_loss: IssueHealthBreakdown
  high_latency: IssueHealthBreakdown
  extended_loss: IssueHealthBreakdown
  drained: IssueHealthBreakdown
  no_data: IssueHealthBreakdown
  no_issues: IssueHealthBreakdown
}

function LinkHealthFilterCard({
  links,
  selected,
  onChange,
  issuesByHealth,
  timeRange,
}: {
  links: { healthy: number; degraded: number; unhealthy: number; disabled: number; total: number }
  selected: HealthFilter[]
  onChange: (filters: HealthFilter[]) => void
  issuesByHealth?: IssuesByHealth
  timeRange: TimeRange
}) {
  const toggleFilter = (filter: HealthFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === 4

  const healthyPct = links.total > 0 ? (links.healthy / links.total) * 100 : 100
  const degradedPct = links.total > 0 ? (links.degraded / links.total) * 100 : 0
  const unhealthyPct = links.total > 0 ? (links.unhealthy / links.total) * 100 : 0
  const disabledPct = links.total > 0 ? ((links.disabled || 0) / links.total) * 100 : 0

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Link Health</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(['healthy', 'degraded', 'unhealthy', 'disabled'])}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {healthyPct > 0 && (
          <div
            className={`bg-green-500 h-full transition-all ${!selected.includes('healthy') ? 'opacity-30' : ''}`}
            style={{ width: `${healthyPct}%` }}
          />
        )}
        {degradedPct > 0 && (
          <div
            className={`bg-amber-500 h-full transition-all ${!selected.includes('degraded') ? 'opacity-30' : ''}`}
            style={{ width: `${degradedPct}%` }}
          />
        )}
        {unhealthyPct > 0 && (
          <div
            className={`bg-red-500 h-full transition-all ${!selected.includes('unhealthy') ? 'opacity-30' : ''}`}
            style={{ width: `${unhealthyPct}%` }}
          />
        )}
        {disabledPct > 0 && (
          <div
            className={`bg-gray-500 dark:bg-gray-700 h-full transition-all ${!selected.includes('disabled') ? 'opacity-30' : ''}`}
            style={{ width: `${disabledPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        <HealthFilterItem
          color="bg-green-500"
          label="Healthy"
          count={links.healthy}
          description="No active issues detected."
          selected={selected.includes('healthy')}
          onClick={() => toggleFilter('healthy')}
          issueBreakdown={issuesByHealth?.healthy}
        />
        <HealthFilterItem
          color="bg-amber-500"
          label="Degraded"
          count={links.degraded}
          description="Moderate packet loss (1% - 10%), or latency SLA breach."
          selected={selected.includes('degraded')}
          onClick={() => toggleFilter('degraded')}
          issueBreakdown={issuesByHealth?.degraded}
        />
        <HealthFilterItem
          color="bg-red-500"
          label="Unhealthy"
          count={links.unhealthy}
          description="Severe packet loss (>= 10%), or missing telemetry (link dark)."
          selected={selected.includes('unhealthy')}
          onClick={() => toggleFilter('unhealthy')}
          issueBreakdown={issuesByHealth?.unhealthy}
        />
        <HealthFilterItem
          color="bg-gray-500 dark:bg-gray-700"
          label="Disabled"
          count={links.disabled || 0}
          description="Drained (soft, hard, or ISIS delay override), or extended packet loss (100% for 2+ hours)."
          selected={selected.includes('disabled')}
          onClick={() => toggleFilter('disabled')}
          issueBreakdown={issuesByHealth?.disabled}
        />
      </div>
    </div>
  )
}

function LinkIssuesFilterCard({
  counts,
  selected,
  onChange,
  healthByIssue,
  timeRange,
}: {
  counts: IssueCounts
  selected: IssueFilter[]
  onChange: (filters: IssueFilter[]) => void
  healthByIssue?: HealthByIssue
  timeRange: TimeRange
}) {
  const allFilters: IssueFilter[] = ['packet_loss', 'high_latency', 'extended_loss', 'drained', 'no_data', 'no_issues']

  const toggleFilter = (filter: IssueFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === allFilters.length

  const items: { filter: IssueFilter; label: string; color: string; description: string }[] = [
    { filter: 'packet_loss', label: 'Packet Loss', color: 'bg-purple-500', description: 'Link experiencing measurable packet loss (>= 1%).' },
    { filter: 'high_latency', label: 'High Latency', color: 'bg-blue-500', description: 'Link latency exceeds committed RTT.' },
    { filter: 'extended_loss', label: 'Extended Loss', color: 'bg-orange-500', description: 'Link has 100% packet loss for 2+ hours.' },
    { filter: 'drained', label: 'Drained', color: 'bg-slate-500 dark:bg-slate-600', description: 'Link is soft-drained, hard-drained, or has ISIS delay override.' },
    { filter: 'no_data', label: 'No Data', color: 'bg-pink-500', description: 'No telemetry received for this link.' },
    { filter: 'no_issues', label: 'No Issues', color: 'bg-cyan-500', description: 'Link with no detected issues in the time range.' },
  ]

  const grandTotal = (counts.total + counts.no_issues) || 1
  const packetLossPct = (counts.packet_loss / grandTotal) * 100
  const highLatencyPct = (counts.high_latency / grandTotal) * 100
  const extendedLossPct = (counts.extended_loss / grandTotal) * 100
  const drainedPct = (counts.drained / grandTotal) * 100
  const noDataPct = (counts.no_data / grandTotal) * 100
  const noIssuesPct = (counts.no_issues / grandTotal) * 100

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Link Issues</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(allFilters)}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {noIssuesPct > 0 && (
          <div
            className={`bg-cyan-500 h-full transition-all ${!selected.includes('no_issues') ? 'opacity-30' : ''}`}
            style={{ width: `${noIssuesPct}%` }}
          />
        )}
        {packetLossPct > 0 && (
          <div
            className={`bg-purple-500 h-full transition-all ${!selected.includes('packet_loss') ? 'opacity-30' : ''}`}
            style={{ width: `${packetLossPct}%` }}
          />
        )}
        {highLatencyPct > 0 && (
          <div
            className={`bg-blue-500 h-full transition-all ${!selected.includes('high_latency') ? 'opacity-30' : ''}`}
            style={{ width: `${highLatencyPct}%` }}
          />
        )}
        {extendedLossPct > 0 && (
          <div
            className={`bg-orange-500 h-full transition-all ${!selected.includes('extended_loss') ? 'opacity-30' : ''}`}
            style={{ width: `${extendedLossPct}%` }}
          />
        )}
        {drainedPct > 0 && (
          <div
            className={`bg-slate-500 dark:bg-slate-600 h-full transition-all ${!selected.includes('drained') ? 'opacity-30' : ''}`}
            style={{ width: `${drainedPct}%` }}
          />
        )}
        {noDataPct > 0 && (
          <div
            className={`bg-pink-500 h-full transition-all ${!selected.includes('no_data') ? 'opacity-30' : ''}`}
            style={{ width: `${noDataPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        {items.map(({ filter, label, color, description }) => (
          <HealthFilterItem
            key={filter}
            color={color}
            label={label}
            count={counts[filter] || 0}
            description={description}
            selected={selected.includes(filter)}
            onClick={() => toggleFilter(filter)}
            healthBreakdown={healthByIssue?.[filter]}
          />
        ))}
      </div>
    </div>
  )
}

function formatBandwidth(bps: number): string {
  if (bps >= 1e9) {
    return `${(bps / 1e9).toFixed(1)} Gbps`
  } else if (bps >= 1e6) {
    return `${(bps / 1e6).toFixed(0)} Mbps`
  } else if (bps >= 1e3) {
    return `${(bps / 1e3).toFixed(0)} Kbps`
  }
  return `${bps.toFixed(0)} bps`
}

function TopLinkUtilization({ links }: { links: StatusResponse['links']['top_util_links'] }) {
  if (!links || links.length === 0) {
    return (
      <div className="border border-border rounded-lg p-4">
        <div className="flex items-center gap-2 mb-3">
          <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Max Link Utilization</h3>
          <span className="text-xs text-muted-foreground ml-auto">p95 - Last 24h</span>
        </div>
        <div className="text-sm text-muted-foreground">No link data available</div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Max Link Utilization</h3>
        <span className="text-xs text-muted-foreground ml-auto">p95 - Last 24h</span>
      </div>
      <div className="space-y-2">
        {links.slice(0, 5).map((link) => {
          const maxUtil = Math.max(link.utilization_in, link.utilization_out)
          const peakBps = Math.max(link.in_bps, link.out_bps)
          return (
            <div key={link.pk} className="flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <Link to={`/dz/links/${link.pk}`} className="font-mono text-xs truncate hover:underline" title={link.code}>{link.code}</Link>
                <div className="text-[10px] text-muted-foreground">{link.side_a_metro} - {link.side_z_metro}</div>
              </div>
              <div className="text-xs text-muted-foreground tabular-nums w-16 text-right">
                {formatBandwidth(peakBps)}
              </div>
              <div className="w-20 flex items-center gap-2">
                <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full ${maxUtil >= 90 ? 'bg-red-500' : maxUtil >= 70 ? 'bg-amber-500' : 'bg-green-500'}`}
                    style={{ width: `${Math.min(maxUtil, 100)}%` }}
                  />
                </div>
                <span className="text-xs tabular-nums w-8 text-right">{maxUtil.toFixed(0)}%</span>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function TopDeviceUtilization({ devices }: { devices: StatusResponse['top_device_util'] }) {
  if (!devices || devices.length === 0) {
    return (
      <div className="border border-border rounded-lg p-4">
        <div className="flex items-center gap-2 mb-3">
          <Cpu className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Top Device Utilization</h3>
          <span className="text-xs text-muted-foreground ml-auto">Current</span>
        </div>
        <div className="text-sm text-muted-foreground">No device data available</div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <Cpu className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Top Device Utilization</h3>
        <span className="text-xs text-muted-foreground ml-auto">Current</span>
      </div>
      <div className="space-y-2">
        {devices.slice(0, 5).map((device) => (
          <div key={device.pk} className="flex items-center gap-3">
            <div className="flex-1 min-w-0">
              <Link to={`/dz/devices/${device.pk}`} className="font-mono text-xs truncate hover:underline" title={device.code}>{device.code}</Link>
              <div className="text-[10px] text-muted-foreground">{device.current_users}/{device.max_users} users</div>
            </div>
            <div className="w-24 flex items-center gap-2">
              <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
                <div
                  className={`h-full rounded-full ${device.utilization >= 80 ? 'bg-amber-500' : 'bg-green-500'}`}
                  style={{ width: `${Math.min(device.utilization, 100)}%` }}
                />
              </div>
              <span className="text-xs tabular-nums w-10 text-right">{device.utilization.toFixed(0)}%</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

interface DisabledLinkRow {
  pk: string
  code: string
  link_type: string
  side_a_metro: string
  side_z_metro: string
  reason: string
}

function DisabledLinksTable({
  drainedLinks,
  packetLossLinks
}: {
  drainedLinks: NonActivatedLink[] | null
  packetLossLinks: DisabledLinkRow[]
}) {
  const allLinks = useMemo(() => {
    const linkMap = new Map<string, DisabledLinkRow>()

    for (const link of drainedLinks || []) {
      const reason = link.status === 'hard-drained' ? 'hard drained' : 'soft drained'
      linkMap.set(link.code, {
        pk: link.pk,
        code: link.code,
        link_type: link.link_type,
        side_a_metro: link.side_a_metro,
        side_z_metro: link.side_z_metro,
        reason,
      })
    }

    for (const link of packetLossLinks) {
      if (!linkMap.has(link.code)) {
        linkMap.set(link.code, link)
      }
    }

    return Array.from(linkMap.values())
  }, [drainedLinks, packetLossLinks])

  if (allLinks.length === 0) return null

  const reasonColors: Record<string, string> = {
    'soft drained': 'text-amber-600 dark:text-amber-400',
    'hard drained': 'text-orange-600 dark:text-orange-400',
    'isis delay override': 'text-amber-600 dark:text-amber-400',
    'extended packet loss': 'text-red-600 dark:text-red-400',
  }

  return (
    <div id="disabled-links" className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Disabled Links</h3>
        <span className="text-sm text-muted-foreground ml-auto">Current</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Link</th>
              <th className="px-4 py-2 font-medium">Route</th>
              <th className="px-4 py-2 font-medium">Reason</th>
            </tr>
          </thead>
          <tbody>
            {allLinks.map((link, idx) => (
              <tr key={`${link.code}-${idx}`} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">
                  <Link to={`/dz/links/${link.pk}`} className="font-mono text-sm hover:underline">{link.code}</Link>
                  <span className="text-xs text-muted-foreground ml-2">{link.link_type}</span>
                </td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{link.side_a_metro} - {link.side_z_metro}</td>
                <td className={`px-4 py-2.5 text-sm capitalize ${reasonColors[link.reason] || ''}`}>
                  {link.reason}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function DisabledDevicesTable({ devices }: { devices: StatusResponse['alerts']['devices'] | null }) {
  if (!devices || devices.length === 0) return null

  const statusColors: Record<string, string> = {
    'soft-drained': 'text-amber-600 dark:text-amber-400',
    'hard-drained': 'text-amber-600 dark:text-amber-400',
    suspended: 'text-red-600 dark:text-red-400',
    pending: 'text-amber-600 dark:text-amber-400',
    deleted: 'text-gray-400',
    rejected: 'text-red-400',
  }

  return (
    <div id="disabled-devices" className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Disabled Devices</h3>
        <span className="text-sm text-muted-foreground ml-auto">Current</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Device</th>
              <th className="px-4 py-2 font-medium">Metro</th>
              <th className="px-4 py-2 font-medium">Status</th>
              <th className="px-4 py-2 font-medium text-right">Since</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((device, idx) => (
              <tr key={`${device.code}-${idx}`} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">
                  <Link to={`/dz/devices/${device.pk}`} className="font-mono text-sm hover:underline">{device.code}</Link>
                  <span className="text-xs text-muted-foreground ml-2">{device.device_type}</span>
                </td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{device.metro}</td>
                <td className={`px-4 py-2.5 text-sm capitalize ${statusColors[device.status] || ''}`}>
                  {device.status.replace('-', ' ')}
                </td>
                <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                  {formatTimeAgo(device.since)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function InterfaceIssuesTable({
  issues,
  timeRange,
  onTimeRangeChange,
  isLoading,
}: {
  issues: InterfaceIssue[] | null
  timeRange: TimeRange
  onTimeRangeChange: (range: TimeRange) => void
  isLoading?: boolean
}) {
  const timeRangeOptions: { value: TimeRange; label: string }[] = [
    { value: '3h', label: '3h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '3d', label: '3d' },
    { value: '7d', label: '7d' },
  ]

  if (isLoading) {
    return (
      <div className="border border-border rounded-lg overflow-hidden">
        <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
          <Cpu className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Interface Issues</h3>
        </div>
        <div className="p-6 text-center text-sm text-muted-foreground">
          Loading interface issues...
        </div>
      </div>
    )
  }

  if (!issues || issues.length === 0) {
    return (
      <div className="border border-border rounded-lg overflow-hidden">
        <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
          <Cpu className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Interface Issues</h3>
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
        </div>
        <div className="p-6 text-center text-sm text-muted-foreground">
          No interface issues in the selected time range
        </div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <Cpu className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Interface Issues</h3>
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
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Device</th>
              <th className="px-4 py-2 font-medium">Interface</th>
              <th className="px-4 py-2 font-medium">Link</th>
              <th className="px-4 py-2 font-medium text-right">Errors</th>
              <th className="px-4 py-2 font-medium text-right">Discards</th>
              <th className="px-4 py-2 font-medium text-right">Carrier</th>
              <th className="px-4 py-2 font-medium text-right">First Seen</th>
              <th className="px-4 py-2 font-medium text-right">Last Seen</th>
            </tr>
          </thead>
          <tbody>
            {issues.map((issue, idx) => {
              const totalErrors = issue.in_errors + issue.out_errors
              const totalDiscards = issue.in_discards + issue.out_discards
              return (
                <tr key={`${issue.device_code}-${issue.interface_name}-${idx}`} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5">
                    <Link to={`/dz/devices/${issue.device_pk}`} className="font-mono text-sm hover:underline">{issue.device_code}</Link>
                    <div className="text-xs text-muted-foreground">{issue.contributor || issue.device_type}</div>
                  </td>
                  <td className="px-4 py-2.5 font-mono text-sm">{issue.interface_name}</td>
                  <td className="px-4 py-2.5 text-sm">
                    {issue.link_code && issue.link_pk ? (
                      <div>
                        <Link to={`/dz/links/${issue.link_pk}`} className="font-mono hover:underline">{issue.link_code}</Link>
                        <span className="text-xs text-muted-foreground ml-1">
                          ({issue.link_type} side {issue.link_side})
                        </span>
                      </div>
                    ) : (
                      <span className="text-muted-foreground">-</span>
                    )}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${totalErrors > 0 ? 'text-red-600 dark:text-red-400' : ''}`}>
                    {totalErrors > 0 ? totalErrors.toLocaleString() : '-'}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${totalDiscards > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                    {totalDiscards > 0 ? totalDiscards.toLocaleString() : '-'}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${issue.carrier_transitions > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                    {issue.carrier_transitions > 0 ? issue.carrier_transitions.toLocaleString() : '-'}
                  </td>
                  <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                    {issue.first_seen ? formatTimeAgo(issue.first_seen) : '-'}
                  </td>
                  <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                    {issue.last_seen ? formatTimeAgo(issue.last_seen) : '-'}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function useBucketCount() {
  const [buckets, setBuckets] = useState(72)

  useEffect(() => {
    const updateBuckets = () => {
      const width = window.innerWidth
      if (width < 640) {
        setBuckets(24)
      } else if (width < 1024) {
        setBuckets(48)
      } else {
        setBuckets(72)
      }
    }

    updateBuckets()
    window.addEventListener('resize', updateBuckets)
    return () => window.removeEventListener('resize', updateBuckets)
  }, [])

  return buckets
}

// Links tab content
function LinksContent({ status, linkHistory, criticalLinks }: { status: StatusResponse; linkHistory: any; criticalLinks: CriticalLinksResponse | undefined }) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [issueFilters, setIssueFilters] = useState<IssueFilter[]>(['packet_loss', 'high_latency', 'extended_loss', 'drained'])
  const [healthFilters, setHealthFilters] = useState<HealthFilter[]>(['healthy', 'degraded', 'unhealthy', 'disabled'])

  // Get search filters from URL
  const searchFilters = useStatusFilters()

  // Bucket count based on time range
  const buckets = (() => {
    switch (timeRange) {
      case '3h': return 36
      case '6h': return 36
      case '12h': return 48
      case '24h': return 72
      case '3d': return 72
      case '7d': return 84
      default: return 72
    }
  })()

  // Fetch link history for the selected time range (used for both display and health/issue counts)
  const { data: linkHistoryData } = useQuery({
    queryKey: ['link-history', timeRange, buckets],
    queryFn: () => fetchLinkHistory(timeRange, buckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  // Apply search filters to link history
  const filteredLinkHistory = useMemo(() => {
    if (!linkHistoryData?.links || searchFilters.length === 0) {
      return linkHistoryData
    }
    return {
      ...linkHistoryData,
      links: linkHistoryData.links.filter((link: LinkHistory) => linkMatchesSearchFilters(link, searchFilters))
    }
  }, [linkHistoryData, searchFilters])

  // Helper to get the effective health status from a link's hours
  // Returns the worst status seen in the time range
  // Excludes the latest bucket if it's no_data (likely still being collected)
  const getEffectiveHealth = (link: LinkHistory): string => {
    if (!link.hours || link.hours.length === 0) return 'healthy'

    // Priority from worst to best (lower index = worse)
    const statusPriority: Record<string, number> = {
      'unhealthy': 0,
      'no_data': 1,
      'disabled': 2,
      'degraded': 3,
      'healthy': 4,
    }

    let worstStatus = 'healthy'
    let worstPriority = statusPriority['healthy']

    // Check if we should skip the last bucket (if it's no_data, it's likely still being collected)
    const lastBucket = link.hours[link.hours.length - 1]
    const skipLastBucket = lastBucket?.status === 'no_data' && link.hours.length > 1
    const bucketsToCheck = skipLastBucket ? link.hours.slice(0, -1) : link.hours

    for (const bucket of bucketsToCheck) {
      const status = bucket.status || 'healthy'
      const priority = statusPriority[status] ?? 4
      if (priority < worstPriority) {
        worstPriority = priority
        worstStatus = status
      }
    }

    return worstStatus
  }

  // Calculate health counts from link history (based on most recent bucket status)
  const healthCounts = useMemo(() => {
    if (!filteredLinkHistory?.links) {
      return { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }
    }

    const counts = { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }

    for (const link of filteredLinkHistory.links) {
      counts.total++
      const status = getEffectiveHealth(link)
      if (status === 'healthy') counts.healthy++
      else if (status === 'degraded') counts.degraded++
      else if (status === 'unhealthy' || status === 'no_data') counts.unhealthy++ // no_data maps to unhealthy
      else if (status === 'disabled') counts.disabled++
    }

    return counts
  }, [filteredLinkHistory])

  // Calculate issue breakdown per health category
  const issuesByHealth = useMemo((): IssuesByHealth => {
    const emptyBreakdown = (): HealthIssueBreakdown => ({
      packet_loss: 0,
      high_latency: 0,
      extended_loss: 0,
      drained: 0,
      no_data: 0,
    })

    const result: IssuesByHealth = {
      healthy: emptyBreakdown(),
      degraded: emptyBreakdown(),
      unhealthy: emptyBreakdown(),
      disabled: emptyBreakdown(),
    }

    if (!filteredLinkHistory?.links) return result

    for (const link of filteredLinkHistory.links) {
      const rawHealth = getEffectiveHealth(link)
      // Map no_data to unhealthy for categorization
      const health = rawHealth === 'no_data' ? 'unhealthy' : rawHealth
      if (!(health in result)) continue

      const breakdown = result[health as keyof IssuesByHealth]
      const issues = link.issue_reasons ?? []

      if (issues.includes('packet_loss')) breakdown.packet_loss++
      if (issues.includes('high_latency')) breakdown.high_latency++
      if (issues.includes('extended_loss')) breakdown.extended_loss++
      if (issues.includes('drained')) breakdown.drained++
      if (issues.includes('no_data')) breakdown.no_data++
    }

    return result
  }, [filteredLinkHistory])

  // Calculate health breakdown per issue type
  const healthByIssue = useMemo((): HealthByIssue => {
    const emptyBreakdown = (): IssueHealthBreakdown => ({
      healthy: 0,
      degraded: 0,
      unhealthy: 0,
      disabled: 0,
    })

    const result: HealthByIssue = {
      packet_loss: emptyBreakdown(),
      high_latency: emptyBreakdown(),
      extended_loss: emptyBreakdown(),
      drained: emptyBreakdown(),
      no_data: emptyBreakdown(),
      no_issues: emptyBreakdown(),
    }

    if (!filteredLinkHistory?.links) return result

    for (const link of filteredLinkHistory.links) {
      const rawHealth = getEffectiveHealth(link)
      // Map no_data to unhealthy for categorization
      const health = (rawHealth === 'no_data' ? 'unhealthy' : rawHealth) as keyof IssueHealthBreakdown
      const issues = link.issue_reasons ?? []

      if (issues.length === 0) {
        result.no_issues[health]++
      } else {
        if (issues.includes('packet_loss')) result.packet_loss[health]++
        if (issues.includes('high_latency')) result.high_latency[health]++
        if (issues.includes('extended_loss')) result.extended_loss[health]++
        if (issues.includes('drained')) result.drained[health]++
        if (issues.includes('no_data')) result.no_data[health]++
      }
    }

    return result
  }, [filteredLinkHistory])

  // Issue counts from filter time range
  const issueCounts = useMemo((): IssueCounts => {
    if (!filteredLinkHistory?.links) {
      return { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    }

    const counts = { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    const seenLinks = new Set<string>()

    for (const link of filteredLinkHistory.links) {
      if (link.issue_reasons?.includes('packet_loss')) counts.packet_loss++
      if (link.issue_reasons?.includes('high_latency')) counts.high_latency++
      if (link.issue_reasons?.includes('extended_loss')) counts.extended_loss++
      if (link.issue_reasons?.includes('drained')) counts.drained++
      if (link.issue_reasons?.includes('no_data')) counts.no_data++
      if (link.issue_reasons?.length > 0 && !seenLinks.has(link.code)) {
        counts.total++
        seenLinks.add(link.code)
      }
    }

    const totalLinks = healthCounts.total || 0
    counts.no_issues = Math.max(0, totalLinks - counts.total)

    return counts
  }, [filteredLinkHistory, healthCounts])

  // Get set of link codes with issues in the filter time range (for filtering history table)
  const linksWithIssues = useMemo(() => {
    if (!filteredLinkHistory?.links) return new Map<string, string[]>()
    const map = new Map<string, string[]>()
    for (const link of filteredLinkHistory.links) {
      if (link.issue_reasons?.length > 0) {
        map.set(link.code, link.issue_reasons)
      }
    }
    return map
  }, [filteredLinkHistory])

  // Get health status for each link from the filter time range (for filtering history table)
  const linksWithHealth = useMemo(() => {
    if (!filteredLinkHistory?.links) return new Map<string, string>()
    const map = new Map<string, string>()
    for (const link of filteredLinkHistory.links) {
      map.set(link.code, getEffectiveHealth(link))
    }
    return map
  }, [filteredLinkHistory])

  // Build criticality map from critical links data
  // Maps link code -> criticality by matching device codes
  const criticalityMap = useMemo(() => {
    const map = new Map<string, 'critical' | 'important' | 'redundant'>()
    if (!criticalLinks?.links || !filteredLinkHistory?.links) return map

    // Build a map from device pair -> criticality
    const devicePairMap = new Map<string, 'critical' | 'important' | 'redundant'>()
    for (const critLink of criticalLinks.links) {
      // Create keys for both directions
      const key1 = `${critLink.sourceCode}::${critLink.targetCode}`
      const key2 = `${critLink.targetCode}::${critLink.sourceCode}`
      devicePairMap.set(key1, critLink.criticality)
      devicePairMap.set(key2, critLink.criticality)
    }

    // Match link history entries to criticality by device codes
    for (const link of filteredLinkHistory.links) {
      if (link.side_a_device && link.side_z_device) {
        const key = `${link.side_a_device}::${link.side_z_device}`
        const criticality = devicePairMap.get(key)
        if (criticality) {
          map.set(link.code, criticality)
        }
      }
    }

    return map
  }, [criticalLinks, filteredLinkHistory])

  const packetLossDisabledLinks = useMemo((): DisabledLinkRow[] => {
    if (!linkHistory?.links || !status) return []

    const drainedCodes = new Set(
      (status.alerts?.links || [])
        .filter(l => l.status === 'soft-drained' || l.status === 'hard-drained')
        .map(l => l.code)
    )

    const bucketsFor2Hours = Math.ceil(120 / (linkHistory.bucket_minutes || 20))

    const isCurrentlyDisabledByPacketLoss = (hours: { status: string }[]): boolean => {
      if (!hours || hours.length < bucketsFor2Hours) return false
      const recentBuckets = hours.slice(-bucketsFor2Hours)
      return recentBuckets.every(h => h.status === 'disabled')
    }

    return linkHistory.links
      .filter((link: LinkHistory) => !drainedCodes.has(link.code) && isCurrentlyDisabledByPacketLoss(link.hours))
      .map((link: LinkHistory) => ({
        pk: link.pk,
        code: link.code,
        link_type: link.link_type,
        side_a_metro: link.side_a_metro,
        side_z_metro: link.side_z_metro,
        reason: 'extended packet loss',
      }))
  }, [linkHistory, status])

  return (
    <>
      {/* Link Health & Issues */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <LinkHealthFilterCard
          links={healthCounts}
          selected={healthFilters}
          onChange={setHealthFilters}
          issuesByHealth={issuesByHealth}
          timeRange={timeRange}
        />
        <LinkIssuesFilterCard
          counts={issueCounts}
          selected={issueFilters}
          onChange={setIssueFilters}
          healthByIssue={healthByIssue}
          timeRange={timeRange}
        />
      </div>

      {/* Link Status History */}
      <div id="link-status-history" className="mb-8 scroll-mt-8">
        <LinkStatusTimelines timeRange={timeRange} onTimeRangeChange={setTimeRange} issueFilters={issueFilters} healthFilters={healthFilters} linksWithIssues={linksWithIssues} linksWithHealth={linksWithHealth} criticalityMap={criticalityMap} />
      </div>

      {/* Disabled Links */}
      <div className="mb-8">
        <DisabledLinksTable
          drainedLinks={status.alerts?.links}
          packetLossLinks={packetLossDisabledLinks}
        />
      </div>

      {/* Methodology link */}
      <div className="text-center text-sm text-muted-foreground pb-4">
        <Link to="/status/methodology" className="hover:text-foreground hover:underline">
          How is status calculated?
        </Link>
      </div>
    </>
  )
}

// Device issue counts interface
interface DeviceIssueCounts {
  interface_errors: number
  discards: number
  carrier_transitions: number
  drained: number
  no_issues: number
  total: number
}

// Device issues breakdown per health category
interface DeviceIssuesByHealth {
  healthy: { interface_errors: number; discards: number; carrier_transitions: number; drained: number }
  degraded: { interface_errors: number; discards: number; carrier_transitions: number; drained: number }
  unhealthy: { interface_errors: number; discards: number; carrier_transitions: number; drained: number }
  disabled: { interface_errors: number; discards: number; carrier_transitions: number; drained: number }
}

// Device health breakdown per issue type
interface DeviceHealthByIssue {
  interface_errors: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  discards: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  carrier_transitions: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  drained: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  no_issues: { healthy: number; degraded: number; unhealthy: number; disabled: number }
}

function DeviceHealthFilterCard({
  devices,
  selected,
  onChange,
  issuesByHealth,
  timeRange,
}: {
  devices: { healthy: number; degraded: number; unhealthy: number; disabled: number; total: number }
  selected: HealthFilter[]
  onChange: (filters: HealthFilter[]) => void
  issuesByHealth?: DeviceIssuesByHealth
  timeRange: TimeRange
}) {
  const toggleFilter = (filter: HealthFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === 4

  const healthyPct = devices.total > 0 ? (devices.healthy / devices.total) * 100 : 100
  const degradedPct = devices.total > 0 ? (devices.degraded / devices.total) * 100 : 0
  const unhealthyPct = devices.total > 0 ? (devices.unhealthy / devices.total) * 100 : 0
  const disabledPct = devices.total > 0 ? ((devices.disabled || 0) / devices.total) * 100 : 0

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <Cpu className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Device Health</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(['healthy', 'degraded', 'unhealthy', 'disabled'])}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {healthyPct > 0 && (
          <div
            className={`bg-green-500 h-full transition-all ${!selected.includes('healthy') ? 'opacity-30' : ''}`}
            style={{ width: `${healthyPct}%` }}
          />
        )}
        {degradedPct > 0 && (
          <div
            className={`bg-amber-500 h-full transition-all ${!selected.includes('degraded') ? 'opacity-30' : ''}`}
            style={{ width: `${degradedPct}%` }}
          />
        )}
        {unhealthyPct > 0 && (
          <div
            className={`bg-red-500 h-full transition-all ${!selected.includes('unhealthy') ? 'opacity-30' : ''}`}
            style={{ width: `${unhealthyPct}%` }}
          />
        )}
        {disabledPct > 0 && (
          <div
            className={`bg-gray-500 dark:bg-gray-700 h-full transition-all ${!selected.includes('disabled') ? 'opacity-30' : ''}`}
            style={{ width: `${disabledPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        <HealthFilterItem
          color="bg-green-500"
          label="Healthy"
          count={devices.healthy}
          description="No interface issues detected."
          selected={selected.includes('healthy')}
          onClick={() => toggleFilter('healthy')}
          issueBreakdown={issuesByHealth?.healthy ? { ...issuesByHealth.healthy, high_latency: 0, extended_loss: 0, packet_loss: 0, no_data: 0 } : undefined}
        />
        <HealthFilterItem
          color="bg-amber-500"
          label="Degraded"
          count={devices.degraded}
          description="Moderate interface errors or discards."
          selected={selected.includes('degraded')}
          onClick={() => toggleFilter('degraded')}
          issueBreakdown={issuesByHealth?.degraded ? { ...issuesByHealth.degraded, high_latency: 0, extended_loss: 0, packet_loss: 0, no_data: 0 } : undefined}
        />
        <HealthFilterItem
          color="bg-red-500"
          label="Unhealthy"
          count={devices.unhealthy}
          description="Significant interface errors or carrier transitions."
          selected={selected.includes('unhealthy')}
          onClick={() => toggleFilter('unhealthy')}
          issueBreakdown={issuesByHealth?.unhealthy ? { ...issuesByHealth.unhealthy, high_latency: 0, extended_loss: 0, packet_loss: 0, no_data: 0 } : undefined}
        />
        <HealthFilterItem
          color="bg-gray-500 dark:bg-gray-700"
          label="Disabled"
          count={devices.disabled || 0}
          description="Device is drained or suspended."
          selected={selected.includes('disabled')}
          onClick={() => toggleFilter('disabled')}
          issueBreakdown={issuesByHealth?.disabled ? { ...issuesByHealth.disabled, high_latency: 0, extended_loss: 0, packet_loss: 0, no_data: 0 } : undefined}
        />
      </div>
    </div>
  )
}

function DeviceIssuesFilterCard({
  counts,
  selected,
  onChange,
  healthByIssue,
  timeRange,
}: {
  counts: DeviceIssueCounts
  selected: DeviceIssueFilter[]
  onChange: (filters: DeviceIssueFilter[]) => void
  healthByIssue?: DeviceHealthByIssue
  timeRange: TimeRange
}) {
  const allFilters: DeviceIssueFilter[] = ['interface_errors', 'discards', 'carrier_transitions', 'drained', 'no_issues']

  const toggleFilter = (filter: DeviceIssueFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === allFilters.length

  const grandTotal = (counts.total + counts.no_issues) || 1
  const interfaceErrorsPct = (counts.interface_errors / grandTotal) * 100
  const discardsPct = (counts.discards / grandTotal) * 100
  const carrierTransitionsPct = (counts.carrier_transitions / grandTotal) * 100
  const drainedPct = (counts.drained / grandTotal) * 100
  const noIssuesPct = (counts.no_issues / grandTotal) * 100

  const items: { filter: DeviceIssueFilter; label: string; color: string; description: string }[] = [
    { filter: 'interface_errors', label: 'Interface Errors', color: 'bg-fuchsia-500', description: 'Device experiencing interface errors.' },
    { filter: 'discards', label: 'Discards', color: 'bg-rose-500', description: 'Device experiencing interface discards.' },
    { filter: 'carrier_transitions', label: 'Link Flapping', color: 'bg-orange-500', description: 'Device experiencing carrier state changes (link up/down).' },
    { filter: 'drained', label: 'Drained', color: 'bg-slate-500 dark:bg-slate-600', description: 'Device is soft-drained, hard-drained, or suspended.' },
    { filter: 'no_issues', label: 'No Issues', color: 'bg-cyan-500', description: 'Device with no detected issues in the time range.' },
  ]

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Device Issues</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(allFilters)}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {noIssuesPct > 0 && (
          <div
            className={`bg-cyan-500 h-full transition-all ${!selected.includes('no_issues') ? 'opacity-30' : ''}`}
            style={{ width: `${noIssuesPct}%` }}
          />
        )}
        {interfaceErrorsPct > 0 && (
          <div
            className={`bg-fuchsia-500 h-full transition-all ${!selected.includes('interface_errors') ? 'opacity-30' : ''}`}
            style={{ width: `${interfaceErrorsPct}%` }}
          />
        )}
        {discardsPct > 0 && (
          <div
            className={`bg-rose-500 h-full transition-all ${!selected.includes('discards') ? 'opacity-30' : ''}`}
            style={{ width: `${discardsPct}%` }}
          />
        )}
        {carrierTransitionsPct > 0 && (
          <div
            className={`bg-orange-500 h-full transition-all ${!selected.includes('carrier_transitions') ? 'opacity-30' : ''}`}
            style={{ width: `${carrierTransitionsPct}%` }}
          />
        )}
        {drainedPct > 0 && (
          <div
            className={`bg-slate-500 dark:bg-slate-600 h-full transition-all ${!selected.includes('drained') ? 'opacity-30' : ''}`}
            style={{ width: `${drainedPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        {items.map(({ filter, label, color, description }) => (
          <HealthFilterItem
            key={filter}
            color={color}
            label={label}
            count={counts[filter] || 0}
            description={description}
            selected={selected.includes(filter)}
            onClick={() => toggleFilter(filter)}
            healthBreakdown={healthByIssue?.[filter]}
          />
        ))}
      </div>
    </div>
  )
}

// Helper to check if a device matches search filters
function deviceMatchesSearchFilters(device: DeviceHistory, filters: StatusFilter[]): boolean {
  if (filters.length === 0) return true

  return filters.some(filter => {
    switch (filter.type) {
      case 'device':
        return device.code.toLowerCase().includes(filter.value.toLowerCase())
      case 'metro':
        return device.metro?.toLowerCase() === filter.value.toLowerCase()
      case 'contributor':
        return device.contributor?.toLowerCase().includes(filter.value.toLowerCase())
      default:
        return false
    }
  })
}

// Helper to check if an interface issue matches search filters
function interfaceIssueMatchesSearchFilters(issue: InterfaceIssue, filters: StatusFilter[]): boolean {
  if (filters.length === 0) return true

  return filters.some(filter => {
    switch (filter.type) {
      case 'device':
        return issue.device_code.toLowerCase().includes(filter.value.toLowerCase())
      case 'metro':
        return issue.metro?.toLowerCase() === filter.value.toLowerCase()
      case 'contributor':
        return issue.contributor?.toLowerCase().includes(filter.value.toLowerCase())
      case 'link':
        return issue.link_code?.toLowerCase().includes(filter.value.toLowerCase())
      default:
        return false
    }
  })
}

// Helper to check if a link matches search filters
function linkMatchesSearchFilters(link: LinkHistory, filters: StatusFilter[]): boolean {
  if (filters.length === 0) return true

  return filters.some(filter => {
    switch (filter.type) {
      case 'link':
        return link.code.toLowerCase().includes(filter.value.toLowerCase())
      case 'device':
        return link.side_a_device?.toLowerCase().includes(filter.value.toLowerCase()) ||
               link.side_z_device?.toLowerCase().includes(filter.value.toLowerCase())
      case 'metro':
        return link.side_a_metro?.toLowerCase() === filter.value.toLowerCase() ||
               link.side_z_metro?.toLowerCase() === filter.value.toLowerCase()
      case 'contributor':
        return link.contributor?.toLowerCase().includes(filter.value.toLowerCase())
      default:
        return false
    }
  })
}

// Helper to check if a link metric (for utilization) matches search filters
function linkMetricMatchesSearchFilters(link: LinkMetric, filters: StatusFilter[]): boolean {
  if (filters.length === 0) return true

  return filters.some(filter => {
    switch (filter.type) {
      case 'link':
        return link.code.toLowerCase().includes(filter.value.toLowerCase())
      case 'metro':
        return link.side_a_metro?.toLowerCase() === filter.value.toLowerCase() ||
               link.side_z_metro?.toLowerCase() === filter.value.toLowerCase()
      case 'contributor':
        return link.contributor?.toLowerCase().includes(filter.value.toLowerCase())
      default:
        return false
    }
  })
}

// Helper to check if a device utilization matches search filters
function deviceUtilMatchesSearchFilters(device: DeviceUtilization, filters: StatusFilter[]): boolean {
  if (filters.length === 0) return true

  return filters.some(filter => {
    switch (filter.type) {
      case 'device':
        return device.code.toLowerCase().includes(filter.value.toLowerCase())
      case 'metro':
        return device.metro?.toLowerCase() === filter.value.toLowerCase()
      case 'contributor':
        return device.contributor?.toLowerCase().includes(filter.value.toLowerCase())
      default:
        return false
    }
  })
}

// Devices tab content
function DevicesContent({ status }: { status: StatusResponse }) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [issueFilters, setIssueFilters] = useState<DeviceIssueFilter[]>(['interface_errors', 'discards', 'carrier_transitions', 'drained'])
  const [healthFilters, setHealthFilters] = useState<HealthFilter[]>(['healthy', 'degraded', 'unhealthy', 'disabled'])

  // Get search filters from URL
  const searchFilters = useStatusFilters()

  // Bucket count based on time range
  const buckets = (() => {
    switch (timeRange) {
      case '3h': return 36
      case '6h': return 36
      case '12h': return 48
      case '24h': return 72
      case '3d': return 72
      case '7d': return 84
      default: return 72
    }
  })()

  // Fetch device history for the selected time range (used for both display and health/issue counts)
  const { data: deviceHistoryData } = useQuery({
    queryKey: ['device-history', timeRange, buckets],
    queryFn: () => fetchDeviceHistory(timeRange, buckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  // Fetch interface issues for the selected time range
  const { data: interfaceIssuesData, isLoading: interfaceIssuesLoading } = useQuery({
    queryKey: ['interface-issues', timeRange],
    queryFn: () => fetchInterfaceIssues(timeRange),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  // Apply search filters to device history
  const filteredDeviceHistory = useMemo(() => {
    if (!deviceHistoryData?.devices || searchFilters.length === 0) {
      return deviceHistoryData
    }
    return {
      ...deviceHistoryData,
      devices: deviceHistoryData.devices.filter(d => deviceMatchesSearchFilters(d, searchFilters))
    }
  }, [deviceHistoryData, searchFilters])

  // Apply search filters to interface issues
  const filteredInterfaceIssues = useMemo(() => {
    if (!interfaceIssuesData?.issues || searchFilters.length === 0) {
      return interfaceIssuesData?.issues ?? null
    }
    return interfaceIssuesData.issues.filter(i => interfaceIssueMatchesSearchFilters(i, searchFilters))
  }, [interfaceIssuesData, searchFilters])

  // Helper to get the effective health status from a device's hours
  const getEffectiveHealth = (device: DeviceHistory): string => {
    if (!device.hours || device.hours.length === 0) return 'healthy'

    const statusPriority: Record<string, number> = {
      'unhealthy': 0,
      'no_data': 1,
      'disabled': 2,
      'degraded': 3,
      'healthy': 4,
    }

    let worstStatus = 'healthy'
    let worstPriority = statusPriority['healthy']

    // Skip the last bucket if it's no_data (still being collected)
    const lastBucket = device.hours[device.hours.length - 1]
    const skipLastBucket = lastBucket?.status === 'no_data' && device.hours.length > 1
    const bucketsToCheck = skipLastBucket ? device.hours.slice(0, -1) : device.hours

    for (const bucket of bucketsToCheck) {
      const status = bucket.status || 'healthy'
      const priority = statusPriority[status] ?? 4
      if (priority < worstPriority) {
        worstPriority = priority
        worstStatus = status
      }
    }

    return worstStatus
  }

  // Calculate health counts from device history
  const healthCounts = useMemo(() => {
    if (!filteredDeviceHistory?.devices) {
      return { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }
    }

    const counts = { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }

    for (const device of filteredDeviceHistory.devices) {
      counts.total++
      const status = getEffectiveHealth(device)
      if (status === 'healthy') counts.healthy++
      else if (status === 'degraded') counts.degraded++
      else if (status === 'unhealthy' || status === 'no_data') counts.unhealthy++
      else if (status === 'disabled') counts.disabled++
    }

    return counts
  }, [filteredDeviceHistory])

  // Calculate issue breakdown per health category
  const issuesByHealth = useMemo((): DeviceIssuesByHealth => {
    const emptyBreakdown = () => ({ interface_errors: 0, discards: 0, carrier_transitions: 0, drained: 0 })

    const result: DeviceIssuesByHealth = {
      healthy: emptyBreakdown(),
      degraded: emptyBreakdown(),
      unhealthy: emptyBreakdown(),
      disabled: emptyBreakdown(),
    }

    if (!filteredDeviceHistory?.devices) return result

    for (const device of filteredDeviceHistory.devices) {
      const rawHealth = getEffectiveHealth(device)
      const health = rawHealth === 'no_data' ? 'unhealthy' : rawHealth
      if (!(health in result)) continue

      const breakdown = result[health as keyof DeviceIssuesByHealth]
      const issues = device.issue_reasons ?? []

      if (issues.includes('interface_errors')) breakdown.interface_errors++
      if (issues.includes('discards')) breakdown.discards++
      if (issues.includes('carrier_transitions')) breakdown.carrier_transitions++
      if (issues.includes('drained')) breakdown.drained++
    }

    return result
  }, [filteredDeviceHistory])

  // Calculate health breakdown per issue type
  const healthByIssue = useMemo((): DeviceHealthByIssue => {
    const emptyBreakdown = () => ({ healthy: 0, degraded: 0, unhealthy: 0, disabled: 0 })

    const result: DeviceHealthByIssue = {
      interface_errors: emptyBreakdown(),
      discards: emptyBreakdown(),
      carrier_transitions: emptyBreakdown(),
      drained: emptyBreakdown(),
      no_issues: emptyBreakdown(),
    }

    if (!filteredDeviceHistory?.devices) return result

    for (const device of filteredDeviceHistory.devices) {
      const rawHealth = getEffectiveHealth(device)
      const health = (rawHealth === 'no_data' ? 'unhealthy' : rawHealth) as keyof IssueHealthBreakdown
      const issues = device.issue_reasons ?? []

      if (issues.length === 0) {
        result.no_issues[health]++
      } else {
        if (issues.includes('interface_errors')) result.interface_errors[health]++
        if (issues.includes('discards')) result.discards[health]++
        if (issues.includes('carrier_transitions')) result.carrier_transitions[health]++
        if (issues.includes('drained')) result.drained[health]++
      }
    }

    return result
  }, [filteredDeviceHistory])

  // Issue counts from filter time range
  const issueCounts = useMemo((): DeviceIssueCounts => {
    if (!filteredDeviceHistory?.devices) {
      return { interface_errors: 0, discards: 0, carrier_transitions: 0, drained: 0, no_issues: 0, total: 0 }
    }

    const counts = { interface_errors: 0, discards: 0, carrier_transitions: 0, drained: 0, no_issues: 0, total: 0 }
    const seenDevices = new Set<string>()

    for (const device of filteredDeviceHistory.devices) {
      if (device.issue_reasons?.includes('interface_errors')) counts.interface_errors++
      if (device.issue_reasons?.includes('discards')) counts.discards++
      if (device.issue_reasons?.includes('carrier_transitions')) counts.carrier_transitions++
      if (device.issue_reasons?.includes('drained')) counts.drained++
      if (device.issue_reasons?.length > 0 && !seenDevices.has(device.code)) {
        counts.total++
        seenDevices.add(device.code)
      }
    }

    const totalDevices = healthCounts.total || 0
    counts.no_issues = Math.max(0, totalDevices - counts.total)

    return counts
  }, [filteredDeviceHistory, healthCounts])

  // Get set of device codes with issues in the filter time range
  const devicesWithIssues = useMemo(() => {
    if (!filteredDeviceHistory?.devices) return new Map<string, string[]>()
    const map = new Map<string, string[]>()
    for (const device of filteredDeviceHistory.devices) {
      if (device.issue_reasons?.length > 0) {
        map.set(device.code, device.issue_reasons)
      }
    }
    return map
  }, [filteredDeviceHistory])

  // Get health status for each device from the filter time range
  const devicesWithHealth = useMemo(() => {
    if (!filteredDeviceHistory?.devices) return new Map<string, string>()
    const map = new Map<string, string>()
    for (const device of filteredDeviceHistory.devices) {
      map.set(device.code, getEffectiveHealth(device))
    }
    return map
  }, [filteredDeviceHistory])

  return (
    <>
      {/* Device Health & Issues */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <DeviceHealthFilterCard
          devices={healthCounts}
          selected={healthFilters}
          onChange={setHealthFilters}
          issuesByHealth={issuesByHealth}
          timeRange={timeRange}
        />
        <DeviceIssuesFilterCard
          counts={issueCounts}
          selected={issueFilters}
          onChange={setIssueFilters}
          healthByIssue={healthByIssue}
          timeRange={timeRange}
        />
      </div>

      {/* Device Status History */}
      <div className="mb-8">
        <DeviceStatusTimelines
          timeRange={timeRange}
          onTimeRangeChange={setTimeRange}
          issueFilters={issueFilters}
          healthFilters={healthFilters}
          devicesWithIssues={devicesWithIssues}
          devicesWithHealth={devicesWithHealth}
        />
      </div>

      {/* Disabled Devices */}
      <div className="mb-8">
        <DisabledDevicesTable devices={status.alerts?.devices} />
      </div>

      {/* Interface Issues */}
      <div className="mb-8">
        <InterfaceIssuesTable
          issues={filteredInterfaceIssues}
          timeRange={timeRange}
          onTimeRangeChange={setTimeRange}
          isLoading={interfaceIssuesLoading}
        />
      </div>

      {/* Methodology link */}
      <div className="text-center text-sm text-muted-foreground pb-4">
        <Link to="/status/methodology" className="hover:text-foreground hover:underline">
          How is status calculated?
        </Link>
      </div>
    </>
  )
}

// Metro health filter card
function MetroHealthFilterCard({
  metros,
  selected,
  onChange,
  timeRange,
}: {
  metros: { healthy: number; degraded: number; unhealthy: number; total: number }
  selected: MetroHealthFilter[]
  onChange: (filters: MetroHealthFilter[]) => void
  timeRange: TimeRange
}) {
  const toggleFilter = (filter: MetroHealthFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === 3

  const healthyPct = metros.total > 0 ? (metros.healthy / metros.total) * 100 : 100
  const degradedPct = metros.total > 0 ? (metros.degraded / metros.total) * 100 : 0
  const unhealthyPct = metros.total > 0 ? (metros.unhealthy / metros.total) * 100 : 0

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Metro Health</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(['healthy', 'degraded', 'unhealthy'])}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {healthyPct > 0 && (
          <div
            className={`bg-green-500 h-full transition-all ${!selected.includes('healthy') ? 'opacity-30' : ''}`}
            style={{ width: `${healthyPct}%` }}
          />
        )}
        {degradedPct > 0 && (
          <div
            className={`bg-amber-500 h-full transition-all ${!selected.includes('degraded') ? 'opacity-30' : ''}`}
            style={{ width: `${degradedPct}%` }}
          />
        )}
        {unhealthyPct > 0 && (
          <div
            className={`bg-red-500 h-full transition-all ${!selected.includes('unhealthy') ? 'opacity-30' : ''}`}
            style={{ width: `${unhealthyPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        <HealthFilterItem
          color="bg-green-500"
          label="Operational"
          count={metros.healthy}
          description="All links operational, no issues detected."
          selected={selected.includes('healthy')}
          onClick={() => toggleFilter('healthy')}
        />
        <HealthFilterItem
          color="bg-amber-500"
          label="Some Issues"
          count={metros.degraded}
          description="Some links down but metro still connected."
          selected={selected.includes('degraded')}
          onClick={() => toggleFilter('degraded')}
        />
        <HealthFilterItem
          color="bg-red-500"
          label="Significant Issues"
          count={metros.unhealthy}
          description="Most links down, major connectivity impact."
          selected={selected.includes('unhealthy')}
          onClick={() => toggleFilter('unhealthy')}
        />
      </div>
    </div>
  )
}

// Metro issues filter card
function MetroIssuesFilterCard({
  counts,
  selected,
  onChange,
  timeRange,
}: {
  counts: { has_issues: number; has_spof: number; no_issues: number; total: number }
  selected: MetroIssueFilter[]
  onChange: (filters: MetroIssueFilter[]) => void
  timeRange: TimeRange
}) {
  const allFilters: MetroIssueFilter[] = ['has_issues', 'has_spof', 'no_issues']

  const toggleFilter = (filter: MetroIssueFilter) => {
    if (selected.includes(filter)) {
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === allFilters.length

  const grandTotal = counts.total || 1
  const hasIssuesPct = (counts.has_issues / grandTotal) * 100
  const hasSpofPct = (counts.has_spof / grandTotal) * 100
  const noIssuesPct = (counts.no_issues / grandTotal) * 100

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Metro Issues</h3>
        <span className="text-xs text-muted-foreground">({timeRangeLabels[timeRange]})</span>
        <button
          onClick={() => onChange(allFilters)}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      <div className="h-2 rounded-full overflow-hidden flex mb-3 bg-muted">
        {noIssuesPct > 0 && (
          <div
            className={`bg-cyan-500 h-full transition-all ${!selected.includes('no_issues') ? 'opacity-30' : ''}`}
            style={{ width: `${noIssuesPct}%` }}
          />
        )}
        {hasIssuesPct > 0 && (
          <div
            className={`bg-purple-500 h-full transition-all ${!selected.includes('has_issues') ? 'opacity-30' : ''}`}
            style={{ width: `${hasIssuesPct}%` }}
          />
        )}
        {hasSpofPct > 0 && (
          <div
            className={`bg-red-500 h-full transition-all ${!selected.includes('has_spof') ? 'opacity-30' : ''}`}
            style={{ width: `${hasSpofPct}%` }}
          />
        )}
      </div>

      <div className="space-y-0.5 text-sm">
        <HealthFilterItem
          color="bg-purple-500"
          label="Has Issues"
          count={counts.has_issues}
          description="Metro has links with health issues in the time range."
          selected={selected.includes('has_issues')}
          onClick={() => toggleFilter('has_issues')}
        />
        <HealthFilterItem
          color="bg-red-500"
          label="Has SPOF"
          count={counts.has_spof}
          description="Metro has single points of failure (critical links)."
          selected={selected.includes('has_spof')}
          onClick={() => toggleFilter('has_spof')}
        />
        <HealthFilterItem
          color="bg-cyan-500"
          label="No Issues"
          count={counts.no_issues}
          description="Metro with no issues and no critical links."
          selected={selected.includes('no_issues')}
          onClick={() => toggleFilter('no_issues')}
        />
      </div>
    </div>
  )
}

// Metros tab content
function MetrosContent({
  linkHistory,
  criticalLinks,
  isLoading,
  metroNames,
}: {
  linkHistory: any
  criticalLinks: CriticalLinksResponse | undefined
  isLoading: boolean
  metroNames: Map<string, string>
}) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [healthFilters, setHealthFilters] = useState<MetroHealthFilter[]>(['healthy', 'degraded', 'unhealthy'])
  const [issueFilters, setIssueFilters] = useState<MetroIssueFilter[]>(['has_issues', 'has_spof', 'no_issues'])

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

  // Calculate metro counts for filter cards using proportional logic
  // Metro health is based on % of working links, not worst-case single link
  const metroCounts = useMemo(() => {
    if (!linkHistory?.links) {
      return {
        health: { healthy: 0, degraded: 0, unhealthy: 0, total: 0 },
        issues: { has_issues: 0, has_spof: 0, no_issues: 0, total: 0 },
      }
    }

    // Track link counts per metro for current status (last bucket)
    const metroMap = new Map<string, {
      workingLinks: number  // healthy or degraded
      downLinks: number     // unhealthy, disabled, no_data
      totalLinks: number
      hasIssues: boolean    // had any non-healthy status in time range
      hasSpof: boolean
    }>()

    for (const link of linkHistory.links) {
      if (!link.hours || link.hours.length === 0) continue

      const deviceKey = `${link.side_a_device}--${link.side_z_device}`
      const isSpof = criticalLinkSet.has(deviceKey)

      // Get current status from last bucket
      // If last bucket is no_data (still collecting), use the previous bucket
      const lastBucket = link.hours[link.hours.length - 1]
      const prevBucket = link.hours.length > 1 ? link.hours[link.hours.length - 2] : null
      const currentStatus = (lastBucket?.status === 'no_data' && prevBucket)
        ? (prevBucket.status || 'healthy')
        : (lastBucket?.status || 'healthy')
      const isWorking = currentStatus === 'healthy' || currentStatus === 'degraded'

      // Check if link had any issues in time range (no_data is also an issue)
      const hadIssues = link.hours.some((h: { status: string }) =>
        h.status === 'unhealthy' || h.status === 'degraded' || h.status === 'disabled' || h.status === 'no_data'
      )

      for (const metroCode of [link.side_a_metro, link.side_z_metro]) {
        if (!metroCode) continue
        if (!metroMap.has(metroCode)) {
          metroMap.set(metroCode, { workingLinks: 0, downLinks: 0, totalLinks: 0, hasIssues: false, hasSpof: false })
        }
        const metro = metroMap.get(metroCode)!
        metro.totalLinks++
        if (isWorking) metro.workingLinks++
        else metro.downLinks++
        if (hadIssues) metro.hasIssues = true
        if (isSpof) metro.hasSpof = true
      }
    }

    const health = { healthy: 0, degraded: 0, unhealthy: 0, total: 0 }
    const issues = { has_issues: 0, has_spof: 0, no_issues: 0, total: 0 }

    for (const [, data] of metroMap) {
      health.total++
      issues.total++

      // Proportional health: >= 80% working = healthy, 20-80% = degraded, < 20% = unhealthy
      const workingPct = data.totalLinks > 0 ? (data.workingLinks / data.totalLinks) * 100 : 100
      if (workingPct >= 80) health.healthy++
      else if (workingPct >= 20) health.degraded++
      else health.unhealthy++

      if (data.hasSpof) issues.has_spof++
      if (data.hasIssues) issues.has_issues++
      if (!data.hasIssues && !data.hasSpof) issues.no_issues++
    }

    return { health, issues }
  }, [linkHistory, criticalLinkSet])

  return (
    <>
      {/* Metro Health & Issues Filter Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <MetroHealthFilterCard
          metros={metroCounts.health}
          selected={healthFilters}
          onChange={setHealthFilters}
          timeRange={timeRange}
        />
        <MetroIssuesFilterCard
          counts={metroCounts.issues}
          selected={issueFilters}
          onChange={setIssueFilters}
          timeRange={timeRange}
        />
      </div>

      {/* Metro Status History */}
      <div className="mb-8">
        <MetroStatusTimelines
          linkHistory={linkHistory}
          criticalLinks={criticalLinks}
          isLoading={isLoading}
          timeRange={timeRange}
          onTimeRangeChange={setTimeRange}
          healthFilters={healthFilters}
          issueFilters={issueFilters}
          metroNames={metroNames}
        />
      </div>

      {/* Methodology link */}
      <div className="text-center text-sm text-muted-foreground pb-4">
        <Link to="/status/methodology" className="hover:text-foreground hover:underline">
          How is status calculated?
        </Link>
      </div>
    </>
  )
}

export function StatusPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const buckets = useBucketCount()

  // Determine active tab from URL
  const activeTab = location.pathname.includes('/devices') ? 'devices' :
                    location.pathname.includes('/metros') ? 'metros' : 'links'

  // Redirect /status to /status/links (preserving query params)
  useEffect(() => {
    if (location.pathname === '/status') {
      navigate('/status/links' + location.search, { replace: true })
    }
  }, [location.pathname, location.search, navigate])

  const { data: status, isLoading, error } = useQuery({
    queryKey: ['status'],
    queryFn: fetchStatus,
    refetchInterval: 30_000,
    staleTime: 15_000,
  })

  const { data: linkHistory } = useQuery({
    queryKey: ['link-history', '24h', buckets],
    queryFn: () => fetchLinkHistory('24h', buckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  const { data: criticalLinks, isLoading: criticalLinksLoading } = useQuery({
    queryKey: ['critical-links'],
    queryFn: fetchCriticalLinks,
    staleTime: 5 * 60_000, // 5 minutes - topology changes less frequently
  })

  const { data: metrosData } = useQuery({
    queryKey: ['metros'],
    queryFn: () => fetchMetros(500, 0), // Fetch all metros
    staleTime: 5 * 60_000,
  })

  // Build metro code -> name lookup
  const metroNames = useMemo(() => {
    const map = new Map<string, string>()
    if (metrosData?.items) {
      for (const metro of metrosData.items) {
        map.set(metro.code, metro.name)
      }
    }
    return map
  }, [metrosData])

  const showSkeleton = useDelayedLoading(isLoading)

  // Get search filters for filtering utilization data
  const searchFilters = useStatusFilters()

  // Filter top link utilization by search filters
  const filteredTopLinks = useMemo(() => {
    if (!status?.links?.top_util_links || searchFilters.length === 0) {
      return status?.links?.top_util_links ?? []
    }
    return status.links.top_util_links.filter(link => linkMetricMatchesSearchFilters(link, searchFilters))
  }, [status?.links?.top_util_links, searchFilters])

  // Filter top device utilization by search filters
  const filteredTopDevices = useMemo(() => {
    if (!status?.top_device_util || searchFilters.length === 0) {
      return status?.top_device_util ?? []
    }
    return status.top_device_util.filter(device => deviceUtilMatchesSearchFilters(device, searchFilters))
  }, [status?.top_device_util, searchFilters])

  if (isLoading && showSkeleton) {
    return <StatusPageSkeleton />
  }

  if (isLoading) {
    return null
  }

  if (error || !status) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <XCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <div className="text-lg font-medium mb-2">Unable to load status</div>
          <div className="text-sm text-muted-foreground">{error?.message || 'Unknown error'}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <StatusIndicator statusData={status} />
        </div>

        {/* Network Stats Grid */}
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-x-8 gap-y-6 mb-8">
          <StatCard label="Contributors" value={status.network.contributors} format="number" />
          <StatCard label="Metros" value={status.network.metros} format="number" />
          <StatCard label="Devices" value={status.network.devices} format="number" />
          <StatCard label="Links" value={status.network.links} format="number" />
          <StatCard label="Users" value={status.network.users} format="number" />
          <StatCard label="Validators on DZ" value={status.network.validators_on_dz} format="number" />
          <StatCard label="SOL Connected" value={status.network.total_stake_sol} format="stake" />
          <StatCard label="Stake Share" value={status.network.stake_share_pct} format="percent" delta={status.network.stake_share_delta} />
          <StatCard label="WAN Capacity" value={status.network.wan_bandwidth_bps} format="bandwidth" />
          <StatCard label="User Inbound" value={status.network.user_inbound_bps} format="bandwidth" />
        </div>

        {/* Utilization Charts */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-8">
          <TopLinkUtilization links={filteredTopLinks} />
          <TopDeviceUtilization devices={filteredTopDevices} />
        </div>

        {/* Tab Navigation */}
        <TabNavigation activeTab={activeTab} />

        {/* Tab Content */}
        {activeTab === 'links' ? (
          <LinksContent status={status} linkHistory={linkHistory} criticalLinks={criticalLinks} />
        ) : activeTab === 'devices' ? (
          <DevicesContent status={status} />
        ) : (
          <MetrosContent linkHistory={linkHistory} criticalLinks={criticalLinks} isLoading={!linkHistory || criticalLinksLoading} metroNames={metroNames} />
        )}
      </div>
    </div>
  )
}

// Export for routes
export function StatusLinksPage() {
  return <StatusPage />
}

export function StatusDevicesPage() {
  return <StatusPage />
}

export function StatusMetrosPage() {
  return <StatusPage />
}
