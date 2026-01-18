import { useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight, Keyboard, Command } from 'lucide-react'
import { cn } from '@/lib/utils'

interface CollapsibleSectionProps {
  title: string
  defaultOpen?: boolean
  children: React.ReactNode
}

function CollapsibleSection({ title, defaultOpen = false, children }: CollapsibleSectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen)

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between px-4 py-3 bg-secondary hover:bg-muted transition-colors text-left"
      >
        <span className="font-medium">{title}</span>
        {isOpen ? (
          <ChevronDown className="h-4 w-4 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-4 w-4 text-muted-foreground" />
        )}
      </button>
      {isOpen && (
        <div className="px-4 py-4 border-t border-border">
          {children}
        </div>
      )}
    </div>
  )
}

function KeyboardShortcut({ keys, description }: { keys: string[]; description: string }) {
  return (
    <div className="flex items-center justify-between py-2">
      <span className="text-muted-foreground">{description}</span>
      <div className="flex items-center gap-1">
        {keys.map((key, i) => (
          <span key={i}>
            <kbd className="px-2 py-1 text-xs bg-muted border border-border rounded font-mono">
              {key}
            </kbd>
            {i < keys.length - 1 && <span className="text-muted-foreground mx-1">+</span>}
          </span>
        ))}
      </div>
    </div>
  )
}

