# Topology Explorer UI Plan

## Overview

A web-based visualization tool for exploring IS-IS network topology using data from the Neo4j graph database. The primary focus is helping users understand routing and topology structure through interactive graph visualization.

## Goals

1. Visualize the IS-IS topology as an interactive graph
2. Enable path exploration between devices
3. Surface topology anomalies (configured vs discovered mismatches)
4. Guide users with suggested questions and exploration patterns

## Tech Stack

| Layer | Technology |
|-------|------------|
| Framework | React 18+ with TypeScript |
| Build | Vite |
| Graph Visualization | Cytoscape.js (or vis.js) |
| Geographic View | MapLibre GL JS (secondary view) |
| State Management | TanStack Query (for API data) |
| Styling | Tailwind CSS |
| API Layer | REST endpoints from lake/indexer |

## Data Source

The Neo4j graph store already exposes these query methods in `lake/indexer/pkg/dz/graph/query.go`:

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

**Required**: Expose these as REST endpoints (see API section below).

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

New REST endpoints needed in `lake/indexer/pkg/server/`:

```
GET /api/v1/topology/isis
    → Full ISIS topology graph (nodes + edges)
    → Response: { nodes: [...], edges: [...] }

GET /api/v1/topology/isis/device/{pk}
    → Single device with its adjacencies
    → Response: { device: {...}, adjacencies: [...] }

GET /api/v1/topology/isis/device/{pk}/neighborhood?hops=2
    → N-hop subgraph around device
    → Response: { nodes: [...], edges: [...] }

GET /api/v1/topology/path?from={pk}&to={pk}&weight=isis_metric
    → Shortest path between devices
    → weight: hops | isis_metric | rtt | bandwidth
    → Response: { path: [...], totalCost: N, details: [...] }

GET /api/v1/topology/compare
    → Topology health comparison
    → Response: { matched: [...], missingAdjacencies: [...], unexpectedAdjacencies: [...] }

GET /api/v1/topology/impact/{devicePK}?maxHops=5
    → Failure impact analysis
    → Response: { unreachableDevices: [...], affectedUsers: N }

GET /api/v1/devices
    → List all devices (for search/filter)
    → Query params: status, contributor, metro, type

GET /api/v1/metros
    → List all metros with coordinates
    → Response: [{ pk, code, name, lat, lng }, ...]
```

---

## Implementation Phases

### Phase 1: Foundation
- [ ] Set up React/Vite/TypeScript project structure
- [ ] Add Cytoscape.js and basic graph rendering
- [ ] Implement REST API endpoints for topology data
- [ ] Basic graph display with pan/zoom
- [ ] Device click → side panel details

### Phase 2: Core Features
- [ ] Node/edge styling (color, size based on properties)
- [ ] Filter controls (contributor, metro, status)
- [ ] Search functionality
- [ ] Path mode with shortest path visualization
- [ ] Weight selector for path calculation

### Phase 3: Topology Health
- [ ] Compare mode (configured vs discovered)
- [ ] Anomaly highlighting
- [ ] Failure impact analysis view
- [ ] Summary statistics panel

### Phase 4: Guided Experience
- [ ] Suggested questions panel
- [ ] Contextual suggestions based on selection
- [ ] Click-to-action for each suggestion
- [ ] Issue detection and investigation flows

### Phase 5: Polish & Geographic View
- [ ] MapLibre integration for geo view
- [ ] Toggle between graph/map layouts
- [ ] Performance optimization for large graphs
- [ ] Export/share functionality

---

## Geo Map Integration

Since there's already a MapLibre geo map in the app, the topology explorer can integrate with it rather than replace it. The two views answer different questions:

| Abstract Graph | Geo Map |
|----------------|---------|
| Logical topology structure | Physical location |
| "How is the network connected?" | "Where are things?" |
| Routing paths by metric | Routing paths by geography |
| Cluster/density of connections | Regional distribution |

### Integration Patterns

#### 1. Linked Selection (Cross-Highlighting)
When user selects a device in either view, highlight it in both:
- Select device on map → pulse/highlight same node in graph view
- Select device in graph → fly-to and highlight on map
- Enables quick context switching: "I see this device is in NYC, what's its logical connectivity?"

#### 2. Metro Aggregation Layer (Map)
Add a topology layer to the existing map:

