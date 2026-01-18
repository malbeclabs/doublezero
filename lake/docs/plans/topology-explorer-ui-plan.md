# Topology Explorer UI Plan

This document captures the full roadmap for topology exploration features using Neo4j graph data.

## Completed Work

### Phase 1-6: Foundation (Done)

- [x] ISIS topology graph visualization (Cytoscape.js)
- [x] Path finding between two devices (shortest path)
- [x] Compare mode (configured links vs ISIS adjacencies)
- [x] Failure impact analysis (what if device X goes down)
- [x] Guided exploration panel with suggestions
- [x] Keyboard shortcuts (Esc, ?, p, c, f)
- [x] Device filtering and search
- [x] Non-ISIS devices shown as disabled in path mode

---

## Development Guidelines

### Dual-View Implementation
When adding visual/interactive features to the topology views, **implement on both Graph and Map views** where appropriate. Both views share the same underlying data and many features benefit from being available in either visualization:

- **Path finding** → Both views (implemented)
- **Critical links overlay** → Both views
- **What-if simulations** → Both views
- **Failure impact** → Both views

Features that are view-specific:
- **Metro clustering** → Graph only (map already shows geographic clustering)
- **Geographic context** → Map only

---

## Roadmap: Graph/Map View Enhancements

These features enhance both the topology graph and map views with interactive, visual explorations.

### 7. Find All Paths (K-Shortest Paths)
**Priority: High** | **Complexity: Medium** | **Status: Done**

Show multiple paths between two devices, not just the shortest.

**Features:**
- Use Neo4j's `allShortestPaths()` or custom K-shortest implementation
- Display up to 5 paths with different colors
- Show path metrics (total cost, hop count)
- Allow user to highlight/compare specific paths
- Toggle between paths in the UI

**Use Cases:**
- Understanding redundancy between two points
- Capacity planning - see alternative routes
- Troubleshooting - why is traffic taking this path?

**API:** `GET /api/topology/paths?from={pk}&to={pk}&k=5`

**Neo4j Query:**
```cypher
MATCH (a:Device {pk: $from}), (b:Device {pk: $to})
MATCH path = (a)-[:ISIS_ADJACENT*1..15]-(b)
WITH path, reduce(cost = 0, r IN relationships(path) | cost + r.metric) AS totalMetric
ORDER BY totalMetric
LIMIT $k
RETURN path, totalMetric
```

---

### 8. Critical Links Detection
**Priority: High** | **Complexity: Medium** | **Status: Done**

Identify and highlight links that would partition the network if removed (bridge edges).

**Features:**
- Find "bridge" edges in the graph (single points of failure)
- Color-code edges by criticality:
  - Red = critical (removal partitions network)
  - Yellow = important (removal significantly increases path lengths)
  - Green = redundant (removal has minimal impact)
- Toggle overlay on/off in graph view
- Click critical link to see what would be disconnected

**Use Cases:**
- Risk assessment before maintenance
- Identifying where to add redundancy
- Network design validation

**API:** `GET /api/topology/critical-links`

**Neo4j Query:**
```cypher
// Find edges whose removal disconnects the graph
MATCH (a:Device)-[r:ISIS_ADJACENT]-(b:Device)
WHERE a.isis_system_id IS NOT NULL AND b.isis_system_id IS NOT NULL
WITH a, b, r
// Check if removing this edge disconnects a from b
WHERE NOT EXISTS {
  MATCH path = (a)-[:ISIS_ADJACENT*1..20]-(b)
  WHERE NOT r IN relationships(path)
}
RETURN a.pk AS fromPK, a.code AS fromCode,
       b.pk AS toPK, b.code AS toCode,
       r.metric AS metric
```

---

### 9. What-If Link Removal
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Simulate removing a link and show impact on paths.

**Features:**
- Click a link to simulate its removal
- Show which paths would be rerouted
- Display new path metrics vs old (before/after comparison)
- Highlight affected devices
- Show if any devices become unreachable

**Use Cases:**
- Maintenance planning - "what if I take this link down?"
- Risk assessment - "how bad would it be if this link fails?"
- Change validation - verify redundancy works

**Implementation Options:**
1. Server-side: `GET /api/topology/simulate-removal?link={pk}`
2. Client-side: Temporarily remove edge from Cytoscape graph, recalculate paths

---

### 10. What-If Link Addition
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Simulate adding a new link between two devices.

