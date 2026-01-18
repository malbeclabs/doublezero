import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { useDelayedLoading } from '@/hooks/use-delayed-loading'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { CheckCircle2, AlertTriangle, XCircle, ArrowUpDown, Cpu, ChevronDown } from 'lucide-react'
import { fetchStatus, fetchLinkHistory, fetchDeviceHistory, type StatusResponse, type InterfaceIssue, type NonActivatedLink, type LinkHistory, type DeviceHistory } from '@/lib/api'
import { StatCard } from '@/components/stat-card'
import { LinkStatusTimelines } from '@/components/link-status-timelines'
import { DeviceStatusTimelines } from '@/components/device-status-timelines'

type TimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'
type FilterTimeRange = '3h' | '6h' | '12h' | '24h' | '3d' | '7d'
type IssueFilter = 'packet_loss' | 'high_latency' | 'extended_loss' | 'drained' | 'no_data' | 'no_issues'
type DeviceIssueFilter = 'interface_errors' | 'carrier_transitions' | 'drained' | 'no_issues'
type HealthFilter = 'healthy' | 'degraded' | 'unhealthy' | 'disabled'

const filterTimeRangeLabels: Record<FilterTimeRange, string> = {
  '3h': 'Last 3 Hours',
  '6h': 'Last 6 Hours',
  '12h': 'Last 12 Hours',
  '24h': 'Last 24 Hours',
  '3d': 'Last 3 Days',
  '7d': 'Last 7 Days',
}