```
┌─────────────────────────────────────────┐
│                   Map                   │
│                                         │
│     ○ SEA                               │
│      \                                  │
│       \_____ ○ CHI ───── ○ NYC          │
│              / \          |             │
│     ○ LAX __/   \        |             │
│      \           ○ DFW ──┘             │
│       \_________/                       │
│                                         │
│  ○ = Metro (size = device count)        │
│  ─ = Links between metros               │
└─────────────────────────────────────────┘
```

- **Metro nodes**: Circles sized by device count, colored by health (% online)
- **Metro links**: Lines showing aggregate connectivity between metros
  - Thickness = number of links or total bandwidth
  - Color = health (green if all adjacencies up, red if issues)
- **Click metro** → zoom in to show individual devices, or filter graph view to that metro

#### 3. Path Visualization on Map
When a path is calculated in the graph view, optionally show it on the map:
- Draw the geographic route as a highlighted line
- Shows "logical path goes NYC → CHI → LAX" overlaid on geography
- Useful for understanding if traffic takes a geographically sensible route

#### 4. Regional Health Overlay (Map)
Heat map or status indicators by region:
- Color metros by health status
- Show mini-badges: "3 devices, 2 issues"
- Quick visual scan for regional problems

#### 5. Geo-Aware Suggested Questions
When viewing the map, surface location-relevant questions:

```
💡 Geographic Questions
• Which metros have the most devices?
• What's the furthest geographic path in the network?
• Are there any isolated metros (single link)?
• Show all devices within 100ms RTT of NYC
```

### Map-Specific Features

**Device Markers** (when zoomed into a metro):
```
┌──────────────────────────────────────────┐
│ NYC Metro (zoomed)                       │
│                                          │
│         🟢 nyc-core-01                   │
│        /   \                             │
│   🟢 nyc-edge-01   🟢 nyc-edge-02        │
│        \   /                             │
│         🟢 nyc-core-02                   │
│                                          │
│  🟢 = online  🔴 = offline  🟡 = issues  │
└──────────────────────────────────────────┘
```

**Link Lines on Map**:
- Straight lines or great-circle arcs between connected devices
- Styled similarly to graph edges (color by health, thickness by metric)
- Toggle visibility to reduce clutter

**Cluster Expansion**:
- At low zoom: show metros as single markers with count badges
- At medium zoom: show device clusters
- At high zoom: show individual devices with connections

### Sync State Between Views

Keep both views in sync:

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

### Suggested UI Layout

**Option A: Side-by-side**
```
┌────────────────────┬────────────────────┐
│                    │                    │
│   Abstract Graph   │     Geo Map        │
│                    │                    │
└────────────────────┴────────────────────┘
```

**Option B: Tabbed/Toggle**
```
┌─────────────────────────────────────────┐
│ [Graph View] [Map View]                 │
├─────────────────────────────────────────┤
│                                         │
│         (current view)                  │
│                                         │
└─────────────────────────────────────────┘
```

**Option C: Map with Graph Overlay Panel**
```
┌─────────────────────────────────────────┐
│  Map (full width)          ┌──────────┐ │
│                            │ Graph    │ │
│    ○ SEA                   │ (mini)   │ │
│     \                      │          │ │
│      ○ CHI ── ○ NYC        │  ◯──◯    │ │
│                            │  |  |    │ │
│                            │  ◯──◯    │ │
│                            └──────────┘ │
└─────────────────────────────────────────┘
```

Option C works well if the map is the primary navigation and graph is supplementary.

---

## Open Questions

1. **Scale**: How many devices/links typical? Affects rendering strategy.
2. **Real-time**: Should topology updates stream live, or poll/refresh?
3. **Permissions**: Any RBAC needed (view-only vs admin)?
4. **History**: Show topology changes over time, or just current state?
5. **Integration**: Embed in existing app or standalone?

---

## Appendix: Graph Data Format

Cytoscape.js expected format:

```typescript
interface TopologyResponse {
  nodes: Array<{
    data: {
      id: string;          // device PK
      label: string;       // device code
      status: string;
      deviceType: string;
      metro: string;
      contributor: string;
      isisSystemId?: string;
      isisRouterId?: string;
      // For geo view
      lat?: number;
      lng?: number;
    }
  }>;
  edges: Array<{
    data: {
      id: string;          // edge ID
      source: string;      // from device PK
      target: string;      // to device PK
      isisMetric?: number;
      adjSids?: number[];
      lastSeen?: string;
      // For comparison
      hasConfiguredLink: boolean;
      configuredLinkCode?: string;
    }
  }>;
}
```