**Features:**
- Select two devices to add simulated link
- Input metric for the new link
- Show how paths would change
- Display connectivity improvements
- Show new redundancy created

**Use Cases:**
- Capacity planning - "where should we add links?"
- Network design - "what if we connect these two metros?"
- Cost-benefit analysis for new links

**API:** `GET /api/topology/simulate-addition?from={pk}&to={pk}&metric={n}`

---

### 11. Metro Clustering View
**Priority: Low** | **Complexity: Low** | **Status: Not Started**

Group and color devices by metro/datacenter.

**Features:**
- Color-code nodes by metro (each metro gets a distinct color)
- Option to collapse metro into single "super node"
- Show inter-metro links only in collapsed view
- Expand metro on click to see internal topology

**Use Cases:**
- Geographic visualization at high level
- Understanding inter-metro connectivity
- Simplifying view for large networks

**Implementation:** Client-side using existing metro data on nodes. No new API needed.

---

### 12. Traffic Flow Visualization
**Priority: Low** | **Complexity: High** | **Status: Not Started**

Animate traffic flowing along paths (requires traffic data).

**Features:**
- Overlay traffic volume on edges (thickness = bandwidth utilization)
- Animate packets/flows along paths
- Show congestion hotspots (red = congested)
- Historical playback - see traffic patterns over time

**Use Cases:**
- Capacity monitoring
- Troubleshooting congestion
- Understanding traffic patterns

**Dependency:** Requires traffic telemetry data in ClickHouse (partially available)

---

## Roadmap: Separate Pages/Tools

These features are better as dedicated analysis tools with tables and reports.

### 13. Redundancy Report
**Priority: High** | **Complexity: Medium** | **Status: Done**

Comprehensive report of single points of failure.

**Page:** `/topology/redundancy`

