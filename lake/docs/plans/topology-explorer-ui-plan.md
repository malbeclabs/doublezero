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
**Priority: Medium** | **Complexity: Medium** | **Status: Done**

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

**API:** `GET /api/topology/simulate-link-removal?sourcePK={pk}&targetPK={pk}`

---

### 10. What-If Link Addition
**Priority: Medium** | **Complexity: Medium** | **Status: Done**

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

**API:** `GET /api/topology/simulate-link-addition?sourcePK={pk}&targetPK={pk}&metric={n}`

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
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Overlay real-time and historical traffic utilization on topology views.

**Features:**
- Edge thickness = bandwidth utilization percentage
- Color gradient: green (0-50%) → yellow (50-80%) → red (80%+)
- Tooltip shows: current bps, utilization %, peak in last hour
- Toggle between in/out/bidirectional view
- Time slider for historical playback (last 24h, 7d, 30d)
- Animate flow direction (optional, can be distracting)

**Use Cases:**
- Capacity monitoring - identify saturated links
- Troubleshooting congestion - where is the bottleneck?
- Capacity planning - which links need upgrades?

**Data Source:** `fact_dz_device_interface_counters` (ClickHouse)
- `in_octets_delta`, `out_octets_delta` for bytes transferred
- `delta_duration` for rate calculation
- `link_pk` to join with topology
- `bandwidth_bps` from `dim_dz_links_current` for utilization %

**API:** `GET /api/topology/traffic-overlay?timeRange=5m`

**Query Pattern:**
```sql
SELECT
  link_pk,
  SUM(in_octets_delta) * 8 / SUM(delta_duration) AS in_bps,
  SUM(out_octets_delta) * 8 / SUM(delta_duration) AS out_bps
FROM fact_dz_device_interface_counters
WHERE event_ts > now() - INTERVAL 5 MINUTE
  AND link_pk != ''
GROUP BY link_pk
```

**Implementation:**
1. API endpoint returns traffic per link_pk
2. Graph/Map views merge with topology data
3. Apply visual styles based on utilization thresholds

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
**Priority: Medium** | **Complexity: Medium** | **Status: Done**

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

---

### 15. Path Calculator
**Priority: Medium** | **Complexity: Low** | **Status: Done**

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

**API:** `GET /api/topology/paths?from={pk}&to={pk}&k=5`

---

### 16. Maintenance Planner
**Priority: Medium** | **Complexity: High** | **Status: Done**

Plan maintenance windows with impact analysis.

**Page:** `/topology/maintenance`

**Features:**
- Select multiple devices/links to take offline (multi-select)
- Show comprehensive impact:
  - ISIS adjacencies going down (specific links per metro pair)
  - Routing impact: paths rerouted with before/after hops and latency
  - Devices that become isolated
  - Affected metro connectivity
- Generate maintenance checklist (exportable)
- Summary stats: adjacencies down, paths rerouted, paths disconnected, devices isolated

**API:** `POST /api/topology/maintenance-impact`
```json
{
  "devices": ["pk1", "pk2"],
  "links": ["linkpk1"]
}
```

---

### 17. Network Evolution Timeline
**Priority: Medium** | **Complexity: High** | **Status: Not Started**

Historical topology changes over time.

**Page:** `/topology/history`

**Features:**
- Timeline slider to view topology at point in time
- Diff view showing:
  - Added devices (green)
  - Removed devices (red)
  - Added links
  - Removed links
  - Status changes (e.g., link went down/up)
- Track when links flapped (went up/down)
- Correlate with incidents
- Animation: play topology evolution over time

**Data Source:** SCD2 history tables in ClickHouse (already available):
- `dim_dz_devices_history` - device changes with `snapshot_ts`, `is_deleted`
- `dim_dz_links_history` - link changes with `snapshot_ts`, `status`, `is_deleted`
- `dim_dz_metros_history` - metro changes

**API:** `GET /api/topology/history?from={ts}&to={ts}`

**Query Pattern:**
```sql
-- Find devices added in time range
SELECT entity_id, snapshot_ts, code, status, 'added' AS change_type
FROM dim_dz_devices_history
WHERE snapshot_ts BETWEEN {from} AND {to}
  AND is_deleted = 0
  AND entity_id NOT IN (
    SELECT entity_id FROM dim_dz_devices_history
    WHERE snapshot_ts < {from}
  )

UNION ALL

-- Find devices removed in time range
SELECT entity_id, snapshot_ts, code, status, 'removed' AS change_type
FROM dim_dz_devices_history
WHERE snapshot_ts BETWEEN {from} AND {to}
  AND is_deleted = 1
```

