import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, AlertTriangle, XCircle, Clock, Activity, ArrowUpDown, Gauge, Cpu } from 'lucide-react'
import { fetchStatus, type StatusResponse, type LinkIssue, type LinkMetric, type InterfaceIssue, type NonActivatedDevice, type NonActivatedLink } from '@/lib/api'
import { StatCard } from '@/components/stat-card'

function formatLatency(us: number): string {
  if (us >= 1000) {
    return `${(us / 1000).toFixed(1)}ms`
  }
  return `${us.toFixed(0)}μs`
}

function formatBandwidth(bps: number): string {
  const gbps = bps / 1_000_000_000
  if (gbps >= 1000) {
    return `${(gbps / 1000).toFixed(1)} Tbps`
  }
  if (gbps >= 1) {
    return `${gbps.toFixed(1)} Gbps`
  }
  const mbps = bps / 1_000_000
  if (mbps >= 1) {
    return `${mbps.toFixed(0)} Mbps`
  }
  const kbps = bps / 1_000
  return `${kbps.toFixed(0)} Kbps`
}

function formatPercent(pct: number): string {
  if (pct < 0.01) return '<0.01%'
  if (pct < 1) return `${pct.toFixed(2)}%`
  return `${pct.toFixed(1)}%`
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

function StatusIndicator({ statusData }: { statusData: StatusResponse }) {
  const status = statusData.status
  const reasons = getStatusReasons(statusData)

  const config = {
    healthy: {
      icon: CheckCircle2,
      label: 'All Systems Operational',
      className: 'text-green-700 dark:text-green-400',
      borderClassName: 'border-l-green-500',
    },
    degraded: {
      icon: AlertTriangle,
      label: 'Degraded Performance',
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
        {reasons.length > 0 ? (
          <div className="text-sm text-muted-foreground">{reasons.slice(0, 2).join(' · ')}</div>
        ) : (
          <div className="text-sm text-muted-foreground">DoubleZero Network</div>
        )}
      </div>
    </div>
  )
}

function DataFreshnessCard({ system, timestamp }: { system: StatusResponse['system']; timestamp: string }) {
  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <Clock className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Data Freshness</h3>
      </div>
      <div className="space-y-3">
        {system.last_ingested && (
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Last Telemetry</span>
            <span className="text-sm">{formatTimeAgo(system.last_ingested)}</span>
          </div>
        )}
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">Last Refresh</span>
          <span className="text-sm">{formatTimeAgo(timestamp)}</span>
        </div>
      </div>
    </div>
  )
}

function InfrastructureAlertsCard({ alerts }: { alerts: StatusResponse['alerts'] }) {
  const hasAlerts = (alerts?.devices?.length || 0) > 0 || (alerts?.links?.length || 0) > 0

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Infrastructure Alerts</h3>
      </div>
      {!hasAlerts ? (
        <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
          <CheckCircle2 className="h-4 w-4" />
          All devices and links activated
        </div>
      ) : (
        <div className="text-sm">
          {(alerts?.devices?.length || 0) > 0 && (
            <div className="text-muted-foreground">
              {alerts!.devices.length} device{alerts!.devices.length > 1 ? 's' : ''} not activated
            </div>
          )}
          {(alerts?.links?.length || 0) > 0 && (
            <div className="text-muted-foreground">
              {alerts!.links.length} link{alerts!.links.length > 1 ? 's' : ''} not activated
            </div>
          )}
          <div className="text-xs text-muted-foreground mt-2">See details below</div>
        </div>
      )}
    </div>
  )
}

