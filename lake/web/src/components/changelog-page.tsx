import { FileText } from 'lucide-react'

interface ChangelogEntry {
  date: string
  changes: {
    type: 'feature' | 'improvement' | 'fix'
    description: string
  }[]
}

const changelog: ChangelogEntry[] = [
  {
    date: 'January 22, 2026',
    changes: [
      { type: 'feature', description: 'IS-IS delay override tracking in link timeline' },
      { type: 'improvement', description: 'Zoom and pan to selected items when loading topology from URL' },
      { type: 'improvement', description: 'Suggested questions refresh when clicking Chat in navigation' },
      { type: 'improvement', description: 'Grace period before showing update notifications' },
      { type: 'fix', description: 'Link removal panel now shows latency impact first' },
      { type: 'fix', description: 'Blank page when navigating to new query sessions' },
      { type: 'fix', description: 'Query progress spinner not updating to completed state' },
    ],
  },
  {
    date: 'January 21, 2026',
    changes: [
      { type: 'feature', description: 'Soft drained links list in status page header' },
      { type: 'feature', description: 'Pin toggle for current outages on outages page' },
      { type: 'improvement', description: 'Transpacific links draw across the Pacific Ocean on topology map' },
      { type: 'improvement', description: 'Better zoom behavior when selecting links on topology' },
      { type: 'improvement', description: 'Wider name column on links status history table' },
      { type: 'improvement', description: 'Metro and contributor shown on device utilization cards' },
      { type: 'improvement', description: 'Sidebar no longer auto-collapses in topology unless already collapsed' },
      { type: 'fix', description: 'Sidebar state pollution from topology auto-collapse' },
      { type: 'fix', description: 'Issue thresholds for errors and discards' },
    ],
  },
  {
    date: 'January 14, 2026',
    changes: [
      { type: 'feature', description: 'DZ vs Internet performance comparison' },
      { type: 'feature', description: 'Path latency analysis' },
      { type: 'feature', description: 'Maintenance planner' },
      { type: 'feature', description: 'Redundancy analysis for network resilience' },
      { type: 'feature', description: 'Metro connectivity report' },
    ],
  },
  {
    date: 'January 10, 2026',
    changes: [
      { type: 'feature', description: 'Network topology map and graph visualization' },
      { type: 'feature', description: 'Path calculator for analyzing network routes' },
    ],
  },
  {
    date: 'January 6, 2026',
    changes: [
      { type: 'feature', description: 'Neo4j graph database support' },
      { type: 'feature', description: 'Cypher query support' },
    ],
  },
  {
    date: 'January 2, 2026',
    changes: [
      { type: 'feature', description: 'Outages tracker' },
    ],
  },
  {
    date: 'December 30, 2025',
    changes: [
      { type: 'feature', description: 'Network status dashboard' },
      { type: 'feature', description: 'Event timeline' },
      { type: 'feature', description: 'Device, link, metro, and contributor browsers' },
      { type: 'feature', description: 'Solana validator and gossip node browsers' },
    ],
  },
  {
    date: 'December 26, 2025',
    changes: [
      { type: 'feature', description: 'AI-powered chat for natural language network queries' },
      { type: 'feature', description: 'SQL query editor' },
    ],
  },
  {
    date: 'December 24, 2025',
    changes: [
      { type: 'feature', description: 'Slack bot interface for AI agent' },
    ],
  },
  {
    date: 'December 23, 2025',
    changes: [
      { type: 'feature', description: 'ClickHouse data indexer for DZ network telemetry' },
      { type: 'feature', description: 'Solana RPC data ingestion' },
      { type: 'feature', description: 'AI agent with tool-calling for natural language queries' },
    ],
  },
]

function ChangeTypeBadge({ type }: { type: 'feature' | 'improvement' | 'fix' }) {
  const styles = {
    feature: 'bg-green-500/10 text-green-500 border-green-500/20',
    improvement: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
    fix: 'bg-orange-500/10 text-orange-500 border-orange-500/20',
  }

  const labels = {
    feature: 'New',
    improvement: 'Improved',
    fix: 'Fixed',
  }

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 text-xs font-medium rounded border ${styles[type]}`}
    >
      {labels[type]}
    </span>
  )
}

export function ChangelogPage() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-3xl mx-auto px-6 py-8">
        <div className="flex items-center gap-3 mb-8">
          <div className="p-2 rounded-lg bg-primary/10">
            <FileText className="h-6 w-6 text-primary" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold">Changelog</h1>
            <p className="text-sm text-muted-foreground">What's new</p>
          </div>
        </div>

        <div className="space-y-10">
          {changelog.map((entry) => (
            <div key={entry.date}>
              <h2 className="text-lg font-semibold mb-4">{entry.date}</h2>
              <div className="space-y-3 pl-4 border-l-2 border-border">
                {entry.changes.map((change, i) => (
                  <div key={i} className="flex items-start gap-3">
                    <ChangeTypeBadge type={change.type} />
                    <span className="text-sm text-foreground/90">{change.description}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