**Implementation:**
1. API queries history tables for changes in time range
2. UI shows timeline with change markers
3. Click marker to see diff at that point
4. Slider scrubs through time, updating graph/map view

---

### 18. Link Health Overlay (Latency vs SLA)
**Priority: High** | **Complexity: Medium** | **Status: Not Started**

Color links by measured latency health compared to SLA commitments.

**Features:**
- Compare measured `rtt_us` against `committed_rtt_ns` from link config
- Color gradient:
  - Green = within SLA (measured < committed)
  - Yellow = approaching limit (80-100% of committed)
  - Red = exceeding SLA (measured > committed)
- Tooltip shows: measured RTT, committed RTT, % of SLA
- Include jitter (`ipdv_us`) and packet loss indicators
- Toggle overlay on/off (like critical links mode)

**Use Cases:**
- SLA monitoring - which links are violating commitments?
- Proactive maintenance - catch degrading links before they fail
- Troubleshooting - is latency the issue?

**Data Source:** `fact_dz_device_link_latency` + `dim_dz_links_current` (ClickHouse)

**API:** `GET /api/topology/link-health`

**Query Pattern:**
```sql
SELECT
  l.link_pk,
  AVG(l.rtt_us) AS avg_rtt_us,
  AVG(l.ipdv_us) AS avg_jitter_us,
  countIf(l.loss = true) * 100.0 / count(*) AS loss_pct,
  dl.committed_rtt_ns / 1000 AS committed_rtt_us,
  dl.committed_jitter_ns / 1000 AS committed_jitter_us
FROM fact_dz_device_link_latency l
JOIN dim_dz_links_current dl ON l.link_pk = dl.pk
WHERE l.event_ts > now() - INTERVAL 5 MINUTE
GROUP BY l.link_pk, dl.committed_rtt_ns, dl.committed_jitter_ns
```

**Implementation:**
1. API returns per-link health metrics
2. Graph/Map views apply color based on SLA %
3. Keyboard shortcut: `h` for health overlay

---

### 19. DZ vs Internet Latency Comparison
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Show how much faster DZ is compared to public internet between metros.

**Features:**
- Enhance Metro Connectivity Matrix with latency comparison columns:
  - DZ Latency (from `fact_dz_device_link_latency`)
  - Internet Latency (from `fact_dz_internet_metro_latency`)
  - Advantage: "DZ is 8x faster" or "45ms saved"
- Visual indicator (green bar showing improvement)
- Click cell to see latency trend over time
- Add to path calculator: "Internet equivalent would be Xms"

**Use Cases:**
- Demonstrate DZ value proposition
- Sales/marketing data
- Route optimization decisions

**Data Sources:**
- `fact_dz_device_link_latency` - DZ link latency
- `fact_dz_internet_metro_latency` - Public internet baseline

**API:** `GET /api/topology/metro-latency-comparison`

**Query Pattern:**
```sql
-- DZ latency between metros (via device links)
WITH dz_latency AS (
  SELECT
    d1.metro_pk AS from_metro,
    d2.metro_pk AS to_metro,
    AVG(l.rtt_us) AS dz_rtt_us
  FROM fact_dz_device_link_latency l
  JOIN dim_dz_devices_current d1 ON l.origin_device_pk = d1.pk
  JOIN dim_dz_devices_current d2 ON l.target_device_pk = d2.pk
  WHERE l.event_ts > now() - INTERVAL 1 HOUR
  GROUP BY d1.metro_pk, d2.metro_pk
),
-- Internet latency between same metros
internet_latency AS (
  SELECT
    origin_metro_pk AS from_metro,
    target_metro_pk AS to_metro,
    AVG(rtt_us) AS internet_rtt_us
  FROM fact_dz_internet_metro_latency
  WHERE event_ts > now() - INTERVAL 1 HOUR
  GROUP BY origin_metro_pk, target_metro_pk
)
SELECT
  d.from_metro,
  d.to_metro,
  d.dz_rtt_us,
  i.internet_rtt_us,
  i.internet_rtt_us / d.dz_rtt_us AS speedup_factor,
  i.internet_rtt_us - d.dz_rtt_us AS latency_saved_us
FROM dz_latency d
LEFT JOIN internet_latency i ON d.from_metro = i.from_metro AND d.to_metro = i.to_metro
```

