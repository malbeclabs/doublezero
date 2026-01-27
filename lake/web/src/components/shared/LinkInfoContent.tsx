import { Link } from 'react-router-dom'
import type { LinkDetail } from '@/lib/api'
import { TrafficCharts } from '@/components/topology/TrafficCharts'
import { LatencyCharts } from '@/components/topology/LatencyCharts'
import { LinkStatusCharts } from '@/components/topology/LinkStatusCharts'

// Shared link info type that both topology and link page can use
export interface LinkInfoData {
  pk: string
  code: string
  status: string
  linkType: string
  bandwidthBps: number
  sideAPk: string
  sideACode: string
  sideAMetro: string
  sideAIfaceName: string
  sideAIP: string
  sideZPk: string
  sideZCode: string
  sideZMetro: string
  sideZIfaceName: string
  sideZIP: string
  contributorPk: string
  contributorCode: string
  inBps: number
  outBps: number
  utilizationIn: number
  utilizationOut: number
  latencyUs: number
  jitterUs: number
  latencyAtoZUs: number
  jitterAtoZUs: number
  latencyZtoAUs: number
  jitterZtoAUs: number
  lossPercent: number
  peakInBps?: number
  peakOutBps?: number
}

interface LinkInfoContentProps {
  link: LinkInfoData
  /** Compact mode for sidebar panels */
  compact?: boolean
}

