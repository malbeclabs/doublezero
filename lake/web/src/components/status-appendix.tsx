import { ArrowLeft } from 'lucide-react'
import { Link } from 'react-router-dom'

export function StatusAppendix() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-4xl mx-auto px-4 sm:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <Link
            to="/status"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground mb-4"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to Status
          </Link>
          <h1 className="text-2xl font-semibold">Status Page Methodology</h1>
          <p className="text-muted-foreground mt-2">
            This document explains the criteria and methodology used to calculate link health,
            classify issues, and determine overall network status.
          </p>
        </div>

        {/* Links Section */}
        <section className="mb-12">
          <h2 className="text-xl font-semibold mb-6 pb-2 border-b-2 border-border">Links</h2>

          {/* Link Issue Types */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Issue Types</h3>
          <p className="text-sm text-muted-foreground mb-4">
            A link "issue" is any degradation or problem. Seven distinct link issue types are tracked:
          </p>

          <div className="space-y-4">
            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">1. Soft-Drained</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-500/5 dark:bg-gray-500/20 text-gray-700 dark:text-gray-400">
                  Disabled
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Link status set to <code className="bg-muted px-1 py-0.5 rounded text-xs">soft-drained</code>.
                Traffic is routed away but the link remains available for failover.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">2. Hard-Drained</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-500/5 dark:bg-gray-500/20 text-gray-700 dark:text-gray-400">
                  Disabled
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Link status set to <code className="bg-muted px-1 py-0.5 rounded text-xs">hard-drained</code>.
                Link is fully disabled and not available for traffic.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">3. ISIS Delay Override (Effective Soft-Drain)</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-500/5 dark:bg-gray-500/20 text-gray-700 dark:text-gray-400">
                  Disabled
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Link has <code className="bg-muted px-1 py-0.5 rounded text-xs">isis_delay_override_ns</code> set to <code className="bg-muted px-1 py-0.5 rounded text-xs">1000ms</code>.
                This makes the link less preferred in routing, effectively soft-draining it without changing the status field.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <h3 className="font-medium mb-2">4. Packet Loss</h3>
              <p className="text-sm text-muted-foreground mb-2">
                Link experiencing measurable packet loss. Severity levels:
              </p>
              <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
                <li><strong>Minor:</strong> Loss &lt; 1% — Detectable but likely not impactful</li>
                <li className="flex items-center gap-2 flex-wrap">
                  <span><strong>Moderate:</strong> Loss 1% - 10% — Noticeable degradation</span>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-amber-500/5 dark:bg-amber-500/20 text-amber-700 dark:text-amber-400">
                    Degraded
                  </span>
                </li>
                <li className="flex items-center gap-2 flex-wrap">
                  <span><strong>Severe:</strong> Loss &ge; 10% — Significant impact</span>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-red-500/5 dark:bg-red-500/20 text-red-700 dark:text-red-400">
                    Unhealthy
                  </span>
                </li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">5. High Latency</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-amber-500/5 dark:bg-amber-500/20 text-amber-700 dark:text-amber-400">
                  Degraded
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Link measured RTT significantly exceeds the committed RTT (SLA).
                Only applies to activated inter-metro WAN links with a committed RTT configured.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">6. Missing Telemetry (Link Dark)</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-red-500/5 dark:bg-red-500/20 text-red-700 dark:text-red-400">
                  Unhealthy
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                No latency samples received for 2+ hours. Could indicate: link down, monitoring failure,
                or connectivity issue. Distinguished from packet loss since we have no measurements at all.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <h3 className="font-medium">7. Extended Packet Loss (Effective Disabled)</h3>
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-500/5 dark:bg-gray-500/20 text-gray-700 dark:text-gray-400">
                  Disabled
                </span>
              </div>
              <p className="text-sm text-muted-foreground">
                Link has had 100% packet loss for 2+ consecutive hours. The link is receiving telemetry
                but all probes are failing, indicating the link is effectively down even though it hasn't
                been officially drained.
              </p>
            </div>
          </div>
          </div>

          {/* Link Health Classification */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Health Classification</h3>
          <p className="text-sm text-muted-foreground mb-4">
            Links are classified into health states based on which issue types apply:
          </p>

          <div className="space-y-4">
            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-green-500" />
                <h3 className="font-medium">Healthy</h3>
              </div>
              <p className="text-sm text-muted-foreground ml-5">
                No active issues detected.
              </p>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-amber-500" />
                <h3 className="font-medium">Degraded</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Moderate packet loss (1% - 10%)</li>
                <li>High latency</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-red-500" />
                <h3 className="font-medium">Unhealthy</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Severe packet loss (&ge; 10%)</li>
                <li>Missing telemetry (link dark)</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-gray-500 dark:bg-gray-700" />
                <h3 className="font-medium">Disabled</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Soft-drained, hard-drained, or ISIS delay override</li>
                <li>Extended packet loss (100% for 2+ hours)</li>
              </ul>
            </div>
          </div>
          </div>

          {/* Latency Considerations */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Latency Classification</h3>
          <p className="text-sm text-muted-foreground mb-4">
            Latency is only considered for link health classification when all of the following conditions are met:
          </p>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><strong>Link type is WAN</strong> — DZX and other local link types are excluded</li>
            <li><strong>Inter-metro connection</strong> — Links between devices in the same metro are excluded (intra-metro)</li>
            <li><strong>Committed RTT is defined</strong> — The link must have a committed RTT SLA configured</li>
          </ul>
          <p className="text-sm text-muted-foreground mt-4">
            Latency overage is calculated as a percentage over the committed RTT:
          </p>
          <pre className="bg-muted/50 border border-border rounded-lg p-3 mt-2 text-xs font-mono overflow-x-auto">
            overage_pct = ((measured_latency - committed_rtt) / committed_rtt) * 100
          </pre>
          </div>

          {/* Link Status Timeline */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Status Timeline</h3>
          <p className="text-sm text-muted-foreground mb-4">
            The timeline shows historical link health in time buckets. The bucket size varies based on the selected time range:
          </p>
          <div className="overflow-x-auto">
            <table className="w-full text-sm border border-border rounded-lg">
              <thead>
                <tr className="bg-muted/50">
                  <th className="px-4 py-2 text-left font-medium border-b border-border">Time Range</th>
                  <th className="px-4 py-2 text-left font-medium border-b border-border">Bucket Size</th>
                  <th className="px-4 py-2 text-left font-medium border-b border-border">Total Buckets</th>
                </tr>
              </thead>
              <tbody className="text-muted-foreground">
                <tr><td className="px-4 py-2 border-b border-border">1 hour</td><td className="px-4 py-2 border-b border-border">~5 minutes</td><td className="px-4 py-2 border-b border-border">12-72</td></tr>
                <tr><td className="px-4 py-2 border-b border-border">6 hours</td><td className="px-4 py-2 border-b border-border">~5-10 minutes</td><td className="px-4 py-2 border-b border-border">36-72</td></tr>
                <tr><td className="px-4 py-2 border-b border-border">24 hours</td><td className="px-4 py-2 border-b border-border">~20 minutes</td><td className="px-4 py-2 border-b border-border">72</td></tr>
                <tr><td className="px-4 py-2 border-b border-border">3 days</td><td className="px-4 py-2 border-b border-border">~1 hour</td><td className="px-4 py-2 border-b border-border">72</td></tr>
                <tr><td className="px-4 py-2">7 days</td><td className="px-4 py-2">~2.3 hours</td><td className="px-4 py-2">72</td></tr>
              </tbody>
            </table>
          </div>

          <h3 className="font-medium mt-6 mb-3">Timeline Bucket States</h3>
          <div className="space-y-3">
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-green-500 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Healthy</strong> — No active issues detected
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-amber-500 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Degraded</strong> — Moderate packet loss (1% - 10%) or latency SLA breach
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-red-500 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Unhealthy</strong> — Severe packet loss (&ge; 10%) or missing telemetry (link dark)
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-gray-500 dark:bg-gray-700 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Disabled</strong> — Drained (soft, hard, or ISIS delay override) or extended packet loss (100% for 2+ hours)
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-transparent border border-gray-200 dark:border-gray-700 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">No Data</strong> — No telemetry received for this time bucket (often the most recent bucket waiting for data)
              </div>
            </div>
          </div>
          </div>

          {/* Disabled Links */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Disabled Links</h3>
          <p className="text-sm text-muted-foreground mb-4">
            Links can be classified as "disabled" for several reasons:
          </p>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><strong>Soft drained</strong> — Link status set to soft-drained</li>
            <li><strong>Hard drained</strong> — Link status set to hard-drained</li>
            <li><strong>ISIS delay override</strong> — Link has <code className="bg-muted px-1 py-0.5 rounded text-xs">isis_delay_override_ns</code> set to 1000ms, effectively soft-draining it without changing the status field</li>
            <li><strong>Link dark</strong> — No telemetry received for 2+ hours</li>
          </ul>
          <p className="text-sm text-muted-foreground mt-4">
            The "Disabled Links" table shows the current state, not historical. A link appears there only if it is
            currently disabled, not if it was disabled at some point in the past.
          </p>
          </div>

          {/* Link Criticality */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Link Criticality (Redundancy)</h3>
            <p className="text-sm text-muted-foreground mb-4">
              Links are classified by their redundancy level based on how many connections each endpoint device has:
            </p>

            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium bg-red-500/15 text-red-600 dark:text-red-400">
                    SPOF
                  </span>
                  <h4 className="font-medium">Single Point of Failure</h4>
                </div>
                <p className="text-sm text-muted-foreground">
                  At least one endpoint device has only this connection. If this link fails, a device loses all connectivity.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium bg-amber-500/15 text-amber-600 dark:text-amber-400">
                    Limited
                  </span>
                  <h4 className="font-medium">Limited Redundancy</h4>
                </div>
                <p className="text-sm text-muted-foreground">
                  Both endpoint devices have exactly 2 connections each. Losing this link leaves each device with only 1 remaining connection.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <h4 className="font-medium mb-2">Well Connected (No Badge)</h4>
                <p className="text-sm text-muted-foreground">
                  Both endpoint devices have 3+ connections. Traffic can reroute if this link fails.
                </p>
              </div>
            </div>

            <p className="text-sm text-muted-foreground mt-4">
              Criticality badges appear next to the link code in the status timeline. Click the info icon to see
              more details about the link's redundancy in the popover.
            </p>
          </div>

          {/* Issue Reasons */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Issue Reason Tags</h3>
          <p className="text-sm text-muted-foreground mb-4">
            Links in the status timeline can have issue reason tags that indicate why they appear in the list:
          </p>
          <div className="space-y-3">
            <div className="flex items-start gap-3">
              <span
                className="text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0"
                style={{ backgroundColor: 'rgba(239, 68, 68, 0.15)', color: '#dc2626' }}
              >
                Loss
              </span>
              <div className="text-sm text-muted-foreground">
                At least one bucket has packet loss &ge; 1% (moderate or severe)
              </div>
            </div>
            <div className="flex items-start gap-3">
              <span
                className="text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0"
                style={{ backgroundColor: 'rgba(245, 158, 11, 0.15)', color: '#d97706' }}
              >
                Latency
              </span>
              <div className="text-sm text-muted-foreground">
                At least one bucket has latency SLA breach (inter-metro WAN links only)
              </div>
            </div>
            <div className="flex items-start gap-3">
              <span
                className="text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0"
                style={{ backgroundColor: 'rgba(55, 65, 81, 0.15)', color: '#4b5563' }}
              >
                Disabled
              </span>
              <div className="text-sm text-muted-foreground">
                Link is drained (soft, hard, or ISIS delay override) or has extended packet loss (100% for 2+ hours)
              </div>
            </div>
          </div>
          <p className="text-sm text-muted-foreground mt-4">
            If a link has only the "Disabled" tag, the "Loss" tag is suppressed since the extended outage
            is a more accurate characterization than packet loss.
          </p>
          </div>
        </section>

        {/* Devices Section */}
        <section className="mb-12">
          <h2 className="text-xl font-semibold mb-6 pb-2 border-b-2 border-border">Devices</h2>

          {/* Device Issue Types */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Issue Types</h3>
            <p className="text-sm text-muted-foreground mb-4">
              A device "issue" is any problem detected on the device or its interfaces. Four distinct device issue types are tracked.
              Device interfaces may be associated with a link (as side A or side Z), but not all interfaces are part of a link.
            </p>

            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <h4 className="font-medium">1. Drained</h4>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-gray-500/5 dark:bg-gray-500/20 text-gray-700 dark:text-gray-400">
                    Disabled
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">
                  Device status set to <code className="bg-muted px-1 py-0.5 rounded text-xs">drained</code>.
                  Traffic is routed away from this device.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <h4 className="font-medium">2. Interface Discards</h4>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-amber-500/5 dark:bg-amber-500/20 text-amber-700 dark:text-amber-400">
                    Degraded
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">
                  Interface is dropping packets. Can be inbound (rx) or outbound (tx) discards.
                  Often indicates buffer overflow, QoS policy drops, or congestion.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <h4 className="font-medium">3. Interface Errors</h4>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-red-500/5 dark:bg-red-500/20 text-red-700 dark:text-red-400">
                    Unhealthy
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">
                  Interface is experiencing errors. Can be inbound (rx) or outbound (tx) errors.
                  Often indicates physical layer issues, CRC errors, or hardware problems.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <h4 className="font-medium">4. Interface Carrier Transitions</h4>
                  <span className="text-xs px-1.5 py-0.5 rounded bg-red-500/5 dark:bg-red-500/20 text-red-700 dark:text-red-400">
                    Unhealthy
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">
                  Interface carrier state is flapping (going up and down). Can be inbound (rx) or outbound (tx).
                  Indicates link instability, often due to physical layer issues or misconfiguration.
                </p>
              </div>
            </div>
          </div>
        </section>

        {/* Metros Section */}
        <section className="mb-12">
          <h2 className="text-xl font-semibold mb-6 pb-2 border-b-2 border-border">Metros</h2>

          {/* Metro Health Calculation */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Health Calculation</h3>
            <p className="text-sm text-muted-foreground mb-4">
              Metro health is calculated based on the proportion of working links touching that metro.
              A link "touches" a metro if either endpoint (side A or side Z) is in that metro.
            </p>

            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <div className="w-3 h-3 rounded-full bg-green-500" />
                  <h4 className="font-medium">Operational</h4>
                </div>
                <p className="text-sm text-muted-foreground ml-5">
                  ≥ 80% of active links are working (healthy or degraded)
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <div className="w-3 h-3 rounded-full bg-amber-500" />
                  <h4 className="font-medium">Some Issues</h4>
                </div>
                <p className="text-sm text-muted-foreground ml-5">
                  20% - 80% of active links are working
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <div className="w-3 h-3 rounded-full bg-red-500" />
                  <h4 className="font-medium">Significant Issues</h4>
                </div>
                <p className="text-sm text-muted-foreground ml-5">
                  &lt; 20% of active links are working
                </p>
              </div>
            </div>

            <p className="text-sm text-muted-foreground mt-4">
              <strong>Note:</strong> Disabled links (drained) and links with no data are excluded from the calculation.
              Only active links (healthy, degraded, or unhealthy) are considered.
            </p>
          </div>

          {/* SPOF Links */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Single Points of Failure (SPOF)</h3>
            <p className="text-sm text-muted-foreground mb-4">
              A SPOF link is a link where at least one endpoint device has only that single connection.
              If the link fails, that device loses all network connectivity.
            </p>

            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium bg-amber-500/15 text-amber-600 dark:text-amber-400">
                    2 SPOF
                  </span>
                  <h4 className="font-medium">SPOF Badge (Normal)</h4>
                </div>
                <p className="text-sm text-muted-foreground">
                  Amber badge indicates the metro has SPOF links, but all are currently healthy.
                  The number shows how many SPOF links exist.
                </p>
              </div>

              <div className="border border-border rounded-lg p-4">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-[10px] px-1.5 py-0.5 rounded font-medium bg-red-500/20 text-red-600 dark:text-red-400 inline-flex items-center gap-1">
                    <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                    </svg>
                    2 SPOF
                  </span>
                  <h4 className="font-medium">SPOF Badge (At Risk)</h4>
                </div>
                <p className="text-sm text-muted-foreground">
                  Red badge with warning icon indicates at least one SPOF link is degraded or unhealthy.
                  This is a critical situation requiring immediate attention.
                </p>
              </div>
            </div>
          </div>

          {/* SPOF Impact on Health */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">SPOF Impact on Metro Health</h3>
            <p className="text-sm text-muted-foreground mb-4">
              SPOF link status directly affects metro health classification:
            </p>
            <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
              <li><strong>Any SPOF link unhealthy</strong> → Metro becomes <span className="text-red-500 font-medium">Significant Issues</span> (regardless of other links)</li>
              <li><strong>Any SPOF link degraded</strong> → Metro becomes at least <span className="text-amber-500 font-medium">Some Issues</span></li>
            </ul>
            <p className="text-sm text-muted-foreground mt-4">
              This ensures SPOF issues are always surfaced prominently, even if the metro has many other healthy links.
            </p>
          </div>

          {/* Metro Info Popover */}
          <div className="mb-8">
            <h3 className="text-lg font-semibold mb-4">Metro Details</h3>
            <p className="text-sm text-muted-foreground mb-4">
              Click the info icon next to a metro name to see details including:
            </p>
            <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
              <li><strong>Links</strong> — Total number of links touching this metro</li>
              <li><strong>Single Points of Failure</strong> — List of SPOF links with their current status (click to view link details)</li>
              <li><strong>Current Status</strong> — Overall metro health</li>
            </ul>
          </div>
        </section>

        {/* Overall Status */}
        <section className="mb-10">
          <h2 className="text-xl font-semibold mb-6 pb-2 border-b-2 border-border">Overall Network Status</h2>
          <p className="text-sm text-muted-foreground mb-4">
            The banner at the top of the status page shows overall network health, determined by:
          </p>
          <div className="space-y-4">
            <div className="border border-border rounded-lg p-4 border-l-4 border-l-red-500">
              <h3 className="font-medium mb-2">Unhealthy</h3>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5 list-disc">
                <li>&gt; 10% of links are unhealthy</li>
                <li>Average packet loss &ge; 1%</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4 border-l-4 border-l-amber-500">
              <h3 className="font-medium mb-2">Degraded</h3>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5 list-disc">
                <li>Any links are unhealthy (but &le; 10%)</li>
                <li>&gt; 20% of links are degraded</li>
                <li>Average packet loss &ge; 0.1%</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4 border-l-4 border-l-green-500">
              <h3 className="font-medium mb-2">Healthy</h3>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5 list-disc">
                <li>None of the above conditions are met</li>
              </ul>
            </div>
          </div>
        </section>

        {/* Data Sources */}
        <section className="mb-10">
          <h2 className="text-xl font-semibold mb-6 pb-2 border-b-2 border-border">Data Sources</h2>
          <p className="text-sm text-muted-foreground mb-4">
            Status metrics are derived from the following data sources:
          </p>

          <h3 className="font-medium mt-4 mb-2">Views</h3>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">dz_links_health_current</code> — Current health state of each link with boolean flags (is_soft_drained, is_hard_drained, is_isis_soft_drained, has_packet_loss, exceeds_committed_rtt, is_dark)</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">dz_link_status_changes</code> — Historical status transitions for links</li>
          </ul>

          <h3 className="font-medium mt-4 mb-2">Base Tables</h3>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">fact_dz_device_link_latency</code> — Per-second latency and loss measurements from network probes</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">fact_dz_device_interface_counters</code> — Interface error and discard counters</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">dim_dz_links_history</code> — Historical link status and configuration (including isis_delay_override_ns)</li>
          </ul>
        </section>

        {/* Footer */}
        <div className="text-center text-sm text-muted-foreground pt-4 border-t border-border">
          <Link to="/status" className="hover:text-foreground">
            &larr; Back to Status Page
          </Link>
        </div>
      </div>
    </div>
  )
}