---

### 20. Enhanced Path Calculator with Measured Latency
**Priority: Medium** | **Complexity: Low** | **Status: Not Started**

Show actual measured latency on paths, not just ISIS metric.

**Features:**
- Add columns to path results:
  - ISIS Metric (configured)
  - Measured RTT (from telemetry)
  - Jitter
  - Loss %
- Per-hop breakdown shows measured latency for each link
- Flag hops where measured >> configured
- Historical trend: "Latency on this path increased 15% this week"

**Use Cases:**
- Troubleshooting: configured says 10ms but measured is 50ms
- Path selection: choose path with best actual performance
- Capacity planning: identify consistently slow links

**Implementation:**
1. Extend existing path API to include measured latency
2. Join path links with `fact_dz_device_link_latency`
3. Display side-by-side in path calculator UI

---

### 21. Latency Degradation Alerts in Redundancy Report
**Priority: Medium** | **Complexity: Low** | **Status: Not Started**

Add latency health issues to the redundancy report.

**Features:**
- New section: "Degraded Links"
  - Links where measured latency exceeds SLA
  - Links with high jitter or packet loss
  - Links with latency trending upward
- Severity based on how far over SLA
- Include in maintenance recommendations

**Use Cases:**
- Proactive ops: fix degrading links before they impact users
- Complement to structural redundancy analysis

**Implementation:**
1. Add query to redundancy report API
2. Display as new section in report page
3. Link to graph view to highlight degraded links

---

### 22. Metro Connectivity with Bandwidth
**Priority: High** | **Complexity: Medium** | **Status: Not Started**

Enhance metro connectivity matrix with bandwidth metrics.

**Features:**
- Add columns to metro matrix:
  - Min bandwidth (bottleneck) along best path
  - Max bandwidth (best single path capacity)
  - Aggregate bandwidth (sum of all path capacities)
- Color-code by bandwidth adequacy
- Show bandwidth per path in detail view

**Use Cases:**
- Capacity planning: which metro pairs are bandwidth-constrained?
- Upgrade decisions: where to add capacity?

**Data Source:** `dim_dz_links_current.bandwidth_bps` joined with path topology

**API Enhancement:** Extend `GET /api/topology/metro-connectivity` response

**Query Pattern:**
```sql
-- Get bandwidth for links in a path
SELECT
  l.pk as link_pk,
  l.bandwidth_bps
FROM dim_dz_links_current l
WHERE l.pk IN (... path link pks ...)
-- Min of these = bottleneck bandwidth
```

---

### 23. Path Finding by Measured Latency
**Priority: High** | **Complexity: Medium** | **Status: Not Started**

Find paths optimized for actual measured latency, not just ISIS metric.

**Features:**
- Toggle in path calculator: "Use measured latency" vs "Use ISIS metric"
- Dijkstra on measured RTT values from telemetry
- Show comparison: "ISIS metric path: 5 hops, 50 metric | Lowest latency path: 6 hops, 3.2ms"
- Highlight when paths differ (measured latency suggests different route)

**Use Cases:**
- Troubleshooting: ISIS thinks path A is best, but measured latency shows path B is faster
- Optimization: find actual lowest latency route
- Validation: verify ISIS metrics match reality

**Data Sources:**
- Neo4j for topology structure
- ClickHouse `fact_dz_device_link_latency` for measured RTT per link

**API:** `GET /api/topology/path?from={pk}&to={pk}&metric=measured`

**Implementation:**
1. Query measured latency per link from ClickHouse
2. Build weighted graph with RTT values
3. Run Dijkstra (or use Neo4j with custom weights)
4. Return path with measured latency breakdown

---

### 24. Latency Optimization Opportunities
**Priority: Medium** | **Complexity: High** | **Status: Not Started**

Identify where network latency could be improved.

**Page:** `/topology/optimization`

**Features:**
1. **High-Latency Links** - Links where measured >> expected (based on distance/type)
2. **Suboptimal Paths** - Metro pairs where adding a link would significantly reduce latency
3. **Congestion Hotspots** - Links with high utilization affecting latency
4. **Missing Direct Links** - Metro pairs that route through 3+ hops but could be direct

