# Topology Explorer UI Plan

## Overview

Add an interactive graph visualization to the existing topology page, complementing the current MapLibre geo map. The graph view helps users understand logical network structure—routing paths, adjacencies, and topology health—while the geo map shows physical locations.

## Goals

1. Add abstract graph view (Cytoscape.js) alongside existing geo map
2. Enable path exploration between devices
3. Surface topology anomalies (configured vs discovered mismatches)
4. Guide users with suggested questions and exploration patterns

## Existing Infrastructure

### Current State

The topology feature already exists:

- **API**: `api/handlers/topology.go` - serves metros, devices, links, validators from ClickHouse
- **Frontend**: `web/src/components/topology-page.tsx` and `topology-map.tsx` - MapLibre geo visualization
- **Data**: TanStack Query with 60s refresh interval

### What's Missing

The current implementation shows physical topology on a map but lacks:
- Abstract graph view for understanding logical connectivity
- Path finding between devices
- Topology health comparison (configured vs ISIS adjacencies)
- Failure impact analysis

### Data Sources

**ClickHouse (existing)**: Metros, devices, links, validators, traffic stats
**Neo4j (new)**: ISIS adjacencies, routing paths, topology comparison

The Neo4j graph store exposes query methods in `indexer/pkg/dz/graph/query.go`:

```go
// Core topology
ISISTopology(ctx)                                    // Full graph
ISISAdjacencies(ctx, devicePK)                       // Device's neighbors
NetworkAroundDevice(ctx, devicePK, hops)             // N-hop subgraph

// Path analysis
ShortestPath(ctx, from, to, weightBy)                // Weighted shortest path
ShortestPathByISISMetric(ctx, from, to)              // ISIS metric path
ExplainRoute(ctx, from, to)                          // Detailed breakdown

// Topology health
CompareTopology(ctx)                                 // Configured vs discovered
UnreachableIfDown(ctx, devicePK, maxHops)            // Failure impact
ReachableFromMetro(ctx, metroPK, activeOnly)         // Metro reachability
```

---

## UI Components

### 1. Main Graph Canvas

The primary view is a force-directed graph layout.

**Nodes (Devices):**
- Shape: Circle
- Size: Based on degree (number of adjacencies)
- Color: By status (green=online, red=offline) or by contributor (categorical)
- Label: Device code
- Tooltip: system_id, router_id, metro, contributor

**Edges (ISIS_ADJACENT relationships):**
- Thickness: Inverse of ISIS metric (lower metric = thicker line)
- Color:
  - Green = healthy (adjacency matches configured link)
  - Orange = adjacency exists but no configured link
  - Red dashed = configured link but no adjacency (problem!)
- Label (on hover): metric value, RTT
- Animation: Optional pulse for active/recent adjacencies

**Layout Options:**
- Force-directed (default) - best for seeing cluster structure
- Hierarchical - if there's a clear core/edge pattern
- Geographic - switch to MapLibre view with devices at metro coords

### 2. Toolbar / Controls

```
┌─────────────────────────────────────────────────────────────────┐
│ [View: Graph ▼] [Layout: Force ▼] [Color by: Status ▼]         │
│ [Filter: Contributor ▼] [Metro ▼] [Status ▼]                   │
│ [🔍 Search device...]                              [⚙️ Settings]│
└─────────────────────────────────────────────────────────────────┘
```

### 3. Mode Selector

Three primary interaction modes:

#### Explore Mode (default)
- Pan/zoom the graph
- Click node → show details in side panel
- Click edge → show link details
- Drag nodes to rearrange

#### Path Mode
- Click first device (highlighted as "source")
- Click second device (highlighted as "destination")
- Show shortest path highlighted on graph
- Weight selector: Hops | ISIS Metric | RTT | Bandwidth

#### Compare Mode
- Overlay view showing topology health
- Highlight mismatches between configured links and ISIS adjacencies
- Summary stats: X matched, Y missing adjacencies, Z unexpected adjacencies

### 4. Side Panel

Contextual details based on selection:

