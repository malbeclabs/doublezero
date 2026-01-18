import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import { Link } from 'react-router-dom'
import { CheckCircle2, AlertTriangle, XCircle, ArrowUpDown, Cpu } from 'lucide-react'
import { fetchStatus, fetchLinkHistory, type StatusResponse, type InterfaceIssue, type NonActivatedLink } from '@/lib/api'
import { StatCard } from '@/components/stat-card'
import { LinkStatusTimelines } from '@/components/link-status-timelines'

type TimeRange = '1h' | '6h' | '12h' | '24h' | '3d' | '7d'
type IssueFilter = 'packet_loss' | 'high_latency' | 'extended_loss' | 'drained' | 'no_data' | 'no_issues'

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse bg-muted rounded ${className || ''}`} />
}

function StatusPageSkeleton() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-6xl mx-auto px-4 sm:px-8 py-8">
        {/* Status indicator skeleton */}
        <div className="mb-8">
          <Skeleton className="h-[72px] rounded-lg" />
        </div>

        {/* Stats grid skeleton */}
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-x-8 gap-y-6 mb-8">
          {Array.from({ length: 10 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-24" />
          ))}
        </div>

        {/* Health cards skeleton */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          <Skeleton className="h-[180px] rounded-lg" />
          <Skeleton className="h-[180px] rounded-lg" />
        </div>

        {/* Utilization cards skeleton */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          <Skeleton className="h-[200px] rounded-lg" />
          <Skeleton className="h-[200px] rounded-lg" />
        </div>

        {/* Timeline section skeleton */}
        <div className="mb-8">
          <div className="flex items-center justify-between mb-4">
            <Skeleton className="h-6 w-40" />
            <Skeleton className="h-8 w-64" />
          </div>
          <Skeleton className="h-[300px] rounded-lg" />
        </div>
      </div>
    </div>
  )
}

function TimeRangeSelector({ value, onChange }: { value: TimeRange; onChange: (v: TimeRange) => void }) {
  const options: { value: TimeRange; label: string }[] = [
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '12h', label: '12h' },
    { value: '24h', label: '24h' },
    { value: '3d', label: '3d' },
    { value: '7d', label: '7d' },
  ]

  return (
    <div className="inline-flex rounded-lg border border-border bg-muted/30 p-0.5">
      {options.map((opt) => (
        <button
          key={opt.value}
          onClick={() => onChange(opt.value)}
          className={`px-3 py-1 text-sm rounded-md transition-colors ${
            value === opt.value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          {opt.label}
        </button>
      ))}
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

  // Check link health
  if (status.links.unhealthy > 0) {
    reasons.push(`${status.links.unhealthy} link${status.links.unhealthy > 1 ? 's' : ''} with critical issues`)
  }
  if (status.links.degraded > 0) {
    reasons.push(`${status.links.degraded} link${status.links.degraded > 1 ? 's' : ''} with degraded performance`)
  }

  // Check packet loss
  if (status.performance.avg_loss_percent >= 1.0) {
    reasons.push(`${status.performance.avg_loss_percent.toFixed(1)}% average packet loss`)
  } else if (status.performance.avg_loss_percent >= 0.1) {
    reasons.push(`${status.performance.avg_loss_percent.toFixed(2)}% packet loss detected`)
  }

  // Check for non-activated devices/links
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

function StatusIndicator({ statusData }: { statusData: StatusResponse }) {
  const status = statusData.status
  const reasons = getStatusReasons(statusData)
  const [, forceUpdate] = useState(0)

  // Update relative time every 10 seconds
  useEffect(() => {
    const interval = setInterval(() => forceUpdate(n => n + 1), 10000)
    return () => clearInterval(interval)
  }, [])

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

  return (
    <div className={`flex items-center gap-3 px-6 py-4 rounded-lg bg-card border border-border border-l-4 ${borderClassName}`}>
      <Icon className={`h-8 w-8 ${className}`} />
      <div className="flex-1">
        <div className={`text-lg font-medium ${className}`}>{label}</div>
        {reasons.length > 0 && (
          <div className="text-sm text-muted-foreground">{reasons.slice(0, 2).join(' · ')}</div>
        )}
      </div>
      <div className="text-xs text-muted-foreground/60">
        Updated {formatRelativeTime(statusData.timestamp)}
      </div>
    </div>
  )
}