function FilterTimeRangeSelector({
  value,
  onChange,
}: {
  value: FilterTimeRange
  onChange: (value: FilterTimeRange) => void
}) {
  const [isOpen, setIsOpen] = useState(false)

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-0.5 transition-colors"
      >
        ({filterTimeRangeLabels[value]})
        <ChevronDown className="h-3 w-3" />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute left-0 top-full mt-1 z-50 bg-popover border border-border rounded-md shadow-lg py-1 min-w-[120px]">
            {(Object.keys(filterTimeRangeLabels) as FilterTimeRange[]).map((range) => (
              <button
                key={range}
                onClick={() => {
                  onChange(range)
                  setIsOpen(false)
                }}
                className={`w-full px-3 py-1.5 text-left text-xs transition-colors ${
                  value === range
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
                }`}
              >
                {filterTimeRangeLabels[range]}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
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

function StatusIndicator({ statusData }: { statusData: StatusResponse }) {
  const status = statusData.status
  const reasons = getStatusReasons(statusData)
  const [, forceUpdate] = useState(0)

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

function TabNavigation({ activeTab }: { activeTab: 'links' | 'devices' }) {
  const navigate = useNavigate()

  return (
    <div className="flex gap-1 border-b border-border mb-6">
      <button
        onClick={() => navigate('/status/links')}
        className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
          activeTab === 'links'
            ? 'border-primary text-foreground'
            : 'border-transparent text-muted-foreground hover:text-foreground'
        }`}
      >
        Links
      </button>
      <button
        onClick={() => navigate('/status/devices')}
        className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
          activeTab === 'devices'
            ? 'border-primary text-foreground'
            : 'border-transparent text-muted-foreground hover:text-foreground'
        }`}
      >
        Devices
      </button>
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
  filterTimeRange,
  onFilterTimeRangeChange,
  issuesByHealth,
}: {
  links: { healthy: number; degraded: number; unhealthy: number; disabled: number; total: number }
  selected: HealthFilter[]
  onChange: (filters: HealthFilter[]) => void
  filterTimeRange: FilterTimeRange
  onFilterTimeRangeChange: (range: FilterTimeRange) => void
  issuesByHealth?: IssuesByHealth
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
        <FilterTimeRangeSelector value={filterTimeRange} onChange={onFilterTimeRangeChange} />
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
  filterTimeRange,
  onFilterTimeRangeChange,
  healthByIssue,
}: {
  counts: IssueCounts
  selected: IssueFilter[]
  onChange: (filters: IssueFilter[]) => void
  filterTimeRange: FilterTimeRange
  onFilterTimeRangeChange: (range: FilterTimeRange) => void
  healthByIssue?: HealthByIssue
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
        <FilterTimeRangeSelector value={filterTimeRange} onChange={onFilterTimeRangeChange} />
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
function LinksContent({ status, linkHistory }: { status: StatusResponse; linkHistory: any }) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [filterTimeRange, setFilterTimeRange] = useState<FilterTimeRange>('12h')
  const [issueFilters, setIssueFilters] = useState<IssueFilter[]>(['packet_loss', 'high_latency', 'extended_loss', 'drained'])
  const [healthFilters, setHealthFilters] = useState<HealthFilter[]>(['healthy', 'degraded', 'unhealthy', 'disabled'])

  // Bucket count based on filter time range
  const filterBuckets = (() => {
    switch (filterTimeRange) {
      case '3h': return 36
      case '6h': return 36
      case '12h': return 48
      case '24h': return 72
      case '3d': return 72
      case '7d': return 84
      default: return 72
    }
  })()

  // Fetch link history for the filter time range (used for health and issue counts)
  const { data: filterLinkHistory } = useQuery({
    queryKey: ['link-history', filterTimeRange, filterBuckets],
    queryFn: () => fetchLinkHistory(filterTimeRange, filterBuckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

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
    if (!filterLinkHistory?.links) {
      return { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }
    }

    const counts = { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }

    for (const link of filterLinkHistory.links) {
      counts.total++
      const status = getEffectiveHealth(link)
      if (status === 'healthy') counts.healthy++
      else if (status === 'degraded') counts.degraded++
      else if (status === 'unhealthy' || status === 'no_data') counts.unhealthy++ // no_data maps to unhealthy
      else if (status === 'disabled') counts.disabled++
    }

    return counts
  }, [filterLinkHistory])

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

    if (!filterLinkHistory?.links) return result

    for (const link of filterLinkHistory.links) {
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
  }, [filterLinkHistory])

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

    if (!filterLinkHistory?.links) return result

    for (const link of filterLinkHistory.links) {
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
  }, [filterLinkHistory])

  // Issue counts from filter time range
  const issueCounts = useMemo((): IssueCounts => {
    if (!filterLinkHistory?.links) {
      return { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    }

    const counts = { packet_loss: 0, high_latency: 0, extended_loss: 0, drained: 0, no_data: 0, no_issues: 0, total: 0 }
    const seenLinks = new Set<string>()

    for (const link of filterLinkHistory.links) {
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
  }, [filterLinkHistory, healthCounts])

  // Get set of link codes with issues in the filter time range (for filtering history table)
  const linksWithIssues = useMemo(() => {
    if (!filterLinkHistory?.links) return new Map<string, string[]>()
    const map = new Map<string, string[]>()
    for (const link of filterLinkHistory.links) {
      if (link.issue_reasons?.length > 0) {
        map.set(link.code, link.issue_reasons)
      }
    }
    return map
  }, [filterLinkHistory])

  // Get health status for each link from the filter time range (for filtering history table)
  const linksWithHealth = useMemo(() => {
    if (!filterLinkHistory?.links) return new Map<string, string>()
    const map = new Map<string, string>()
    for (const link of filterLinkHistory.links) {
      map.set(link.code, getEffectiveHealth(link))
    }
    return map
  }, [filterLinkHistory])

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
          filterTimeRange={filterTimeRange}
          onFilterTimeRangeChange={setFilterTimeRange}
          issuesByHealth={issuesByHealth}
        />
        <LinkIssuesFilterCard
          counts={issueCounts}
          selected={issueFilters}
          onChange={setIssueFilters}
          filterTimeRange={filterTimeRange}
          onFilterTimeRangeChange={setFilterTimeRange}
          healthByIssue={healthByIssue}
        />
      </div>

      {/* Link Status History */}
      <div className="mb-8">
        <LinkStatusTimelines timeRange={timeRange} onTimeRangeChange={setTimeRange} issueFilters={issueFilters} healthFilters={healthFilters} linksWithIssues={linksWithIssues} linksWithHealth={linksWithHealth} />
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
  carrier_transitions: number
  drained: number
  no_issues: number
  total: number
}

// Device issues breakdown per health category
interface DeviceIssuesByHealth {
  healthy: { interface_errors: number; carrier_transitions: number; drained: number }
  degraded: { interface_errors: number; carrier_transitions: number; drained: number }
  unhealthy: { interface_errors: number; carrier_transitions: number; drained: number }
  disabled: { interface_errors: number; carrier_transitions: number; drained: number }
}

// Device health breakdown per issue type
interface DeviceHealthByIssue {
  interface_errors: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  carrier_transitions: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  drained: { healthy: number; degraded: number; unhealthy: number; disabled: number }
  no_issues: { healthy: number; degraded: number; unhealthy: number; disabled: number }
}

function DeviceHealthFilterCard({
  devices,
  selected,
  onChange,
  filterTimeRange,
  onFilterTimeRangeChange,
  issuesByHealth,
}: {
  devices: { healthy: number; degraded: number; unhealthy: number; disabled: number; total: number }
  selected: HealthFilter[]
  onChange: (filters: HealthFilter[]) => void
  filterTimeRange: FilterTimeRange
  onFilterTimeRangeChange: (range: FilterTimeRange) => void
  issuesByHealth?: DeviceIssuesByHealth
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
        <FilterTimeRangeSelector value={filterTimeRange} onChange={onFilterTimeRangeChange} />
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
  filterTimeRange,
  onFilterTimeRangeChange,
  healthByIssue,
}: {
  counts: DeviceIssueCounts
  selected: DeviceIssueFilter[]
  onChange: (filters: DeviceIssueFilter[]) => void
  filterTimeRange: FilterTimeRange
  onFilterTimeRangeChange: (range: FilterTimeRange) => void
  healthByIssue?: DeviceHealthByIssue
}) {
  const allFilters: DeviceIssueFilter[] = ['interface_errors', 'carrier_transitions', 'drained', 'no_issues']

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
  const carrierTransitionsPct = (counts.carrier_transitions / grandTotal) * 100
  const drainedPct = (counts.drained / grandTotal) * 100
  const noIssuesPct = (counts.no_issues / grandTotal) * 100

  const items: { filter: DeviceIssueFilter; label: string; color: string; description: string }[] = [
    { filter: 'interface_errors', label: 'Interface Errors', color: 'bg-fuchsia-500', description: 'Device experiencing interface errors or discards.' },
    { filter: 'carrier_transitions', label: 'Link Flapping', color: 'bg-orange-500', description: 'Device experiencing carrier state changes (link up/down).' },
    { filter: 'drained', label: 'Drained', color: 'bg-slate-500 dark:bg-slate-600', description: 'Device is soft-drained, hard-drained, or suspended.' },
    { filter: 'no_issues', label: 'No Issues', color: 'bg-cyan-500', description: 'Device with no detected issues in the time range.' },
  ]

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-3">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Device Issues</h3>
        <FilterTimeRangeSelector value={filterTimeRange} onChange={onFilterTimeRangeChange} />
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

// Devices tab content
function DevicesContent({ status }: { status: StatusResponse }) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [filterTimeRange, setFilterTimeRange] = useState<FilterTimeRange>('12h')
  const [issueFilters, setIssueFilters] = useState<DeviceIssueFilter[]>(['interface_errors', 'carrier_transitions', 'drained'])
  const [healthFilters, setHealthFilters] = useState<HealthFilter[]>(['healthy', 'degraded', 'unhealthy', 'disabled'])

  // Bucket count based on filter time range
  const filterBuckets = (() => {
    switch (filterTimeRange) {
      case '3h': return 36
      case '6h': return 36
      case '12h': return 48
      case '24h': return 72
      case '3d': return 72
      case '7d': return 84
      default: return 72
    }
  })()

  // Fetch device history for the filter time range
  const { data: filterDeviceHistory } = useQuery({
    queryKey: ['device-history', filterTimeRange, filterBuckets],
    queryFn: () => fetchDeviceHistory(filterTimeRange, filterBuckets),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

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
    if (!filterDeviceHistory?.devices) {
      return { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }
    }

    const counts = { healthy: 0, degraded: 0, unhealthy: 0, disabled: 0, total: 0 }

    for (const device of filterDeviceHistory.devices) {
      counts.total++
      const status = getEffectiveHealth(device)
      if (status === 'healthy') counts.healthy++
      else if (status === 'degraded') counts.degraded++
      else if (status === 'unhealthy' || status === 'no_data') counts.unhealthy++
      else if (status === 'disabled') counts.disabled++
    }

    return counts
  }, [filterDeviceHistory])

  // Calculate issue breakdown per health category
  const issuesByHealth = useMemo((): DeviceIssuesByHealth => {
    const emptyBreakdown = () => ({ interface_errors: 0, carrier_transitions: 0, drained: 0 })

    const result: DeviceIssuesByHealth = {
      healthy: emptyBreakdown(),
      degraded: emptyBreakdown(),
      unhealthy: emptyBreakdown(),
      disabled: emptyBreakdown(),
    }

    if (!filterDeviceHistory?.devices) return result

    for (const device of filterDeviceHistory.devices) {
      const rawHealth = getEffectiveHealth(device)
      const health = rawHealth === 'no_data' ? 'unhealthy' : rawHealth
      if (!(health in result)) continue

      const breakdown = result[health as keyof DeviceIssuesByHealth]
      const issues = device.issue_reasons ?? []

      if (issues.includes('interface_errors')) breakdown.interface_errors++
      if (issues.includes('carrier_transitions')) breakdown.carrier_transitions++
      if (issues.includes('drained')) breakdown.drained++
    }

    return result
  }, [filterDeviceHistory])

  // Calculate health breakdown per issue type
  const healthByIssue = useMemo((): DeviceHealthByIssue => {
    const emptyBreakdown = () => ({ healthy: 0, degraded: 0, unhealthy: 0, disabled: 0 })

    const result: DeviceHealthByIssue = {
      interface_errors: emptyBreakdown(),
      carrier_transitions: emptyBreakdown(),
      drained: emptyBreakdown(),
      no_issues: emptyBreakdown(),
    }

    if (!filterDeviceHistory?.devices) return result

    for (const device of filterDeviceHistory.devices) {
      const rawHealth = getEffectiveHealth(device)
      const health = (rawHealth === 'no_data' ? 'unhealthy' : rawHealth) as keyof IssueHealthBreakdown
      const issues = device.issue_reasons ?? []

      if (issues.length === 0) {
        result.no_issues[health]++
      } else {
        if (issues.includes('interface_errors')) result.interface_errors[health]++
        if (issues.includes('carrier_transitions')) result.carrier_transitions[health]++
        if (issues.includes('drained')) result.drained[health]++
      }
    }

    return result
  }, [filterDeviceHistory])

  // Issue counts from filter time range
  const issueCounts = useMemo((): DeviceIssueCounts => {
    if (!filterDeviceHistory?.devices) {
      return { interface_errors: 0, carrier_transitions: 0, drained: 0, no_issues: 0, total: 0 }
    }

    const counts = { interface_errors: 0, carrier_transitions: 0, drained: 0, no_issues: 0, total: 0 }
    const seenDevices = new Set<string>()

    for (const device of filterDeviceHistory.devices) {
      if (device.issue_reasons?.includes('interface_errors')) counts.interface_errors++
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
  }, [filterDeviceHistory, healthCounts])

  // Get set of device codes with issues in the filter time range
  const devicesWithIssues = useMemo(() => {
    if (!filterDeviceHistory?.devices) return new Map<string, string[]>()
    const map = new Map<string, string[]>()
    for (const device of filterDeviceHistory.devices) {
      if (device.issue_reasons?.length > 0) {
        map.set(device.code, device.issue_reasons)
      }
    }
    return map
  }, [filterDeviceHistory])

  // Get health status for each device from the filter time range
  const devicesWithHealth = useMemo(() => {
    if (!filterDeviceHistory?.devices) return new Map<string, string>()
    const map = new Map<string, string>()
    for (const device of filterDeviceHistory.devices) {
      map.set(device.code, getEffectiveHealth(device))
    }
    return map
  }, [filterDeviceHistory])

  return (
    <>
      {/* Device Health & Issues */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <DeviceHealthFilterCard
          devices={healthCounts}
          selected={healthFilters}
          onChange={setHealthFilters}
          filterTimeRange={filterTimeRange}
          onFilterTimeRangeChange={setFilterTimeRange}
          issuesByHealth={issuesByHealth}
        />
        <DeviceIssuesFilterCard
          counts={issueCounts}
          selected={issueFilters}
          onChange={setIssueFilters}
          filterTimeRange={filterTimeRange}
          onFilterTimeRangeChange={setFilterTimeRange}
          healthByIssue={healthByIssue}
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
        <InterfaceIssuesTable issues={status.interfaces.issues} />
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
  const activeTab = location.pathname.includes('/devices') ? 'devices' : 'links'

  // Redirect /status to /status/links
  useEffect(() => {
    if (location.pathname === '/status') {
      navigate('/status/links', { replace: true })
    }
  }, [location.pathname, navigate])

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

  const showSkeleton = useDelayedLoading(isLoading)

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
          <StatCard label="Capacity" value={status.network.wan_bandwidth_bps} format="bandwidth" />
          <StatCard label="User Inbound" value={status.network.user_inbound_bps} format="bandwidth" />
        </div>

        {/* Utilization Charts */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-8">
          <TopLinkUtilization links={status.links.top_util_links} />
          <TopDeviceUtilization devices={status.top_device_util} />
        </div>

        {/* Tab Navigation */}
        <TabNavigation activeTab={activeTab} />

        {/* Tab Content */}
        {activeTab === 'links' ? (
          <LinksContent status={status} linkHistory={linkHistory} />
        ) : (
          <DevicesContent status={status} />
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