function NonActivatedDevicesTable({ devices }: { devices: NonActivatedDevice[] | null }) {
  if (!devices || devices.length === 0) return null

  const statusColors: Record<string, string> = {
    'soft-drained': 'text-amber-600 dark:text-amber-400',
    drained: 'text-gray-500',
    suspended: 'text-red-600 dark:text-red-400',
    pending: 'text-amber-600 dark:text-amber-400',
    deleted: 'text-gray-400',
    rejected: 'text-red-400',
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Non-Activated Devices</h3>
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
                  <span className="font-mono text-sm">{device.code}</span>
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

function NonActivatedLinksTable({ links }: { links: NonActivatedLink[] | null }) {
  if (!links || links.length === 0) return null

  const statusColors: Record<string, string> = {
    'soft-drained': 'text-amber-600 dark:text-amber-400',
    drained: 'text-gray-500',
    suspended: 'text-red-600 dark:text-red-400',
    pending: 'text-amber-600 dark:text-amber-400',
    deleted: 'text-gray-400',
    rejected: 'text-red-400',
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Non-Activated Links</h3>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Link</th>
              <th className="px-4 py-2 font-medium">Route</th>
              <th className="px-4 py-2 font-medium">Status</th>
              <th className="px-4 py-2 font-medium text-right">Since</th>
            </tr>
          </thead>
          <tbody>
            {links.map((link, idx) => (
              <tr key={`${link.code}-${idx}`} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">
                  <span className="font-mono text-sm">{link.code}</span>
                  <span className="text-xs text-muted-foreground ml-2">{link.link_type}</span>
                </td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{link.side_a_metro} — {link.side_z_metro}</td>
                <td className={`px-4 py-2.5 text-sm capitalize ${statusColors[link.status] || ''}`}>
                  {link.status.replace('-', ' ')}
                </td>
                <td className="px-4 py-2.5 text-sm tabular-nums text-right text-muted-foreground">
                  {formatTimeAgo(link.since)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function LinkHealthCard({ links }: { links: StatusResponse['links'] }) {
  const healthyPct = links.total > 0 ? (links.healthy / links.total) * 100 : 100
  const degradedPct = links.total > 0 ? (links.degraded / links.total) * 100 : 0
  const unhealthyPct = links.total > 0 ? (links.unhealthy / links.total) * 100 : 0

  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Link Health</h3>
        <span className="text-sm text-muted-foreground ml-auto">{links.total} WAN links</span>
      </div>

      {/* Health bar */}
      <div className="h-3 rounded-full overflow-hidden flex mb-4 bg-muted">
        {healthyPct > 0 && (
          <div
            className="bg-green-500 h-full transition-all"
            style={{ width: `${healthyPct}%` }}
          />
        )}
        {degradedPct > 0 && (
          <div
            className="bg-amber-500 h-full transition-all"
            style={{ width: `${degradedPct}%` }}
          />
        )}
        {unhealthyPct > 0 && (
          <div
            className="bg-red-500 h-full transition-all"
            style={{ width: `${unhealthyPct}%` }}
          />
        )}
      </div>

      {/* Legend */}
      <div className="space-y-1.5 text-sm">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <div className="h-2.5 w-2.5 rounded-full bg-green-500" />
            <span className="text-muted-foreground">Healthy</span>
          </div>
          <span className="font-medium tabular-nums">{links.healthy}</span>
        </div>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <div className="h-2.5 w-2.5 rounded-full bg-amber-500" />
            <span className="text-muted-foreground">Degraded</span>
          </div>
          <span className="font-medium tabular-nums">{links.degraded}</span>
        </div>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <div className="h-2.5 w-2.5 rounded-full bg-red-500" />
            <span className="text-muted-foreground">Unhealthy</span>
          </div>
          <span className="font-medium tabular-nums">{links.unhealthy}</span>
        </div>
      </div>
    </div>
  )
}

function PerformanceCard({ performance }: { performance: StatusResponse['performance'] }) {
  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <Activity className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Network Performance</h3>
        <span className="text-sm text-muted-foreground ml-auto">Last 3 hours</span>
      </div>
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatLatency(performance.avg_latency_us)}</div>
          <div className="text-sm text-muted-foreground">Avg Latency</div>
        </div>
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatLatency(performance.p95_latency_us)}</div>
          <div className="text-sm text-muted-foreground">P95 Latency</div>
        </div>
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatPercent(performance.avg_loss_percent)}</div>
          <div className="text-sm text-muted-foreground">Packet Loss</div>
        </div>
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatLatency(performance.avg_jitter_us)}</div>
          <div className="text-sm text-muted-foreground">Avg Jitter</div>
        </div>
      </div>
    </div>
  )
}

function ThroughputCard({ performance }: { performance: StatusResponse['performance'] }) {
  return (
    <div className="border border-border rounded-lg p-4">
      <div className="flex items-center gap-2 mb-4">
        <Gauge className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Current Throughput</h3>
        <span className="text-sm text-muted-foreground ml-auto">Last 5 min</span>
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatBandwidth(performance.total_in_bps)}</div>
          <div className="text-sm text-muted-foreground">Inbound</div>
        </div>
        <div>
          <div className="text-2xl font-medium tabular-nums">{formatBandwidth(performance.total_out_bps)}</div>
          <div className="text-sm text-muted-foreground">Outbound</div>
        </div>
      </div>
    </div>
  )
}

function IssuesTable({ issues }: { issues: LinkIssue[] | null }) {
  if (!issues || issues.length === 0) {
    return (
      <div className="border border-border rounded-lg p-6 text-center">
        <CheckCircle2 className="h-8 w-8 text-green-500 mx-auto mb-2" />
        <div className="text-sm text-muted-foreground">No issues detected</div>
      </div>
    )
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border flex items-center gap-2">
        <AlertTriangle className="h-4 w-4 text-muted-foreground" />
        <h3 className="font-medium">Link Issues</h3>
        <span className="text-sm text-muted-foreground ml-auto">Packet loss &amp; latency exceeding committed SLA</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Link</th>
              <th className="px-4 py-2 font-medium">Contributor</th>
              <th className="px-4 py-2 font-medium">Route</th>
              <th className="px-4 py-2 font-medium">Issue</th>
              <th className="px-4 py-2 font-medium text-right">Value</th>
            </tr>
          </thead>
          <tbody className="px-4">
            {issues.map((issue, idx) => (
              <tr key={`${issue.code}-${issue.issue}-${idx}`} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">
                  <span className="font-mono text-sm">{issue.code}</span>
                  <span className="text-xs text-muted-foreground ml-2">{issue.link_type}</span>
                </td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{issue.contributor || '—'}</td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{issue.side_a_metro} — {issue.side_z_metro}</td>
                <td className={`px-4 py-2.5 text-sm ${issue.issue === 'packet_loss' || issue.issue === 'down' ? 'text-red-600 dark:text-red-400' : 'text-amber-600 dark:text-amber-400'}`}>
                  {issue.issue === 'packet_loss' ? 'Packet Loss' : issue.issue === 'high_latency' ? 'High Latency' : issue.issue}
                </td>
                <td className="px-4 py-2.5 text-sm tabular-nums text-right">
                  {issue.issue === 'packet_loss'
                    ? formatPercent(issue.value)
                    : issue.issue === 'high_latency'
                      ? `+${formatPercent(issue.value)} over SLA`
                      : issue.value.toFixed(1)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function UtilizationTable({ links }: { links: LinkMetric[] | null }) {
  if (!links || links.length === 0) return null

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-4 py-3 bg-muted/50 border-b border-border">
        <h3 className="font-medium">High Utilization Links</h3>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="text-left text-sm text-muted-foreground border-b border-border">
              <th className="px-4 py-2 font-medium">Link</th>
              <th className="px-4 py-2 font-medium">Route</th>
              <th className="px-4 py-2 font-medium text-right">Capacity</th>
              <th className="px-4 py-2 font-medium text-right">In</th>
              <th className="px-4 py-2 font-medium text-right">Out</th>
            </tr>
          </thead>
          <tbody>
            {links.map((link, idx) => (
              <tr key={`${link.code}-${idx}`} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5 font-mono text-sm">{link.code}</td>
                <td className="px-4 py-2.5 text-sm text-muted-foreground">{link.side_a_metro} — {link.side_z_metro}</td>
                <td className="px-4 py-2.5 text-sm tabular-nums text-right">{formatBandwidth(link.bandwidth_bps)}</td>
                <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${link.utilization_in >= 90 ? 'text-red-600 dark:text-red-400' : link.utilization_in >= 70 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                  {formatPercent(link.utilization_in)}
                </td>
                <td className={`px-4 py-2.5 text-sm tabular-nums text-right ${link.utilization_out >= 90 ? 'text-red-600 dark:text-red-400' : link.utilization_out >= 70 ? 'text-amber-600 dark:text-amber-400' : ''}`}>
                  {formatPercent(link.utilization_out)}
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
                    <div className="font-mono text-sm">{issue.device_code}</div>
                    <div className="text-xs text-muted-foreground">{issue.contributor || issue.device_type}</div>
                  </td>
                  <td className="px-4 py-2.5 font-mono text-sm">{issue.interface_name}</td>
                  <td className="px-4 py-2.5 text-sm">
                    {issue.link_code ? (
                      <div>
                        <span className="font-mono">{issue.link_code}</span>
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

function StatusBreakdown({ network }: { network: StatusResponse['network'] }) {
  const deviceStatuses = Object.entries(network.devices_by_status).sort((a, b) => b[1] - a[1])
  const linkStatuses = Object.entries(network.links_by_status).sort((a, b) => b[1] - a[1])

  const statusColors: Record<string, string> = {
    activated: 'bg-green-500',
    pending: 'bg-amber-500',
    suspended: 'bg-red-500',
    'soft-drained': 'bg-amber-500',
    drained: 'bg-gray-500',
    deleted: 'bg-gray-400',
    rejected: 'bg-red-400',
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      <div className="border border-border rounded-lg p-4">
        <div className="flex items-center gap-2 mb-3">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Devices by Status</h3>
        </div>
        <div className="space-y-2">
          {deviceStatuses.map(([status, count]) => (
            <div key={status} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className={`h-2.5 w-2.5 rounded-full ${statusColors[status] || 'bg-gray-400'}`} />
                <span className="text-sm capitalize">{status.replace('-', ' ')}</span>
              </div>
              <span className="text-sm font-medium tabular-nums">{count}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="border border-border rounded-lg p-4">
        <div className="flex items-center gap-2 mb-3">
          <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
          <h3 className="font-medium">Links by Status</h3>
        </div>
        <div className="space-y-2">
          {linkStatuses.map(([status, count]) => (
            <div key={status} className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className={`h-2.5 w-2.5 rounded-full ${statusColors[status] || 'bg-gray-400'}`} />
                <span className="text-sm capitalize">{status.replace('-', ' ')}</span>
              </div>
              <span className="text-sm font-medium tabular-nums">{count}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export function StatusPage() {
  const { data: status, isLoading, error } = useQuery({
    queryKey: ['status'],
    queryFn: fetchStatus,
    refetchInterval: 30_000,
    staleTime: 15_000,
  })

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="animate-pulse text-muted-foreground">Loading status...</div>
      </div>
    )
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
          <StatCard label="Utilization" value={status.network.user_inbound_bps} format="bandwidth" />
        </div>

        {/* Health Cards Row */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
          <DataFreshnessCard system={status.system} timestamp={status.timestamp} />
          <InfrastructureAlertsCard alerts={status.alerts} />
          <LinkHealthCard links={status.links} />
          <ThroughputCard performance={status.performance} />
        </div>

        {/* Performance Card */}
        <div className="mb-8">
          <PerformanceCard performance={status.performance} />
        </div>

        {/* Status Breakdown */}
        <div className="mb-8">
          <StatusBreakdown network={status.network} />
        </div>

        {/* Non-Activated Links */}
        <div className="mb-8">
          <NonActivatedLinksTable links={status.alerts?.links} />
        </div>

        {/* Link Issues */}
        <div className="mb-8">
          <IssuesTable issues={status.links.issues} />
        </div>

        {/* High Utilization Links */}
        <div className="mb-8">
          <UtilizationTable links={status.links.high_util_links} />
        </div>

        {/* Non-Activated Devices */}
        <div className="mb-8">
          <NonActivatedDevicesTable devices={status.alerts?.devices} />
        </div>

        {/* Interface Issues */}
        <div className="mb-8">
          <InterfaceIssuesTable issues={status.interfaces.issues} />
        </div>
      </div>
    </div>
  )
}