export function HelpPage() {
  return (
    <div className="flex-1 overflow-auto">
      <div className="max-w-3xl mx-auto px-8 py-12">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-2xl font-semibold mb-2">Help</h1>
          <p className="text-muted-foreground">
            A data analytics platform for the DoubleZero network, providing real-time insights
            into network infrastructure, Solana validators, and traffic patterns through an AI-powered
            chat interface and direct SQL queries.
          </p>
        </div>

        {/* Quick Reference */}
        <div className="mb-8">
          <CollapsibleSection title="Keyboard Shortcuts" defaultOpen>
            <div className="space-y-1 divide-y divide-border">
              <div className="pb-2">
                <h4 className="text-sm font-medium mb-2 flex items-center gap-2">
                  <Keyboard className="h-4 w-4" />
                  Global
                </h4>
                <KeyboardShortcut keys={['\u2318', 'K']} description="Open search spotlight" />
                <KeyboardShortcut keys={['?']} description="Open help page" />
              </div>
              <div className="py-2">
                <h4 className="text-sm font-medium mb-2 flex items-center gap-2">
                  <Command className="h-4 w-4" />
                  Query Editor
                </h4>
                <KeyboardShortcut keys={['\u2318', 'Enter']} description="Run query" />
              </div>
              <div className="py-2">
                <h4 className="text-sm font-medium mb-2">Chat</h4>
                <KeyboardShortcut keys={['Enter']} description="Send message" />
                <KeyboardShortcut keys={['Shift', 'Enter']} description="New line" />
              </div>
              <div className="pt-2">
                <h4 className="text-sm font-medium mb-2">Topology (Graph View)</h4>
                <KeyboardShortcut keys={['Escape']} description="Exit mode / close drawer" />
                <KeyboardShortcut keys={['p']} description="Toggle path finding mode" />
                <KeyboardShortcut keys={['c']} description="Toggle topology compare mode" />
                <KeyboardShortcut keys={['r']} description="Toggle What-If link removal mode" />
                <KeyboardShortcut keys={['a']} description="Toggle What-If link addition mode" />
                <KeyboardShortcut keys={['f']} description="Focus search" />
                <KeyboardShortcut keys={['?']} description="Toggle guide panel" />
              </div>
            </div>
          </CollapsibleSection>
        </div>

        {/* Pages & Features */}
        <div className="space-y-4">
          <h2 className="text-lg font-semibold mb-4">Pages & Features</h2>

          <CollapsibleSection title="Home">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/" className="text-accent hover:underline">home page</Link> provides
                a quick overview of the DoubleZero network.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Network statistics dashboard showing contributors, metros, devices, and links</li>
                <li>Solana validator statistics including stake share</li>
                <li>Quick chat input for asking questions about the network</li>
                <li>Example questions to get started</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Chat">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/chat" className="text-accent hover:underline">chat interface</Link> lets
                you ask natural language questions about network data.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Ask questions like "How is the network doing?" or "Which validators have the most stake?"</li>
                <li>AI generates and executes SQL queries to answer your questions</li>
                <li>View query results as tables or charts</li>
                <li>Multi-turn conversations maintain context</li>
                <li>Click "Edit" on any query to open it in the Query Editor</li>
                <li>Follow-up questions are suggested based on context</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Query Editor">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/query" className="text-accent hover:underline">query editor</Link> provides
                direct SQL access to network data.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Write and run SQL queries directly against ClickHouse</li>
                <li>Schema autocomplete helps you discover tables and columns</li>
                <li>Generate SQL from natural language descriptions</li>
                <li>Visualize results as tables or charts (bar, line, pie, area, scatter)</li>
                <li>Query history tracks your past queries</li>
                <li>Sessions persist locally in your browser</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Topology / Map">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/topology/map" className="text-accent hover:underline">topology map</Link> shows
                a geographic view of network infrastructure.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li><strong>Validators mode:</strong> Show Solana validator locations on the map</li>
                <li><strong>Path Finding mode:</strong> Calculate and visualize routes between devices</li>
                <li><strong>Criticality mode:</strong> Highlight single points of failure in the network</li>
                <li><strong>What-If Removal:</strong> Simulate removing a link to see impact on connectivity</li>
                <li><strong>What-If Addition:</strong> Simulate adding a new link to see path improvements</li>
                <li>Color coding: Blue = healthy, Yellow = degraded, Red = critical</li>
                <li>Click on devices or links for detailed information</li>
                <li>Pan and zoom to explore different regions</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Topology / Graph">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/topology/graph" className="text-accent hover:underline">topology graph</Link> provides
                a force-directed network topology view.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Interactive node and link visualization</li>
                <li>Same modes as map view: Validators, Path Finding, Criticality, What-If simulation</li>
                <li>Drag nodes to rearrange the layout</li>
                <li>Compare mode for topology analysis</li>
                <li>Keyboard shortcuts for quick mode switching (p, c, r, a)</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Topology / Path Calculator">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/topology/path-calculator" className="text-accent hover:underline">path calculator</Link> finds
                multiple paths between devices.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Select source and destination devices</li>
                <li>View multiple alternative paths</li>
                <li>Shows hop count and latency for each path</li>
                <li>Detailed route breakdown with intermediate devices</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Topology / Redundancy Report">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/topology/redundancy" className="text-accent hover:underline">redundancy report</Link> identifies
                single points of failure.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Leaf devices: Devices with only one connection</li>
                <li>Critical links: Links whose failure would partition the network</li>
                <li>Single-exit metros: Metros with only one external connection</li>
                <li>Risk assessment for network planning</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Topology / Metro Matrix">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/topology/metro-matrix" className="text-accent hover:underline">metro matrix</Link> shows
                connectivity between all metros in an NxN grid.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Path count between each metro pair</li>
                <li>Minimum hops and latency for each connection</li>
                <li>Color-coded by connectivity strength (green/yellow/red)</li>
                <li>Click cells for detailed connectivity info</li>
                <li>Export to CSV for analysis</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Status">
            <div className="space-y-3 text-sm">
              <p>
                The <Link to="/status" className="text-accent hover:underline">status page</Link> shows
                network health at a glance.
              </p>
              <ul className="list-disc list-inside space-y-1 text-muted-foreground">
                <li>Network health dashboard with overall status</li>
                <li>Link and device status timelines</li>
                <li>Historical health data and trends</li>
                <li>Filter by time range and status</li>
              </ul>
            </div>
          </CollapsibleSection>

          <CollapsibleSection title="Entity Pages">
            <div className="space-y-3 text-sm">
              <p>
                Browse and explore network entities in detail.
              </p>
              <div className="mt-2 space-y-2">
                <p className="font-medium">DoubleZero</p>
                <ul className="list-disc list-inside space-y-1 text-muted-foreground ml-2">
                  <li><Link to="/dz/devices" className="text-accent hover:underline">Devices</Link> - Network devices (routers, switches)</li>
                  <li><Link to="/dz/links" className="text-accent hover:underline">Links</Link> - Physical connections between devices</li>
                  <li><Link to="/dz/metros" className="text-accent hover:underline">Metros</Link> - Geographic locations</li>
                  <li><Link to="/dz/contributors" className="text-accent hover:underline">Contributors</Link> - Infrastructure providers</li>
                  <li><Link to="/dz/users" className="text-accent hover:underline">Users</Link> - Network users and their connections</li>
                </ul>
                <p className="font-medium mt-4">Solana</p>
                <ul className="list-disc list-inside space-y-1 text-muted-foreground ml-2">
                  <li><Link to="/solana/validators" className="text-accent hover:underline">Validators</Link> - Solana validators connected to DZ</li>
                  <li><Link to="/solana/gossip-nodes" className="text-accent hover:underline">Gossip Nodes</Link> - Solana gossip network participants</li>
                </ul>
              </div>
            </div>
          </CollapsibleSection>
        </div>

        {/* Tips & Tricks */}
        <div className="mt-8">
          <h2 className="text-lg font-semibold mb-4">Tips & Tricks</h2>
          <div className={cn(
            "p-4 rounded-lg border border-border bg-secondary",
            "space-y-3 text-sm text-muted-foreground"
          )}>
            <ul className="list-disc list-inside space-y-2">
              <li>
                <strong className="text-foreground">Search spotlight</strong> ({'\u2318'}K) searches across all
                entities, pages, and recent sessions
              </li>
              <li>
                <strong className="text-foreground">Cmd+Click</strong> on links or session items to open in a new tab
              </li>
              <li>
                <strong className="text-foreground">Sessions auto-save</strong> to your browser's local storage
              </li>
              <li>
                <strong className="text-foreground">Theme</strong> follows your system preference by default,
                or set manually using the toggle in the sidebar
              </li>
              <li>
                <strong className="text-foreground">Ask about results</strong> in the query editor to start a
                chat conversation about your data
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  )
}