**When nothing selected:**
```
┌─────────────────────────────┐
│ Topology Summary            │
│ ─────────────────────────── │
│ Devices: 142 (138 online)   │
│ Adjacencies: 387            │
│ Metros: 12                  │
│ Contributors: 8             │
│                             │
│ Health                      │
│ ─────────────────────────── │
│ ✓ 382 links match config    │
│ ⚠ 3 missing adjacencies     │
│ ⚠ 2 unexpected adjacencies  │
│                             │
│ [View Issues →]             │
└─────────────────────────────┘
```

**When device selected:**
```
┌─────────────────────────────┐
│ Device: nyc-edge-01         │
│ ─────────────────────────── │
│ Status: 🟢 Online           │
│ Type: edge                  │
│ Metro: NYC                  │
│ Contributor: Acme Networks  │
│                             │
│ ISIS Identity               │
│ ─────────────────────────── │
│ System ID: 0000.0000.0001   │
│ Router ID: 10.0.0.1         │
│ Last Sync: 2 min ago        │
│                             │
│ Adjacencies (4)             │
│ ─────────────────────────── │
│ → nyc-core-01  metric: 10   │
│ → nyc-core-02  metric: 10   │
│ → ewr-edge-01  metric: 50   │
│ → jfk-edge-01  metric: 30   │
│                             │
│ [Failure Impact Analysis →] │
│ [Show N-hop neighborhood →] │
└─────────────────────────────┘
```

**When edge selected:**
```
┌─────────────────────────────┐
│ Link: nyc-edge-01 ↔ nyc-... │
│ ─────────────────────────── │
│ ISIS Metric: 10             │
│ Adj SIDs: [16001, 16002]    │
│ Last Seen: 30s ago          │
│                             │
│ Configured Link             │
│ ─────────────────────────── │
│ Code: link-nyc-001          │
│ Status: 🟢 Active           │
│ Bandwidth: 10 Gbps          │
│ Committed RTT: 2ms          │
│ Tunnel Net: 10.255.0.0/31   │
└─────────────────────────────┘
```

**When path selected:**
```
┌─────────────────────────────┐
│ Path: nyc-edge-01 → lax-... │
│ ─────────────────────────── │
│ Weight: ISIS Metric         │
│ Total Cost: 180             │
│ Hops: 5                     │
│                             │
│ Route                       │
│ ─────────────────────────── │
│ 1. nyc-edge-01              │
│    ↓ metric: 10             │
│ 2. nyc-core-01              │
│    ↓ metric: 40             │
│ 3. chi-core-01              │
│    ↓ metric: 40             │
│ 4. lax-core-01              │
│    ↓ metric: 10             │
│ 5. lax-edge-01              │
│                             │
│ [Compare by RTT →]          │
│ [Compare by Bandwidth →]    │
└─────────────────────────────┘
```

### 5. Guided Questions Panel

This is a key UX element. Users often don't know what questions to ask. Provide contextual suggestions:

**Global suggestions (always visible, collapsible):**
```
┌─────────────────────────────────────────────────────────────────┐
│ 💡 Explore your topology                                        │
│ ─────────────────────────────────────────────────────────────── │
│                                                                 │
│ Understand Structure                                            │
│ • Which devices have the most connections?                      │
│ • Are there any single points of failure?                       │
│ • How many hops between my furthest devices?                    │
│                                                                 │
│ Check Health                                                    │
│ • Are all configured links showing ISIS adjacencies?            │
│ • Which devices have degraded connectivity?                     │
│ • Are there any unexpected adjacencies?                         │
│                                                                 │
│ Plan & Troubleshoot                                             │
│ • What's the best path between two devices?                     │
│ • What happens if device X goes down?                           │
│ • Which devices can reach metro Y?                              │
└─────────────────────────────────────────────────────────────────┘
```

Each question is clickable and triggers the appropriate action:
- "Which devices have the most connections?" → Sorts/highlights by degree
- "Are there any single points of failure?" → Runs articulation point analysis
- "Are all configured links showing ISIS adjacencies?" → Enters Compare Mode

**Contextual suggestions (based on current selection):**

When a device is selected:
```
│ 💡 About nyc-edge-01:                                           │
│ • What devices would lose connectivity if this goes down?       │
│ • Show all devices within 3 hops                                │
│ • Find shortest path to another device                          │
```

