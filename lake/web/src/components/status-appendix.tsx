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

        {/* Link Health Classification */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Link Health Classification</h2>
          <p className="text-sm text-muted-foreground mb-4">
            Link health is calculated over a 1-hour rolling window based on packet loss and latency metrics.
            Each activated link is classified into one of four states:
          </p>

          <div className="space-y-4">
            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-green-500" />
                <h3 className="font-medium">Healthy</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Packet loss &lt; 0.1%</li>
                <li>For inter-metro WAN links: latency within 20% of committed RTT</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-amber-500" />
                <h3 className="font-medium">Degraded</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Packet loss &ge; 0.1% and &lt; 1%</li>
                <li>For inter-metro WAN links: latency 20-50% over committed RTT</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-red-500" />
                <h3 className="font-medium">Unhealthy</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Packet loss &ge; 1% and &lt; 95%</li>
                <li>For inter-metro WAN links: latency &gt; 50% over committed RTT</li>
              </ul>
            </div>

            <div className="border border-border rounded-lg p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="w-3 h-3 rounded-full bg-gray-500 dark:bg-gray-700" />
                <h3 className="font-medium">Disabled</h3>
              </div>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5">
                <li>Packet loss &ge; 95% over the 1-hour window</li>
                <li>Indicates the link is effectively down or unreachable</li>
                <li>Distinguished from "unhealthy" to avoid inflating critical issue counts</li>
              </ul>
            </div>
          </div>
        </section>

        {/* Latency Considerations */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Latency Classification</h2>
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
        </section>

        {/* Link Status Timeline */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Link Status Timeline</h2>
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
                <strong className="text-foreground">Healthy</strong> — Normal operation within thresholds
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-amber-500 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Degraded</strong> — Minor packet loss or elevated latency
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-red-500 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Unhealthy</strong> — Significant packet loss or high latency
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-gray-500 dark:bg-gray-700 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">Disabled</strong> — Link drained or 100% packet loss for 2+ consecutive hours
              </div>
            </div>
            <div className="flex items-start gap-3">
              <div className="w-4 h-4 rounded-sm bg-transparent border border-gray-200 dark:border-gray-700 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-muted-foreground">
                <strong className="text-foreground">No Data</strong> — No telemetry received for this time bucket (often the most recent bucket waiting for data)
              </div>
            </div>
          </div>
        </section>

        {/* Disabled Links */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Disabled Links</h2>
          <p className="text-sm text-muted-foreground mb-4">
            Links can be classified as "disabled" for several reasons:
          </p>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><strong>Soft drained</strong> — Link has ISIS delay override set to 1000ms, routing traffic away</li>
            <li><strong>Hard drained</strong> — Link status explicitly set to hard-drained in the system</li>
            <li><strong>Extended packet loss (&gt;2h)</strong> — 100% packet loss for 2+ consecutive hours in the timeline</li>
          </ul>
          <p className="text-sm text-muted-foreground mt-4">
            The "Disabled Links" table shows the current state, not historical. A link appears there only if it is
            currently disabled, not if it was disabled at some point in the past.
          </p>
        </section>

        {/* Issue Reasons */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Issue Reason Tags</h2>
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
                At least one bucket has packet loss &ge; 0.1% (and is not classified as disabled)
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
                At least one bucket has latency &ge; 20% over committed RTT (inter-metro WAN links only)
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
                Link is drained or has extended packet loss (100% for 2+ consecutive hours)
              </div>
            </div>
          </div>
          <p className="text-sm text-muted-foreground mt-4">
            If a link has only the "Disabled" tag, the "Loss" tag is suppressed since the extended outage
            is a more accurate characterization than packet loss.
          </p>
        </section>

        {/* Overall Status */}
        <section className="mb-10">
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Overall Network Status</h2>
          <p className="text-sm text-muted-foreground mb-4">
            The banner at the top of the status page shows overall network health, determined by:
          </p>
          <div className="space-y-4">
            <div className="border border-border rounded-lg p-4 border-l-4 border-l-red-500">
              <h3 className="font-medium mb-2">Unhealthy</h3>
              <ul className="text-sm text-muted-foreground space-y-1 ml-5 list-disc">
                <li>Database connectivity issues</li>
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
          <h2 className="text-lg font-semibold mb-4 pb-2 border-b border-border">Data Sources</h2>
          <p className="text-sm text-muted-foreground mb-4">
            Status metrics are derived from the following data sources:
          </p>
          <ul className="text-sm text-muted-foreground space-y-2 ml-5 list-disc">
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">fact_dz_device_link_latency</code> — Per-second latency and loss measurements from network probes</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">fact_dz_device_interface_counters</code> — Interface error and discard counters</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">dz_links_current</code> — Current link configuration and status</li>
            <li><code className="bg-muted px-1 py-0.5 rounded text-xs">dim_dz_links_history</code> — Historical link status for drain detection</li>
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