**Analysis Sections:**
- "Link LAX-NYC has 45ms latency but geographic distance suggests 35ms possible"
- "Adding direct link SFO-CHI would reduce SFO→NYC latency from 65ms to 40ms"
- "Link ORD-ATL at 85% utilization, latency spiking during peak hours"

**Use Cases:**
- Network planning: where to invest in improvements?
- Vendor discussions: which links are underperforming?
- Capacity planning: which links need upgrades?

**Data Sources:**
- `fact_dz_device_link_latency` - measured latency
- `dim_dz_links_current` - link config, bandwidth
- `dim_dz_metros_current` - coordinates for distance calculation
- What-if simulation APIs - model improvements

---

### 25. Stake Overlay on Topology
**Priority: High** | **Complexity: Medium** | **Status: Not Started**

Visualize Solana validator stake distribution on topology views.

**Features:**
- Node size or color intensity = stake on that device
- Metro-level aggregation: total stake per metro
- Stake-weighted path analysis: "This path serves 15% of network stake"
- Filter: show only paths serving validators with >X stake
- Tooltip: "Device LAX-01: 3 validators, 2.5M SOL (0.5% of stake)"

**Use Cases:**
- Impact analysis: "If this link fails, 10% of stake is affected"
- Capacity planning: ensure high-stake locations have redundancy
- Sales: show stake distribution to potential contributors

**Data Source:** Join chain already exists:
```sql
SELECT
  d.pk as device_pk,
  d.code as device_code,
  m.pk as metro_pk,
  m.code as metro_code,
  SUM(v.activated_stake_lamports) / 1e9 as total_stake_sol,
  COUNT(DISTINCT v.vote_pubkey) as validator_count
FROM solana_vote_accounts_current v
JOIN solana_gossip_nodes_current g ON v.node_pubkey = g.pubkey
JOIN dz_users_current u ON g.gossip_ip = u.dz_ip
JOIN dz_devices_current d ON u.device_pk = d.pk
LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
WHERE v.epoch_vote_account = 'true'
  AND u.status = 'activated'
GROUP BY d.pk, d.code, m.pk, m.code
```

**API:** `GET /api/topology/stake-distribution`

**Implementation:**
1. API returns stake per device and per metro
2. Graph/Map views merge stake data with topology
3. Visual encoding: node size or color gradient
4. Maintenance planner shows stake impact
5. Path calculator shows stake served by path

---

### 26. Stake-Weighted Impact Analysis
**Priority: Medium** | **Complexity: Medium** | **Status: Not Started**

Enhance maintenance planner and failure analysis with stake impact.

**Features:**
- Maintenance planner shows:
  - "Taking down LAX-01 affects 500K SOL stake (0.1%)"
  - "5 validators will lose connectivity"
  - "12 validators will have degraded paths (+20ms latency)"
- Failure impact shows stake at risk
- Prioritize maintenance by minimizing stake impact

**Use Cases:**
- Maintenance scheduling: avoid impacting high-stake validators
- Risk assessment: understand true business impact of failures
- SLA discussions: stake-weighted availability metrics

**Implementation:**
1. Extend maintenance-impact API to include stake calculations
2. Join affected devices with stake distribution
3. Show in UI alongside routing impact

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

### Phase 10: Planning Tools (Done)
- [x] Metro Connectivity Matrix page
- [x] Maintenance Planner page