When viewing a path:
```
│ 💡 About this path:                                             │
│ • Would a different metric give a shorter path?                 │
│ • Are there alternative equal-cost paths?                       │
│ • Which link on this path has the worst latency?                │
```

When anomalies are detected:
```
│ ⚠️ 3 issues detected:                                           │
│ • 2 links configured but no ISIS adjacency                      │
│   [Investigate →]                                               │
│ • 1 adjacency with no configured link                           │
│   [Investigate →]                                               │
```

---

## API Endpoints

New endpoints in `api/handlers/isis.go`:

```
GET /api/topology/isis
    → Full ISIS topology graph (nodes + edges)
    → Response: { nodes: [...], edges: [...] }

GET /api/topology/isis/device/{pk}/neighborhood?hops=2
    → N-hop subgraph around device
    → Response: { nodes: [...], edges: [...] }

GET /api/topology/path?from={pk}&to={pk}&weight=isis_metric
    → Shortest path between devices
    → weight: hops | isis_metric | rtt | bandwidth
    → Response: { path: [...], totalCost: N, details: [...] }

GET /api/topology/compare
    → Topology health comparison (configured links vs ISIS adjacencies)
    → Response: { matched: [...], missingAdjacencies: [...], unexpectedAdjacencies: [...] }

GET /api/topology/impact/{devicePK}?maxHops=5
    → Failure impact analysis
    → Response: { unreachableDevices: [...], affectedUsers: N }
```

**Note**: Existing endpoints already cover devices (`/api/devices`) and metros (`/api/metros`).

### Neo4j Connection

The API connects directly to Neo4j (same pattern as ClickHouse connection in `api/config/`). Add:

```go
// api/config/neo4j.go
var Neo4jDriver neo4j.DriverWithContext

func InitNeo4j() error {
    driver, err := neo4j.NewDriverWithContext(
        os.Getenv("NEO4J_URI"),
        neo4j.BasicAuth(os.Getenv("NEO4J_USER"), os.Getenv("NEO4J_PASSWORD"), ""),
    )
    // ...
}
```

The graph query logic from `indexer/pkg/dz/graph/query.go` can be adapted for use in the API handlers.

---

## Implementation Phases

### Phase 1: Graph View Foundation

**Backend** (`api/`):
- [ ] Add Neo4j driver to `api/config/` (same pattern as ClickHouse)
- [ ] Create `handlers/isis.go` with `GET /api/topology/isis` endpoint
- [ ] Port graph query logic from `indexer/pkg/dz/graph/query.go`
- [ ] Return ISIS topology as Cytoscape-compatible JSON

**Frontend** (`web/src/components/`):
- [ ] Add Cytoscape.js dependency (`bun add cytoscape @types/cytoscape`)
- [ ] Create `topology-graph.tsx` component with basic graph rendering
- [ ] Add view toggle (Graph | Map) to `topology-page.tsx`
- [ ] Implement pan/zoom and node click → show details in side panel

### Phase 2: Core Graph Features

**Frontend**:
- [ ] Node styling: size by degree, color by status/contributor
- [ ] Edge styling: thickness by metric, color by health
- [ ] Filter controls matching existing map filters (contributor, metro, status)
- [ ] Search box with device autocomplete
- [ ] Linked selection: click in graph highlights on map and vice versa

### Phase 3: Path Finding

**Backend**:
- [ ] Add `GET /api/topology/path` endpoint
- [ ] Support weight options: hops, isis_metric, rtt, bandwidth

**Frontend**:
- [ ] Path mode: click source → click destination → show path
- [ ] Highlight path on graph with step-by-step details in side panel
- [ ] Weight selector dropdown

### Phase 4: Topology Health

**Backend**:
- [ ] Add `GET /api/topology/compare` endpoint
- [ ] Add `GET /api/topology/impact/{pk}` endpoint

**Frontend**:
- [ ] Compare mode: overlay showing configured vs discovered
- [ ] Color edges by match status (green=match, orange=unexpected, red=missing)
- [ ] Summary stats panel: X matched, Y issues
- [ ] Failure impact view: "what if this device goes down?"

### Phase 5: Guided Experience