function formatBps(bps: number): string {
  if (bps === 0) return '—'
  if (bps >= 1e12) return `${(bps / 1e12).toFixed(1)} Tbps`
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} Kbps`
  return `${bps.toFixed(0)} bps`
}

function formatLatencyUs(us: number): string {
  if (us === 0) return '—'
  if (us >= 1000) return `${(us / 1000).toFixed(2)} ms`
  return `${us.toFixed(0)} µs`
}

function formatPercent(value: number): string {
  if (value === 0) return '—'
  return `${value.toFixed(2)}%`
}

const statusColors: Record<string, string> = {
  activated: 'text-green-600 dark:text-green-400',
  provisioning: 'text-blue-600 dark:text-blue-400',
  maintenance: 'text-amber-600 dark:text-amber-400',
  offline: 'text-red-600 dark:text-red-400',
}

/**
 * Shared component for displaying link information.
 * Used by both the topology panel and the link detail page.
 */
export function LinkInfoContent({ link, compact = false }: LinkInfoContentProps) {
  // Check if we have directional latency data
  const hasDirectionalData = link.latencyAtoZUs > 0 || link.latencyZtoAUs > 0

  // Compact mode: optimized for sidebar panels
  if (compact) {
    return (
      <div className="space-y-4">
        {/* Endpoints with per-direction latency */}
        <div className="grid grid-cols-2 gap-3">
          <div className="p-2 bg-muted/30 rounded-lg">
            <div className="text-xs text-muted-foreground mb-1">A-Side</div>
            <div className="text-sm font-medium">
              <Link to={`/dz/devices/${link.sideAPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                {link.sideACode}
              </Link>
            </div>
            {link.sideAIfaceName && (
              <div className="text-xs text-muted-foreground font-mono mt-0.5">{link.sideAIfaceName}</div>
            )}
            {link.sideAIP && (
              <div className="text-xs text-muted-foreground font-mono">{link.sideAIP}</div>
            )}
            {hasDirectionalData && (
              <div className="mt-2 pt-2 border-t border-muted/50">
                <div className="text-xs text-muted-foreground">RTT from A</div>
                <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.latencyAtoZUs)}</div>
                <div className="text-xs text-muted-foreground mt-1">Jitter from A</div>
                <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.jitterAtoZUs)}</div>
              </div>
            )}
          </div>
          <div className="p-2 bg-muted/30 rounded-lg">
            <div className="text-xs text-muted-foreground mb-1">Z-Side</div>
            <div className="text-sm font-medium">
              <Link to={`/dz/devices/${link.sideZPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                {link.sideZCode}
              </Link>
            </div>
            {link.sideZIfaceName && (
              <div className="text-xs text-muted-foreground font-mono mt-0.5">{link.sideZIfaceName}</div>
            )}
            {link.sideZIP && (
              <div className="text-xs text-muted-foreground font-mono">{link.sideZIP}</div>
            )}
            {hasDirectionalData && (
              <div className="mt-2 pt-2 border-t border-muted/50">
                <div className="text-xs text-muted-foreground">RTT from Z</div>
                <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.latencyZtoAUs)}</div>
                <div className="text-xs text-muted-foreground mt-1">Jitter from Z</div>
                <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.jitterZtoAUs)}</div>
              </div>
            )}
          </div>
        </div>

        {/* Combined latency (average of both directions) - shown when no directional data */}
        {!hasDirectionalData && (
          <div className="grid grid-cols-2 gap-2">
            <div className="text-center p-2 bg-muted/30 rounded-lg">
              <div className="text-base font-medium tabular-nums tracking-tight">{formatLatencyUs(link.latencyUs)}</div>
              <div className="text-xs text-muted-foreground">Latency</div>
            </div>
            <div className="text-center p-2 bg-muted/30 rounded-lg">
              <div className="text-base font-medium tabular-nums tracking-tight">{formatLatencyUs(link.jitterUs)}</div>
              <div className="text-xs text-muted-foreground">Jitter</div>
            </div>
          </div>
        )}

        {/* Stats grid - 2 columns for sidebar */}
        <div className="grid grid-cols-2 gap-2">
          <div className="text-center p-2 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{formatPercent(link.lossPercent)}</div>
            <div className="text-xs text-muted-foreground">Packet Loss</div>
          </div>
          <div className="text-center p-2 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{formatBps(link.bandwidthBps)}</div>
            <div className="text-xs text-muted-foreground">Bandwidth</div>
          </div>
          <div className="text-center p-2 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{formatBps(link.inBps)}</div>
            <div className="text-xs text-muted-foreground">Current In</div>
          </div>
          <div className="text-center p-2 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums tracking-tight">{formatBps(link.outBps)}</div>
            <div className="text-xs text-muted-foreground">Current Out</div>
          </div>
          <div className="text-center p-2 bg-muted/30 rounded-lg col-span-2">
            <div className="text-base font-medium tabular-nums tracking-tight">
              {link.contributorPk ? (
                <Link to={`/dz/contributors/${link.contributorPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                  {link.contributorCode}
                </Link>
              ) : (
                link.contributorCode || '—'
              )}
            </div>
            <div className="text-xs text-muted-foreground">Contributor</div>
          </div>
        </div>

        {/* Traffic charts */}
        <TrafficCharts entityType="link" entityPk={link.pk} />

        {/* Latency charts */}
        <LatencyCharts linkPk={link.pk} />

        {/* Link status charts (packet loss, interface issues) */}
        <LinkStatusCharts linkPk={link.pk} />
      </div>
    )
  }

  // Wide mode: optimized for full-page view on desktop
  return (
    <div className="space-y-6">
      {/* Endpoints and Stats row - side by side on large screens */}
      <div className="grid grid-cols-1 lg:grid-cols-[auto,1fr] gap-6">
        {/* Endpoints */}
        <div className="grid grid-cols-2 gap-4 lg:w-96">
          <div className="p-3 bg-muted/30 rounded-lg">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">A-Side</div>
            <div className="text-sm font-medium">
              <Link to={`/dz/devices/${link.sideAPk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                {link.sideACode}
              </Link>
              {link.sideAMetro && <span className="text-muted-foreground ml-1">({link.sideAMetro})</span>}
            </div>
            {link.sideAIfaceName && (
              <div className="text-xs text-muted-foreground font-mono mt-1">{link.sideAIfaceName}</div>
            )}
            {link.sideAIP && (
              <div className="text-xs text-muted-foreground font-mono">{link.sideAIP}</div>
            )}
            {hasDirectionalData && (
              <div className="mt-3 pt-3 border-t border-muted/50 grid grid-cols-2 gap-2">
                <div>
                  <div className="text-xs text-muted-foreground">RTT</div>
                  <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.latencyAtoZUs)}</div>
                </div>
                <div>
                  <div className="text-xs text-muted-foreground">Jitter</div>
                  <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.jitterAtoZUs)}</div>
                </div>
              </div>
            )}
          </div>
          <div className="p-3 bg-muted/30 rounded-lg">
            <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Z-Side</div>
            <div className="text-sm font-medium">
              <Link to={`/dz/devices/${link.sideZPk}`} className="text-blue-600 dark:text-blue-400 hover:underline font-mono">
                {link.sideZCode}
              </Link>
              {link.sideZMetro && <span className="text-muted-foreground ml-1">({link.sideZMetro})</span>}
            </div>
            {link.sideZIfaceName && (
              <div className="text-xs text-muted-foreground font-mono mt-1">{link.sideZIfaceName}</div>
            )}
            {link.sideZIP && (
              <div className="text-xs text-muted-foreground font-mono">{link.sideZIP}</div>
            )}
            {hasDirectionalData && (
              <div className="mt-3 pt-3 border-t border-muted/50 grid grid-cols-2 gap-2">
                <div>
                  <div className="text-xs text-muted-foreground">RTT</div>
                  <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.latencyZtoAUs)}</div>
                </div>
                <div>
                  <div className="text-xs text-muted-foreground">Jitter</div>
                  <div className="text-sm font-medium tabular-nums">{formatLatencyUs(link.jitterZtoAUs)}</div>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Stats grid - responsive columns */}
        <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-5 gap-2">
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className={`text-base font-medium capitalize ${statusColors[link.status] || ''}`}>{link.status}</div>
            <div className="text-xs text-muted-foreground">Status</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium">{link.linkType}</div>
            <div className="text-xs text-muted-foreground">Type</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{formatBps(link.bandwidthBps)}</div>
            <div className="text-xs text-muted-foreground">Bandwidth</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{link.utilizationIn.toFixed(1)}%</div>
            <div className="text-xs text-muted-foreground">Util In</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{link.utilizationOut.toFixed(1)}%</div>
            <div className="text-xs text-muted-foreground">Util Out</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{formatBps(link.inBps)}</div>
            <div className="text-xs text-muted-foreground">Current In</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{formatBps(link.outBps)}</div>
            <div className="text-xs text-muted-foreground">Current Out</div>
          </div>
          {link.peakInBps !== undefined && (
            <div className="text-center p-3 bg-muted/30 rounded-lg">
              <div className="text-base font-medium tabular-nums">{formatBps(link.peakInBps)}</div>
              <div className="text-xs text-muted-foreground">Peak In (1h)</div>
            </div>
          )}
          {link.peakOutBps !== undefined && (
            <div className="text-center p-3 bg-muted/30 rounded-lg">
              <div className="text-base font-medium tabular-nums">{formatBps(link.peakOutBps)}</div>
              <div className="text-xs text-muted-foreground">Peak Out (1h)</div>
            </div>
          )}
          {!hasDirectionalData && (
            <>
              <div className="text-center p-3 bg-muted/30 rounded-lg">
                <div className="text-base font-medium tabular-nums">{formatLatencyUs(link.latencyUs)}</div>
                <div className="text-xs text-muted-foreground">Latency</div>
              </div>
              <div className="text-center p-3 bg-muted/30 rounded-lg">
                <div className="text-base font-medium tabular-nums">{formatLatencyUs(link.jitterUs)}</div>
                <div className="text-xs text-muted-foreground">Jitter</div>
              </div>
            </>
          )}
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium tabular-nums">{formatPercent(link.lossPercent)}</div>
            <div className="text-xs text-muted-foreground">Packet Loss</div>
          </div>
          <div className="text-center p-3 bg-muted/30 rounded-lg">
            <div className="text-base font-medium">
              {link.contributorPk ? (
                <Link to={`/dz/contributors/${link.contributorPk}`} className="text-blue-600 dark:text-blue-400 hover:underline">
                  {link.contributorCode}
                </Link>
              ) : (
                link.contributorCode || '—'
              )}
            </div>
            <div className="text-xs text-muted-foreground">Contributor</div>
          </div>
        </div>
      </div>

      {/* Charts row - side by side on large screens */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div>
          <TrafficCharts entityType="link" entityPk={link.pk} />
        </div>
        <div>
          <LatencyCharts linkPk={link.pk} />
        </div>
      </div>

      {/* Link status charts (packet loss, interface issues) */}
      <LinkStatusCharts linkPk={link.pk} />
    </div>
  )
}

/**
 * Convert LinkDetail (from /api/dz/links/:pk) to shared LinkInfoData
 */
export function linkDetailToInfo(link: LinkDetail): LinkInfoData {
  return {
    pk: link.pk,
    code: link.code,
    status: link.status,
    linkType: link.link_type,
    bandwidthBps: link.bandwidth_bps,
    sideAPk: link.side_a_pk,
    sideACode: link.side_a_code,
    sideAMetro: link.side_a_metro,
    sideAIfaceName: link.side_a_iface_name,
    sideAIP: link.side_a_ip,
    sideZPk: link.side_z_pk,
    sideZCode: link.side_z_code,
    sideZMetro: link.side_z_metro,
    sideZIfaceName: link.side_z_iface_name,
    sideZIP: link.side_z_ip,
    contributorPk: link.contributor_pk,
    contributorCode: link.contributor_code,
    inBps: link.in_bps,
    outBps: link.out_bps,
    utilizationIn: link.utilization_in,
    utilizationOut: link.utilization_out,
    latencyUs: link.latency_us,
    jitterUs: link.jitter_us,
    latencyAtoZUs: link.latency_a_to_z_us,
    jitterAtoZUs: link.jitter_a_to_z_us,
    latencyZtoAUs: link.latency_z_to_a_us,
    jitterZtoAUs: link.jitter_z_to_a_us,
    lossPercent: link.loss_percent,
    peakInBps: link.peak_in_bps,
    peakOutBps: link.peak_out_bps,
  }
}

/**
 * Convert topology LinkInfo to shared LinkInfoData
 */
export function topologyLinkToInfo(link: {
  pk: string
  code: string
  linkType: string
  bandwidthBps: number
  latencyUs: number
  jitterUs: number
  latencyAtoZUs: number
  jitterAtoZUs: number
  latencyZtoAUs: number
  jitterZtoAUs: number
  lossPercent: number
  inBps: number
  outBps: number
  deviceAPk: string
  deviceACode: string
  interfaceAName: string
  interfaceAIP: string
  deviceZPk: string
  deviceZCode: string
  interfaceZName: string
  interfaceZIP: string
  contributorPk: string
  contributorCode: string
}): LinkInfoData {
  return {
    pk: link.pk,
    code: link.code,
    status: 'activated', // Topology links are always activated
    linkType: link.linkType,
    bandwidthBps: link.bandwidthBps,
    sideAPk: link.deviceAPk,
    sideACode: link.deviceACode,
    sideAMetro: '', // Not available from topology
    sideAIfaceName: link.interfaceAName,
    sideAIP: link.interfaceAIP,
    sideZPk: link.deviceZPk,
    sideZCode: link.deviceZCode,
    sideZMetro: '', // Not available from topology
    sideZIfaceName: link.interfaceZName,
    sideZIP: link.interfaceZIP,
    contributorPk: link.contributorPk,
    contributorCode: link.contributorCode,
    inBps: link.inBps,
    outBps: link.outBps,
    utilizationIn: link.bandwidthBps > 0 ? (link.inBps / link.bandwidthBps) * 100 : 0,
    utilizationOut: link.bandwidthBps > 0 ? (link.outBps / link.bandwidthBps) * 100 : 0,
    latencyUs: link.latencyUs,
    jitterUs: link.jitterUs,
    latencyAtoZUs: link.latencyAtoZUs,
    jitterAtoZUs: link.jitterAtoZUs,
    latencyZtoAUs: link.latencyZtoAUs,
    jitterZtoAUs: link.jitterZtoAUs,
    lossPercent: link.lossPercent,
  }
}