### Phase 11: Traffic & Utilization
- [ ] Traffic Flow Visualization (#12) - edge thickness by utilization
- [ ] Metro Clustering View (#11) - collapse metros in graph view

### Phase 12: Latency Intelligence
- [ ] Link Health Overlay (#18) - color by SLA compliance
- [ ] DZ vs Internet Comparison (#19) - metro matrix enhancement
- [ ] Path Calculator Measured Latency (#20)
- [ ] Degraded Links in Redundancy Report (#21)
- [ ] Path Finding by Measured Latency (#23) - Dijkstra on actual RTT

### Phase 13: Capacity & Bandwidth
- [ ] Metro Connectivity with Bandwidth (#22) - bottleneck bandwidth per metro pair
- [ ] Latency Optimization Opportunities (#24) - identify improvement areas

### Phase 14: Stake Integration
- [ ] Stake Overlay on Topology (#25) - visualize validator stake distribution
- [ ] Stake-Weighted Impact Analysis (#26) - maintenance planner enhancement

### Phase 15: Historical Analysis
- [ ] Network Evolution Timeline (#17) - topology changes over time

---

## Data Requirements

| Feature | Data Source | Available? |
|---------|-------------|------------|
| ISIS Topology | Neo4j | Yes |
| Device Metros | Neo4j | Yes |
| Link Metrics (configured) | Neo4j | Yes |
| Segment Routing SIDs | Neo4j | Yes |
| Traffic Counters | ClickHouse `fact_dz_device_interface_counters` | Yes |
| Link Latency | ClickHouse `fact_dz_device_link_latency` | Yes |
| Internet Latency | ClickHouse `fact_dz_internet_metro_latency` | Yes |
| Historical Topology | ClickHouse `dim_dz_*_history` tables | Yes |
| Link SLA Commitments | ClickHouse `dim_dz_links_current` | Yes |
| Link Bandwidth | ClickHouse `dim_dz_links_current.bandwidth_bps` | Yes |
| Validator Stake | ClickHouse `solana_vote_accounts_current` | Yes |
| Validator→Device Mapping | ClickHouse join: vote_accounts→gossip→users→devices | Yes |
| Metro Coordinates | ClickHouse `dim_dz_metros_current` (lat/lon) | Yes |

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
| Traffic Flow | Yes | Yes | - | Edge thickness overlay works on both |
| Link Health | Yes | Yes | - | Color overlay by SLA compliance |
| Redundancy Report | - | - | Yes | Tabular checklist for ops |
| Metro Matrix | - | - | Yes | NxN grid doesn't fit visual views |
| Path Calculator | - | - | Yes | Detailed text output |
| Maintenance Planner | - | - | Yes | Complex multi-step workflow |
| Network Evolution | - | - | Yes | Timeline/diff view, not spatial |
| DZ vs Internet | - | - | Yes | Extends metro matrix with latency |

### Navigation

Topology section in sidebar:
```
Topology
├── /topology/map              - Map View (geographic) [done]
├── /topology/graph            - Graph View (ISIS topology) [done]
├── /topology/path-calculator  - Path Calculator [done]
├── /topology/redundancy       - Redundancy Report [done]
├── /topology/metro-matrix     - Metro Connectivity Matrix [done]
├── /topology/maintenance      - Maintenance Planner [done]
└── /topology/history          - Network Evolution Timeline [planned]
```

---

## Open Questions

1. **Maintenance Planner**: Integrate with external ticketing (PagerDuty, Jira)?
2. **Role-based access**: Should maintenance operations require elevated permissions?
3. **Latency thresholds**: What % of SLA should trigger yellow/red warnings?
4. **Traffic animation**: Worth the visual complexity or just use thickness/color?

---

## Appendix: API Endpoints

```
# ISIS Topology (implemented)
GET /api/topology/isis                    # Full graph
GET /api/topology/path                    # Shortest path
GET /api/topology/paths                   # K-shortest paths
GET /api/topology/compare                 # Config vs ISIS comparison
GET /api/topology/impact/{pk}             # Failure impact analysis
GET /api/topology/critical-links          # Bridge edges (SPOFs)
GET /api/topology/simulate-link-removal   # What-if link removal
GET /api/topology/simulate-link-addition  # What-if link addition
GET /api/topology/metro-connectivity      # Metro connectivity matrix
POST /api/topology/maintenance-impact     # Maintenance impact analysis
GET /api/topology/redundancy-report       # Redundancy analysis

# ClickHouse Topology (implemented)
GET /api/topology                         # Metros, devices, links
GET /api/topology/traffic                 # Traffic stats

# Planned Endpoints - Latency & Traffic
GET /api/topology/traffic-overlay         # Per-link traffic for visualization
GET /api/topology/link-health             # Latency vs SLA per link
GET /api/topology/metro-latency-comparison # DZ vs Internet latency
GET /api/topology/path?metric=measured    # Path by measured latency (Dijkstra)

# Planned Endpoints - Capacity
GET /api/topology/metro-connectivity      # (enhance with bandwidth)
GET /api/topology/optimization            # Latency optimization opportunities

# Planned Endpoints - Stake
GET /api/topology/stake-distribution      # Stake per device/metro
POST /api/topology/maintenance-impact     # (enhance with stake impact)

# Planned Endpoints - Historical
GET /api/topology/history                 # Historical topology changes
```