**Frontend**:
- [ ] Suggested questions panel (collapsible)
- [ ] Contextual suggestions based on current selection
- [ ] Click-to-action: each question triggers the relevant analysis
- [ ] Issue detection: surface anomalies proactively

### Phase 6: Polish

- [ ] Performance optimization for large graphs (virtualization, clustering)
- [ ] URL state sync for shareable views
- [ ] Keyboard shortcuts (Esc to deselect, arrow keys, etc.)
- [ ] Export graph as PNG/SVG

---

## Graph + Map Integration

The existing `topology-map.tsx` already renders metros, devices, links, and validators on a MapLibre map. The new graph view complements it:

| Graph View (new) | Map View (existing) |
|------------------|---------------------|
| Logical topology structure | Physical location |
| "How is the network connected?" | "Where are things?" |
| Routing paths by ISIS metric | Device locations by metro |
| Adjacency health | Traffic and latency stats |

### Integration Approach

#### Shared State

Both views share selection and filter state via React context or URL params:

- **Selected device/link**: Click in either view updates both
- **Filters**: Contributor, metro, status filters apply to both views
- **Mode**: Explore/Path/Compare mode applies to graph; map stays in explore mode

#### Linked Selection

- Click device in graph → map flies to that device's metro, highlights marker
- Click device on map → graph centers on that node, highlights it
- Path calculated in graph → optionally overlay path on map as highlighted line

#### View Toggle

Add toggle in `topology-page.tsx` toolbar:

```tsx
<div className="flex gap-2">
  <button onClick={() => setView('map')} className={view === 'map' ? 'active' : ''}>
    <Globe /> Map
  </button>
  <button onClick={() => setView('graph')} className={view === 'graph' ? 'active' : ''}>
    <Network /> Graph
  </button>
  <button onClick={() => setView('split')} className={view === 'split' ? 'active' : ''}>
    <Columns /> Split
  </button>
</div>
```

### Shared State Interface

```typescript
interface ViewState {
  // Selection
  selectedDevicePK: string | null;
  selectedLinkPK: string | null;

  // Filters (apply to both views)
  filters: {
    contributors: string[];
    metros: string[];
    statuses: string[];
    deviceTypes: string[];
  };

  // Mode
  mode: 'explore' | 'path' | 'compare';

  // Path mode state
  pathSource: string | null;
  pathDestination: string | null;
  pathWeight: 'hops' | 'isis_metric' | 'rtt' | 'bandwidth';
  calculatedPath: string[] | null;
}
```

Filters, selection, and path state shared so both views stay coherent.

---

## Open Questions

1. **Scale**: How many devices/links typical? May need lazy loading or clustering.
2. **Real-time**: Poll (current 60s interval) or WebSocket for live updates?
3. **History**: Show topology changes over time, or just current state?

---

## File Structure

```
api/
  config/
    db.go                # Existing - ClickHouse connection
    neo4j.go             # New - Neo4j connection
  handlers/
    topology.go          # Existing - ClickHouse queries for map
    isis.go              # New - Neo4j graph endpoints

web/src/
  components/
    topology-page.tsx    # Existing - add view toggle
    topology-map.tsx     # Existing - MapLibre geo view
    topology-graph.tsx   # New - Cytoscape.js graph view
    topology-sidebar.tsx # New - shared side panel for details
  lib/
    api.ts               # Add new ISIS topology fetch functions
  hooks/
    use-topology-state.ts # New - shared state for selection/filters
```

---

## Appendix: Cytoscape.js Data Format

```typescript
interface ISISTopologyResponse {
  nodes: Array<{
    data: {
      id: string;              // device PK
      label: string;           // device code
      status: string;
      deviceType: string;
      metroPK: string;
      contributorPK: string;
      isisSystemId?: string;
      isisRouterId?: string;
      degree?: number;         // number of adjacencies
    }
  }>;
  edges: Array<{
    data: {
      id: string;              // adjacency ID
      source: string;          // from device PK
      target: string;          // to device PK
      isisMetric?: number;
      adjSids?: number[];
      lastSeen?: string;
      hasConfiguredLink: boolean;
      configuredLinkCode?: string;
    }
  }>;
}
```