function CountWithPopover({
  count,
  label,
  status,
  items,
  renderItem
}: {
  count: number
  label: string
  status: 'good' | 'warning' | 'bad'
  items?: { code: string; status: string }[]
  renderItem?: (item: { code: string; status: string }) => React.ReactNode
}) {
  const [isOpen, setIsOpen] = useState(false)
  const hasPopover = items && items.length > 0 && renderItem

  const statusColors = {
    good: 'bg-green-500',
    warning: 'bg-amber-500',
    bad: 'bg-red-500'
  }

  const content = (
    <div className="flex items-center gap-2">
      <div className={`w-2 h-2 rounded-full ${statusColors[status]}`} />
      <span className="font-medium tabular-nums">{count}</span>
      <span className="text-muted-foreground">{label}</span>
    </div>
  )

  if (!hasPopover) {
    return content
  }

  return (
    <div className="relative inline-block">
      <button
        className="text-left hover:underline underline-offset-2"
        onMouseEnter={() => setIsOpen(true)}
        onMouseLeave={() => setIsOpen(false)}
        onClick={() => setIsOpen(!isOpen)}
      >
        {content}
      </button>
      {isOpen && (
        <div
          className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 min-w-[200px] max-h-[300px] overflow-y-auto"
          onMouseEnter={() => setIsOpen(true)}
          onMouseLeave={() => setIsOpen(false)}
        >
          <div className="space-y-1.5 text-xs">
            {items!.map((item, idx) => (
              <div key={`${item.code}-${idx}`}>{renderItem!(item)}</div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function IssueSummaryCard({
  alerts,
  interfaceIssuesCount,
  disabledLinksCount
}: {
  alerts: StatusResponse['alerts']
  interfaceIssuesCount: number
  disabledLinksCount: number
}) {
  const disabledDevices = (alerts?.devices || []).filter(d =>
    d.status === 'soft-drained' || d.status === 'hard-drained'
  )

  const scrollToInterfaceIssues = () => {
    document.getElementById('interface-issues')?.scrollIntoView({ behavior: 'smooth' })
  }

  const scrollToDisabledDevices = () => {
    document.getElementById('disabled-devices')?.scrollIntoView({ behavior: 'smooth' })
  }

  const scrollToDisabledLinks = () => {
    document.getElementById('disabled-links')?.scrollIntoView({ behavior: 'smooth' })
  }

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Issue Summary</h3>
        <span className="text-xs text-muted-foreground ml-auto">Current</span>
      </div>
      <div className="space-y-2 text-sm">
        {disabledDevices.length > 0 ? (
          <button
            onClick={scrollToDisabledDevices}
            className="flex items-center gap-2 text-left hover:underline underline-offset-2"
          >
            <div className="w-2 h-2 rounded-full bg-amber-500" />
            <span className="font-medium tabular-nums">{disabledDevices.length}</span>
            <span className="text-muted-foreground">{disabledDevices.length === 1 ? 'disabled device' : 'disabled devices'}</span>
          </button>
        ) : (
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-green-500" />
            <span className="font-medium tabular-nums">0</span>
            <span className="text-muted-foreground">disabled devices</span>
          </div>
        )}
        {disabledLinksCount > 0 ? (
          <button
            onClick={scrollToDisabledLinks}
            className="flex items-center gap-2 text-left hover:underline underline-offset-2"
          >
            <div className="w-2 h-2 rounded-full bg-amber-500" />
            <span className="font-medium tabular-nums">{disabledLinksCount}</span>
            <span className="text-muted-foreground">{disabledLinksCount === 1 ? 'disabled link' : 'disabled links'}</span>
          </button>
        ) : (
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-green-500" />
            <span className="font-medium tabular-nums">0</span>
            <span className="text-muted-foreground">disabled links</span>
          </div>
        )}
        {interfaceIssuesCount > 0 ? (
          <button
            onClick={scrollToInterfaceIssues}
            className="flex items-center gap-2 text-left hover:underline underline-offset-2"
          >
            <div className={`w-2 h-2 rounded-full ${interfaceIssuesCount <= 3 ? 'bg-amber-500' : 'bg-red-500'}`} />
            <span className="font-medium tabular-nums">{interfaceIssuesCount}</span>
            <span className="text-muted-foreground">{interfaceIssuesCount === 1 ? 'interface issue' : 'interface issues'}</span>
          </button>
        ) : (
          <CountWithPopover
            count={0}
            label="interface issues"
            status="good"
          />
        )}
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

interface DisabledLinkRow {
  pk: string
  code: string
  link_type: string
  side_a_metro: string
  side_z_metro: string
  reason: string // "drained" or "packet loss"
}

function DisabledLinksTable({
  drainedLinks,
  packetLossLinks
}: {
  drainedLinks: NonActivatedLink[] | null
  packetLossLinks: DisabledLinkRow[]
}) {
  // Merge drained links and packet loss links, deduping by code
  const allLinks = useMemo(() => {
    const linkMap = new Map<string, DisabledLinkRow>()

    // Add drained links first with specific drain type
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

    // Add packet loss links (won't overwrite if already drained)
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
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{link.side_a_metro} — {link.side_z_metro}</td>
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

type HealthFilter = 'healthy' | 'degraded' | 'unhealthy' | 'disabled'

function HealthFilterItem({
  color,
  label,
  count,
  description,
  selected,
  onClick
}: {
  color: string
  label: string
  count: number
  description: string
  selected: boolean
  onClick: () => void
}) {
  const [showTooltip, setShowTooltip] = useState(false)

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
            {description}
          </div>
        )}
      </div>
      <span className={`font-medium tabular-nums transition-colors ${!selected ? 'text-muted-foreground/50' : ''}`}>{count}</span>
    </button>
  )
}

function LinkHealthFilterCard({
  links,
  selected,
  onChange
}: {
  links: StatusResponse['links']
  selected: HealthFilter[]
  onChange: (filters: HealthFilter[]) => void
}) {
  const toggleFilter = (filter: HealthFilter) => {
    if (selected.includes(filter)) {
      // Don't allow deselecting the last filter
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
        <span className="text-xs text-muted-foreground">(Last Hour)</span>
        <button
          onClick={() => onChange(['healthy', 'degraded', 'unhealthy', 'disabled'])}
          className={`text-xs ml-auto px-1.5 py-0.5 rounded transition-colors ${
            allSelected ? 'text-muted-foreground' : 'text-primary hover:underline'
          }`}
        >
          {allSelected ? 'All selected' : 'Select all'}
        </button>
      </div>

      {/* Health bar */}
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

      {/* Legend */}
      <div className="space-y-0.5 text-sm">
        <HealthFilterItem
          color="bg-green-500"
          label="Healthy"
          count={links.healthy}
          description="No active issues detected."
          selected={selected.includes('healthy')}
          onClick={() => toggleFilter('healthy')}
        />
        <HealthFilterItem
          color="bg-amber-500"
          label="Degraded"
          count={links.degraded}
          description="Moderate packet loss (1% - 10%), or latency SLA breach."
          selected={selected.includes('degraded')}
          onClick={() => toggleFilter('degraded')}
        />
        <HealthFilterItem
          color="bg-red-500"
          label="Unhealthy"
          count={links.unhealthy}
          description="Severe packet loss (≥ 10%), or missing telemetry (link dark)."
          selected={selected.includes('unhealthy')}
          onClick={() => toggleFilter('unhealthy')}
        />
        <HealthFilterItem
          color="bg-gray-500 dark:bg-gray-700"
          label="Disabled"
          count={links.disabled || 0}
          description="Drained (soft, hard, or ISIS delay override), or extended packet loss (100% for 2+ hours)."
          selected={selected.includes('disabled')}
          onClick={() => toggleFilter('disabled')}
        />
      </div>
    </div>
  )
}

const timeRangeLabels: Record<TimeRange, string> = {
  '1h': 'Last Hour',
  '6h': 'Last 6 Hours',
  '12h': 'Last 12 Hours',
  '24h': 'Last 24 Hours',
  '3d': 'Last 3 Days',
  '7d': 'Last 7 Days',
}

function LinkIssuesFilterCard({
  counts,
  selected,
  onChange,
  timeRange
}: {
  counts: IssueCounts
  selected: IssueFilter[]
  onChange: (filters: IssueFilter[]) => void
  timeRange: TimeRange
}) {
  const allFilters: IssueFilter[] = ['packet_loss', 'high_latency', 'extended_loss', 'drained', 'no_data', 'no_issues']

  const toggleFilter = (filter: IssueFilter) => {
    if (selected.includes(filter)) {
      // Don't allow deselecting the last filter
      if (selected.length > 1) {
        onChange(selected.filter(f => f !== filter))
      }
    } else {
      onChange([...selected, filter])
    }
  }

  const allSelected = selected.length === allFilters.length

  const items: { filter: IssueFilter; label: string; color: string; description: string }[] = [
    { filter: 'packet_loss', label: 'Packet Loss', color: 'bg-purple-500', description: 'Link experiencing measurable packet loss (≥ 1%).' },
    { filter: 'high_latency', label: 'High Latency', color: 'bg-blue-500', description: 'Link latency exceeds committed RTT.' },
    { filter: 'extended_loss', label: 'Extended Loss', color: 'bg-orange-500', description: 'Link has 100% packet loss for 2+ hours.' },
    { filter: 'drained', label: 'Drained', color: 'bg-slate-500 dark:bg-slate-600', description: 'Link is soft-drained, hard-drained, or has ISIS delay override.' },
    { filter: 'no_data', label: 'No Data', color: 'bg-pink-500', description: 'No telemetry received for this link.' },
    { filter: 'no_issues', label: 'No Issues', color: 'bg-cyan-500', description: 'Link with no detected issues in the time range.' },
  ]

  // Calculate percentages for the bar (total includes no_issues)
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

      {/* Issues bar */}
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

      {/* Issues list */}
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
          <span className="text-xs text-muted-foreground ml-auto">p95 · Last 24h</span>
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
        <span className="text-xs text-muted-foreground ml-auto">p95 · Last 24h</span>
      </div>
      <div className="space-y-2">
        {links.slice(0, 5).map((link) => {
          const maxUtil = Math.max(link.utilization_in, link.utilization_out)
          const peakBps = Math.max(link.in_bps, link.out_bps)
          return (
            <div key={link.pk} className="flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <Link to={`/dz/links/${link.pk}`} className="font-mono text-xs truncate hover:underline" title={link.code}>{link.code}</Link>
                <div className="text-[10px] text-muted-foreground">{link.side_a_metro} — {link.side_z_metro}</div>
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

function InterfaceIssuesTable({ issues }: { issues: InterfaceIssue[] | null }) {
  if (!issues || issues.length === 0) return null

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <Cpu className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Interface Issues</h3>
        <span className="text-sm text-muted-foreground ml-auto">Last 24 hours</span>
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
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${totalErrors > 0 ? 'text-red-600 dark:text-red-400' : ''}`}>
                    {totalErrors > 0 ? totalErrors.toLocaleString() : '—'}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${totalDiscards > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                    {totalDiscards > 0 ? totalDiscards.toLocaleString() : '—'}
                  </td>
                  <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${issue.carrier_transitions > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                    {issue.carrier_transitions > 0 ? issue.carrier_transitions.toLocaleString() : '—'}
                  </td>
                  <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                    {issue.first_seen ? formatTimeAgo(issue.first_seen) : '—'}
                  </td>
                  <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                    {issue.last_seen ? formatTimeAgo(issue.last_seen) : '—'}
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

export function StatusPage() {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [issueFilters, setIssueFilters] = useState<IssueFilter[]>(['packet_loss', 'high_latency', 'extended_loss', 'drained', 'no_data'])
  const [healthFilters, setHealthFilters] = useState<HealthFilter[]>(['healthy', 'degraded', 'unhealthy', 'disabled'])
  const buckets = useBucketCount()

  const { data: status, isLoading, error } = useQuery({
    queryKey: ['status'],
    queryFn: fetchStatus,
    refetchInterval: 30_000,
    staleTime: 15_000,
  })

  const { data: linkHistory } = useQuery({
    queryKey: ['link-history', timeRange, buckets],
    queryFn: () => fetchLinkHistory(timeRange, buckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  const issueCounts = useMemo((): IssueCounts => {
    if (!linkHistory?.links) {
      return { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    }

    const counts = { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    const seenLinks = new Set<string>()

    for (const link of linkHistory.links) {
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

    // Calculate no_issues as total links minus links with issues
    const totalLinks = status?.links.total || 0
    counts.no_issues = Math.max(0, totalLinks - counts.total)

    return counts
  }, [linkHistory, status])

  // Extract links currently disabled due to packet loss (last 2h of 100% loss)
  const packetLossDisabledLinks = useMemo((): DisabledLinkRow[] => {
    if (!linkHistory?.links || !status) return []

    // Get codes of drained links from alerts
    const drainedCodes = new Set(
      (status.alerts?.links || [])
        .filter(l => l.status === 'soft-drained' || l.status === 'hard-drained')
        .map(l => l.code)
    )

    // Calculate how many buckets = 2 hours
    const bucketsFor2Hours = Math.ceil(120 / (linkHistory.bucket_minutes || 20))

    // Check if a link is currently disabled (most recent buckets are all "disabled")
    const isCurrentlyDisabledByPacketLoss = (hours: { status: string }[]): boolean => {
      if (!hours || hours.length < bucketsFor2Hours) return false
      // Check the most recent buckets (end of array)
      const recentBuckets = hours.slice(-bucketsFor2Hours)
      return recentBuckets.every(h => h.status === 'disabled')
    }

    // Get links currently disabled by packet loss (not drained)
    return linkHistory.links
      .filter(link => !drainedCodes.has(link.code) && isCurrentlyDisabledByPacketLoss(link.hours))
      .map(link => ({
        pk: link.pk,
        code: link.code,
        link_type: link.link_type,
        side_a_metro: link.side_a_metro,
        side_z_metro: link.side_z_metro,
        reason: 'extended packet loss',
      }))
  }, [linkHistory, status])

  // Count all disabled links (drained + packet loss)
  const disabledLinksCount = useMemo(() => {
    const drainedCount = (status?.alerts?.links || []).filter(
      l => l.status === 'soft-drained' || l.status === 'hard-drained'
    ).length
    return drainedCount + packetLossDisabledLinks.length
  }, [status, packetLossDisabledLinks])

  // Only show skeleton after delay to avoid flash for fast loads
  const showSkeleton = useDelayedLoading(isLoading)

  if (isLoading && showSkeleton) {
    return <StatusPageSkeleton />
  }

  // Still loading but skeleton delay hasn't passed - show nothing briefly
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
          <StatCard label="Capacity" value={status.network.wan_bandwidth_bps} format="bandwidth" />
          <StatCard label="User Inbound" value={status.network.user_inbound_bps} format="bandwidth" />
        </div>

        {/* Summary Cards Row */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          <IssueSummaryCard
            alerts={status.alerts}
            interfaceIssuesCount={status.interfaces.issues?.length || 0}
            disabledLinksCount={disabledLinksCount}
          />
          <div /> {/* Placeholder for balance */}
        </div>

        {/* Utilization Toplists */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          <TopLinkUtilization links={status.links.top_util_links} />
          <TopDeviceUtilization devices={status.top_device_util} />
        </div>

        {/* Link Status Timeline */}
        <div id="link-status-history" className="mb-8">
          <div className="flex items-center justify-between mb-4 flex-wrap gap-3">
            <h2 className="text-lg font-semibold">Link Status History</h2>
            <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
          </div>

          {/* Filter Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
            <LinkHealthFilterCard
              links={status.links}
              selected={healthFilters}
              onChange={setHealthFilters}
            />
            <LinkIssuesFilterCard
              counts={issueCounts}
              selected={issueFilters}
              onChange={setIssueFilters}
              timeRange={timeRange}
            />
          </div>

          <LinkStatusTimelines timeRange={timeRange} issueFilters={issueFilters} healthFilters={healthFilters} />
        </div>

        {/* Disabled Devices */}
        <div className="mb-8">
          <DisabledDevicesTable devices={status.alerts?.devices} />
        </div>

        {/* Disabled Links */}
        <div className="mb-8">
          <DisabledLinksTable
            drainedLinks={status.alerts?.links}
            packetLossLinks={packetLossDisabledLinks}
          />
        </div>

        {/* Interface Issues */}
        <div id="interface-issues" className="mb-8">
          <InterfaceIssuesTable issues={status.interfaces.issues} />
        </div>

        {/* Methodology link */}
        <div className="text-center text-sm text-muted-foreground pb-4">
          <Link to="/status/methodology" className="hover:text-foreground hover:underline">
            How is status calculated?
          </Link>
        </div>
      </div>
    </div>
  )
}
