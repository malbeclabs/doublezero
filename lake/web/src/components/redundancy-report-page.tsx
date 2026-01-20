import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Loader2, Shield, AlertTriangle, AlertCircle, Info, ExternalLink, ChevronDown, ChevronRight, Activity, Wifi } from 'lucide-react'
import { fetchRedundancyReport, fetchLinkHealth } from '@/lib/api'
import type { RedundancyIssue, TopologyLinkHealth } from '@/lib/api'

// Severity colors
const SEVERITY_COLORS = {
  critical: {
    bg: 'bg-red-100 dark:bg-red-900/30',
    text: 'text-red-700 dark:text-red-400',
    border: 'border-red-200 dark:border-red-800',
    icon: AlertCircle,
  },
  warning: {
    bg: 'bg-yellow-100 dark:bg-yellow-900/30',
    text: 'text-yellow-700 dark:text-yellow-400',
    border: 'border-yellow-200 dark:border-yellow-800',
    icon: AlertTriangle,
  },
  info: {
    bg: 'bg-blue-100 dark:bg-blue-900/30',
    text: 'text-blue-700 dark:text-blue-400',
    border: 'border-blue-200 dark:border-blue-800',
    icon: Info,
  },
}

// Issue type labels
const ISSUE_TYPE_LABELS: Record<string, string> = {
  leaf_device: 'Leaf Device',
  critical_link: 'Critical Link',
  single_exit_metro: 'Single-Exit Metro',
  no_backup_device: 'No Backup Device',
}

function SummaryCard({
  label,
  value,
  color,
  icon: Icon,
}: {
  label: string
  value: number
  color: 'critical' | 'warning' | 'info' | 'neutral'
  icon: React.ComponentType<{ className?: string }>
}) {
  const colorClasses = {
    critical: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400',
    warning: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400',
    info: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400',
    neutral: 'bg-muted text-foreground',
  }

  return (
    <div className={`rounded-lg p-4 ${colorClasses[color]}`}>
      <div className="flex items-center gap-2 mb-1">
        <Icon className="h-4 w-4" />
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="text-2xl font-bold">{value}</div>
    </div>
  )
}