**Sections:**
1. **Leaf Devices** - Devices with only 1 ISIS neighbor (at risk if that neighbor fails)
2. **Critical Links** - Bridge edges (from feature #8)
3. **Single-Exit Metros** - Metros with only one path to the rest of the network
4. **No-Backup Devices** - Devices with no redundant path to core

**Output:** Sortable table with:
- Severity (critical/warning/info)
- Device/link info
- Impact description
- Recommendation

**API:** `GET /api/topology/redundancy-report`

---

### 14. Metro Connectivity Matrix
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Grid showing connectivity between all metros.

**Page:** `/topology/metro-matrix`

**Features:**
- NxN grid of metros (rows and columns)
- Cell shows:
  - Path count (number of distinct paths)
  - Min hops
  - Min total metric
- Color-code by connectivity strength:
  - Green = well connected (multiple paths)
  - Yellow = limited (1-2 paths)
  - Red = single path or disconnected
- Click cell to see paths between those metros
- Export to CSV

**API:** `GET /api/topology/metro-connectivity`

**Neo4j Query:**
```cypher
MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
WHERE m1 <> m2
  AND d1.isis_system_id IS NOT NULL
  AND d2.isis_system_id IS NOT NULL
MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
WITH m1, m2, min(length(path)) AS minHops, count(DISTINCT path) AS pathCount
RETURN m1.code AS fromMetro, m2.code AS toMetro, minHops, pathCount
```

---

### 15. Path Calculator
**Priority: Medium** | **Complexity: Low** | **Status: Not Started**

Detailed multi-path analysis tool.

**Page:** `/topology/path-calculator`

**Features:**
- Input source and destination (autocomplete search)
- Show all paths (up to K) with details:
  - Total metric
  - Hop count
  - Each hop with interface/metric breakdown
  - Segment routing SID stack
- Compare paths side-by-side
- Copy SR SID stack for troubleshooting
- Link to graph view to visualize

**API:** Extends existing `/api/topology/path` with `?detail=full`

---

### 16. Maintenance Planner
**Priority: Medium** | **Complexity: High** | **Status: Not Started**

Plan maintenance windows with impact analysis.

**Page:** `/topology/maintenance`

**Features:**
- Select multiple devices/links to take offline (multi-select)
- Show comprehensive impact:
  - Affected paths (before/after comparison)
  - Devices that lose connectivity
  - Traffic that needs rerouting
  - Estimated failover paths and new metrics
- Generate maintenance checklist
- Recommended maintenance order (take down least impactful first)
- Schedule and track maintenance windows (optional)

**API:** `POST /api/topology/maintenance-impact`
```json
{
  "devices": ["pk1", "pk2"],
  "links": ["linkpk1"]
}
```

---

### 17. Network Evolution Timeline
**Priority: Low** | **Complexity: High** | **Status: Not Started**

Historical topology changes over time.

**Page:** `/topology/history`

**Features:**
- Timeline slider to view topology at point in time
- Diff view showing:
  - Added devices (green)
  - Removed devices (red)
  - Added links
  - Removed links
- Track when links flapped (went up/down)
- Correlate with incidents
- Animation: play topology evolution over time

**Dependency:** Requires historical topology snapshots stored in ClickHouse. Need indexer work to periodically snapshot topology state.

---

## Implementation Phases

### Phase 7: Path Analysis (Done)
- [x] Find All Paths (K-shortest) - API + Graph UI + Map UI
- [x] Path Calculator page
- [x] Topology sub-navigation (Map, Graph, Path Calculator)

### Phase 8: Risk Analysis (Done)
- [x] Critical Links detection - API + Graph UI + Map UI overlay
- [x] Redundancy Report page

### Phase 9: What-If Simulation (Done)
- [x] What-If Link Removal - Graph UI + Map UI
- [x] What-If Link Addition - Graph UI + Map UI

### Phase 10: Planning Tools
- [ ] Metro Connectivity Matrix page
- [ ] Maintenance Planner page

### Phase 11: Advanced Visualization
- [ ] Metro Clustering View
- [ ] Traffic Flow Visualization
- [ ] Network Evolution Timeline

---

## Data Requirements

| Feature | Data Source | Available? |
|---------|-------------|------------|
| ISIS Topology | Neo4j | Yes |
| Device Metros | Neo4j | Yes |
| Link Metrics | Neo4j | Yes |
| Segment Routing SIDs | Neo4j | Yes |
| Traffic Data | ClickHouse | Partial |
| Historical Topology | ClickHouse | No (needs indexer work) |

---

## UI/UX Notes

### Graph View vs Map View vs Separate Page Decision Matrix

| Feature | Graph View | Map View | Separate Page | Rationale |
|---------|------------|----------|---------------|-----------|
| Find All Paths | Yes | Yes | - | Visual comparison of routes on both |
| Critical Links | Yes | Yes | - | Color overlay works on both |
| What-If Removal | Yes | Yes | - | Interactive simulation on both |
| What-If Addition | Yes | Yes | - | Interactive simulation on both |
| Metro Clustering | Yes | - | - | Graph only (map is already geographic) |
| Traffic Flow | Yes | Yes | - | Animation works on both |
| Redundancy Report | - | - | Yes | Tabular checklist for ops |
| Metro Matrix | - | - | Yes | NxN grid doesn't fit visual views |
| Path Calculator | - | - | Yes | Detailed text output |
| Maintenance Planner | - | - | Yes | Complex multi-step workflow |
| Network Evolution | - | - | Yes | Timeline/diff view, not spatial |

### Navigation

Topology section in sidebar (implemented):
```
Topology
├── /topology/map          - Map View (geographic)
├── /topology/graph        - Graph View (ISIS topology)
├── /topology/path-calculator - Path Calculator (done)
├── /topology/redundancy   - Redundancy Report (planned)
├── /topology/metro-matrix - Metro Matrix (planned)
├── /topology/maintenance  - Maintenance Planner (planned)
└── /topology/history      - History (future)
```

---

## Open Questions

1. **What-If simulations**: Client-side (faster, works offline) or server-side (more accurate, handles complex scenarios)?
2. **Historical snapshots**: How often to snapshot? How long to retain?
3. **Maintenance Planner**: Integrate with external ticketing (PagerDuty, Jira)?
4. **Role-based access**: Should maintenance operations require elevated permissions?
5. **Traffic data**: What traffic metrics are available? Interface counters? Flow data?

---

## Appendix: Existing API Endpoints

```
# ISIS Topology (implemented)
GET /api/topology/isis              # Full graph
GET /api/topology/path              # Shortest path
GET /api/topology/compare           # Config vs ISIS comparison
GET /api/topology/impact/{pk}       # Failure impact analysis

# ClickHouse Topology (implemented)
GET /api/topology                   # Metros, devices, links
GET /api/topology/traffic           # Traffic stats
```