function IssueRow({
  issue,
  isExpanded,
  onToggle,
}: {
  issue: RedundancyIssue
  isExpanded: boolean
  onToggle: () => void
}) {
  const severity = SEVERITY_COLORS[issue.severity]
  const Icon = severity.icon

  const getEntityLink = () => {
    if (issue.entityType === 'device') {
      return `/dz/devices/${issue.entityPK}`
    }
    if (issue.entityType === 'metro') {
      return `/dz/metros/${issue.entityPK}`
    }
    if (issue.entityType === 'link') {
      // For links, link to source device
      return `/dz/devices/${issue.entityPK}`
    }
    return null
  }

  const entityLink = getEntityLink()

  return (
    <div className={`border rounded-lg ${severity.border} overflow-hidden`}>
      <button
        onClick={onToggle}
        className={`w-full flex items-center gap-3 p-3 text-left hover:bg-muted/50 transition-colors`}
      >
        <div className={`p-1.5 rounded ${severity.bg}`}>
          <Icon className={`h-4 w-4 ${severity.text}`} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-medium">
              {issue.entityType === 'link' ? (
                <>
                  {issue.entityCode} <span className="text-muted-foreground">↔</span> {issue.targetCode}
                </>
              ) : (
                issue.entityCode
              )}
            </span>
            <span className={`text-xs px-2 py-0.5 rounded ${severity.bg} ${severity.text}`}>
              {ISSUE_TYPE_LABELS[issue.type] || issue.type}
            </span>
          </div>
          <div className="text-sm text-muted-foreground truncate">
            {issue.description}
          </div>
        </div>
        {isExpanded ? (
          <ChevronDown className="h-4 w-4 text-muted-foreground flex-shrink-0" />
        ) : (
          <ChevronRight className="h-4 w-4 text-muted-foreground flex-shrink-0" />
        )}
      </button>

      {isExpanded && (
        <div className="px-3 pb-3 border-t border-border bg-muted/30">
          <div className="pt-3 space-y-3">
            <div>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Impact</div>
              <div className="text-sm">{issue.impact}</div>
            </div>

            {issue.metroCode && (
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Metro</div>
                <Link
                  to={`/dz/metros/${issue.metroPK}`}
                  className="text-sm hover:underline text-primary inline-flex items-center gap-1"
                >
                  {issue.metroCode}
                  <ExternalLink className="h-3 w-3" />
                </Link>
              </div>
            )}

            {entityLink && (
              <div className="pt-2">
                <Link
                  to={entityLink}
                  className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
                >
                  View {issue.entityType} details
                  <ExternalLink className="h-3 w-3" />
                </Link>
                {issue.entityType === 'link' && issue.targetPK && (
                  <>
                    <span className="mx-2 text-muted-foreground">|</span>
                    <Link
                      to={`/dz/devices/${issue.targetPK}`}
                      className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
                    >
                      View {issue.targetCode}
                      <ExternalLink className="h-3 w-3" />
                    </Link>
                  </>
                )}
              </div>
            )}

            {/* Quick actions */}
            {issue.entityType === 'device' && (
              <div className="pt-2 flex gap-2">
                <Link
                  to={`/topology/graph?type=device&id=${issue.entityPK}`}
                  className="text-xs px-2 py-1 bg-muted rounded hover:bg-muted/80 transition-colors"
                >
                  Show in Graph
                </Link>
                <Link
                  to={`/topology/map?type=device&id=${issue.entityPK}`}
                  className="text-xs px-2 py-1 bg-muted rounded hover:bg-muted/80 transition-colors"
                >
                  Show in Map
                </Link>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function formatMicroseconds(us: number): string {
  if (us >= 1000) {
    return `${(us / 1000).toFixed(1)}ms`
  }
  return `${us.toFixed(0)}µs`
}

function DegradedLinkRow({
  link,
  isExpanded,
  onToggle,
}: {
  link: TopologyLinkHealth
  isExpanded: boolean
  onToggle: () => void
}) {
  const isCritical = link.sla_status === 'critical'
  const severity = isCritical ? SEVERITY_COLORS.critical : SEVERITY_COLORS.warning
  const Icon = severity.icon

  const committedRttUs = link.committed_rtt_ns / 1000

  return (
    <div className={`border rounded-lg ${severity.border} overflow-hidden`}>
      <button
        onClick={onToggle}
        className={`w-full flex items-center gap-3 p-3 text-left hover:bg-muted/50 transition-colors`}
      >
        <div className={`p-1.5 rounded ${severity.bg}`}>
          <Icon className={`h-4 w-4 ${severity.text}`} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-medium">
              {link.side_a_code} <span className="text-muted-foreground">↔</span> {link.side_z_code}
            </span>
            <span className={`text-xs px-2 py-0.5 rounded ${severity.bg} ${severity.text}`}>
              {isCritical ? 'SLA Critical' : 'SLA Warning'}
            </span>
          </div>
          <div className="text-sm text-muted-foreground truncate flex items-center gap-3">
            <span className="inline-flex items-center gap-1">
              <Activity className="h-3 w-3" />
              P95: {formatMicroseconds(link.p95_rtt_us)}
              {link.exceeds_commit && <span className="text-red-500">(exceeds {formatMicroseconds(committedRttUs)} commit)</span>}
            </span>
            {link.has_packet_loss && (
              <span className="inline-flex items-center gap-1 text-red-500">
                <Wifi className="h-3 w-3" />
                {link.loss_pct.toFixed(2)}% loss
              </span>
            )}
          </div>
        </div>
        {isExpanded ? (
          <ChevronDown className="h-4 w-4 text-muted-foreground flex-shrink-0" />
        ) : (
          <ChevronRight className="h-4 w-4 text-muted-foreground flex-shrink-0" />
        )}
      </button>

      {isExpanded && (
        <div className="px-3 pb-3 border-t border-border bg-muted/30">
          <div className="pt-3 space-y-3">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Average RTT</div>
                <div className="text-sm font-medium">{formatMicroseconds(link.avg_rtt_us)}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">P95 RTT</div>
                <div className="text-sm font-medium">{formatMicroseconds(link.p95_rtt_us)}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Committed RTT</div>
                <div className="text-sm font-medium">{formatMicroseconds(committedRttUs)}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">SLA Ratio</div>
                <div className="text-sm font-medium">{(link.sla_ratio * 100).toFixed(1)}%</div>
              </div>
            </div>

            {link.has_packet_loss && (
              <div>
                <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Packet Loss</div>
                <div className="text-sm font-medium text-red-500">{link.loss_pct.toFixed(2)}%</div>
              </div>
            )}

            <div className="pt-2 flex gap-2">
              <Link
                to={`/dz/devices/${link.side_a_pk}`}
                className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
              >
                View {link.side_a_code}
                <ExternalLink className="h-3 w-3" />
              </Link>
              <span className="text-muted-foreground">|</span>
              <Link
                to={`/dz/devices/${link.side_z_pk}`}
                className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
              >
                View {link.side_z_code}
                <ExternalLink className="h-3 w-3" />
              </Link>
            </div>

            <div className="pt-2 flex gap-2">
              <Link
                to={`/topology/graph?type=link&id=${link.link_pk}`}
                className="text-xs px-2 py-1 bg-muted rounded hover:bg-muted/80 transition-colors"
              >
                Show in Graph
              </Link>
              <Link
                to={`/topology/map?type=link&id=${link.link_pk}`}
                className="text-xs px-2 py-1 bg-muted rounded hover:bg-muted/80 transition-colors"
              >
                Show in Map
              </Link>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

type FilterType = 'all' | 'leaf_device' | 'critical_link' | 'single_exit_metro'
type FilterSeverity = 'all' | 'critical' | 'warning' | 'info'

export function RedundancyReportPage() {
  const [filterType, setFilterType] = useState<FilterType>('all')
  const [filterSeverity, setFilterSeverity] = useState<FilterSeverity>('all')
  const [expandedIssues, setExpandedIssues] = useState<Set<string>>(new Set())
  const [expandedDegradedLinks, setExpandedDegradedLinks] = useState<Set<string>>(new Set())

  const { data, isLoading, error } = useQuery({
    queryKey: ['redundancy-report'],
    queryFn: fetchRedundancyReport,
  })

  const { data: linkHealthData, isLoading: linkHealthLoading } = useQuery({
    queryKey: ['link-health'],
    queryFn: fetchLinkHealth,
  })

  const filteredIssues = useMemo(() => {
    if (!data?.issues) return []
    return data.issues.filter(issue => {
      if (filterType !== 'all' && issue.type !== filterType) return false
      if (filterSeverity !== 'all' && issue.severity !== filterSeverity) return false
      return true
    })
  }, [data?.issues, filterType, filterSeverity])

  const toggleIssue = (issueKey: string) => {
    setExpandedIssues(prev => {
      const next = new Set(prev)
      if (next.has(issueKey)) {
        next.delete(issueKey)
      } else {
        next.add(issueKey)
      }
      return next
    })
  }

  const getIssueKey = (issue: RedundancyIssue, index: number) => {
    return `${issue.type}-${issue.entityPK}-${issue.targetPK || ''}-${index}`
  }

  const degradedLinks = useMemo(() => {
    if (!linkHealthData?.links) return []
    return linkHealthData.links.filter(
      link => link.sla_status === 'critical' || link.sla_status === 'warning'
    )
  }, [linkHealthData?.links])

  const toggleDegradedLink = (linkPk: string) => {
    setExpandedDegradedLinks(prev => {
      const next = new Set(prev)
      if (next.has(linkPk)) {
        next.delete(linkPk)
      } else {
        next.add(linkPk)
      }
      return next
    })
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-96">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    )
  }

  if (error || data?.error) {
    return (
      <div className="flex flex-col items-center justify-center h-96 gap-4">
        <AlertCircle className="h-12 w-12 text-destructive" />
        <div className="text-lg font-medium">Failed to load redundancy report</div>
        <div className="text-sm text-muted-foreground">
          {error?.message || data?.error || 'An unknown error occurred'}
        </div>
      </div>
    )
  }

  const summary = data?.summary

  return (
    <div className="h-full overflow-auto p-6">
    <div className="max-w-5xl mx-auto">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center gap-3 mb-2">
          <Shield className="h-6 w-6 text-primary" />
          <h1 className="text-2xl font-semibold">Redundancy Report</h1>
        </div>
        <p className="text-muted-foreground">
          Analysis of single points of failure and redundancy gaps in the network topology.
        </p>
      </div>

      {/* Summary Cards */}
      {summary && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <SummaryCard
            label="Critical Issues"
            value={summary.criticalCount}
            color="critical"
            icon={AlertCircle}
          />
          <SummaryCard
            label="Warnings"
            value={summary.warningCount}
            color="warning"
            icon={AlertTriangle}
          />
          <SummaryCard
            label="Leaf Devices"
            value={summary.leafDevices}
            color="critical"
            icon={AlertCircle}
          />
          <SummaryCard
            label="Single-Exit Metros"
            value={summary.singleExitMetros}
            color="warning"
            icon={AlertTriangle}
          />
        </div>
      )}

      {/* Filters */}
      <div className="flex flex-wrap gap-4 mb-6 p-4 bg-muted/50 rounded-lg">
        <div>
          <label className="block text-xs text-muted-foreground uppercase tracking-wider mb-1">
            Issue Type
          </label>
          <select
            value={filterType}
            onChange={(e) => setFilterType(e.target.value as FilterType)}
            className="px-3 py-1.5 border border-border rounded-md bg-card text-sm"
          >
            <option value="all">All Types</option>
            <option value="leaf_device">Leaf Devices</option>
            <option value="critical_link">Critical Links</option>
            <option value="single_exit_metro">Single-Exit Metros</option>
          </select>
        </div>
        <div>
          <label className="block text-xs text-muted-foreground uppercase tracking-wider mb-1">
            Severity
          </label>
          <select
            value={filterSeverity}
            onChange={(e) => setFilterSeverity(e.target.value as FilterSeverity)}
            className="px-3 py-1.5 border border-border rounded-md bg-card text-sm"
          >
            <option value="all">All Severities</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="info">Info</option>
          </select>
        </div>
        <div className="flex items-end ml-auto">
          <span className="text-sm text-muted-foreground">
            {filteredIssues.length} of {data?.issues?.length || 0} issues
          </span>
        </div>
      </div>

      {/* Issues List */}
      <div className="space-y-3">
        {filteredIssues.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            {data?.issues?.length === 0 ? (
              <>
                <Shield className="h-12 w-12 mx-auto mb-4 text-green-500" />
                <div className="text-lg font-medium text-foreground">No redundancy issues found</div>
                <div className="text-sm mt-1">The network topology has good redundancy.</div>
              </>
            ) : (
              <>No issues match the current filters.</>
            )}
          </div>
        ) : (
          filteredIssues.map((issue, index) => {
            const key = getIssueKey(issue, index)
            return (
              <IssueRow
                key={key}
                issue={issue}
                isExpanded={expandedIssues.has(key)}
                onToggle={() => toggleIssue(key)}
              />
            )
          })
        )}
      </div>

      {/* Degraded Links Section */}
      <div className="mt-8">
        <div className="flex items-center gap-3 mb-4">
          <Activity className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">Latency Degradation</h2>
          {linkHealthLoading && (
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          )}
        </div>

        {linkHealthData && (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
            <SummaryCard
              label="Critical Links"
              value={linkHealthData.critical_count}
              color="critical"
              icon={AlertCircle}
            />
            <SummaryCard
              label="Warning Links"
              value={linkHealthData.warning_count}
              color="warning"
              icon={AlertTriangle}
            />
            <SummaryCard
              label="Healthy Links"
              value={linkHealthData.healthy_count}
              color="neutral"
              icon={Shield}
            />
            <SummaryCard
              label="Total Links"
              value={linkHealthData.total_links}
              color="neutral"
              icon={Activity}
            />
          </div>
        )}

        <div className="space-y-3">
          {degradedLinks.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground bg-muted/30 rounded-lg">
              {linkHealthLoading ? (
                <>Loading link health data...</>
              ) : (
                <>
                  <Shield className="h-10 w-10 mx-auto mb-3 text-green-500" />
                  <div className="text-base font-medium text-foreground">All links healthy</div>
                  <div className="text-sm mt-1">No SLA violations detected.</div>
                </>
              )}
            </div>
          ) : (
            degradedLinks.map(link => (
              <DegradedLinkRow
                key={link.link_pk}
                link={link}
                isExpanded={expandedDegradedLinks.has(link.link_pk)}
                onToggle={() => toggleDegradedLink(link.link_pk)}
              />
            ))
          )}
        </div>
      </div>

      {/* Footer links */}
      <div className="mt-8 pt-6 border-t border-border">
        <div className="text-sm text-muted-foreground mb-2">Related Tools</div>
        <div className="flex gap-4">
          <Link
            to="/topology/graph"
            className="text-sm text-primary hover:underline inline-flex items-center gap-1"
          >
            View Topology Graph
            <ExternalLink className="h-3 w-3" />
          </Link>
          <Link
            to="/topology/map"
            className="text-sm text-primary hover:underline inline-flex items-center gap-1"
          >
            View Topology Map
            <ExternalLink className="h-3 w-3" />
          </Link>
        </div>
      </div>
    </div>
    </div>
  )
}
