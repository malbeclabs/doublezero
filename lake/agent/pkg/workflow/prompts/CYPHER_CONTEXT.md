# Graph Database Context (Neo4j)

This document contains Cypher query patterns and guidance for the DZ network graph database.

## When to Use Graph Queries

Use `execute_cypher` for things SQL cannot do efficiently:
- **Path finding**: "What's the path between device A and device B?"
- **Reachability analysis**: "What devices are reachable from metro X?"
- **Impact analysis**: "What's affected if device X goes down?"
- **Multi-hop connectivity**: "What devices are N hops from X?"
- **Network traversal**: "Show the route between these points"

Use `execute_sql` for:
- Listing entities (devices, links, metros)
- Time-series data and metrics
- Validator performance and stake data
- Historical analysis and aggregations

## Combining Tools

Some questions benefit from both tools:
1. Use Cypher to find topology structure (e.g., devices in a path)
2. Use SQL to get performance metrics for those devices

Example: "What's the latency on the path from NYC to LON?"
1. `execute_cypher`: Find the path and links between NYC and LON metros
2. `execute_sql`: Query latency metrics for those specific links

## Graph Model

### Node Labels

**Device**: Network devices (routers, switches)
- `pk` (string): Primary key
- `code` (string): Human-readable device code (e.g., "nyc-dzd1")
- `status` (string): "active", "pending", "drained", etc.
- `device_type` (string): Type of device
- `public_ip` (string): Public IP address
- `isis_system_id` (string): ISIS system ID
- `isis_router_id` (string): ISIS router ID

**Link**: Network connections between devices
- `pk` (string): Primary key
- `code` (string): Link code (e.g., "nyc-lon-1")
- `status` (string): "activated", "soft-drained", etc.
- `committed_rtt_ns` (int): Committed RTT in nanoseconds
- `bandwidth` (int): Bandwidth in bps
- `isis_delay_override_ns` (int): ISIS delay override (>0 indicates drained)

**Metro**: Geographic locations
- `pk` (string): Primary key
- `code` (string): Metro code (e.g., "nyc", "lon")
- `name` (string): Full name

### Relationships

- `(:Link)-[:CONNECTS]->(:Device)`: Links connect to devices (direction is Link→Device)
  - `side` (string): "A" or "Z" indicating which end of the link
  - `iface_name` (string): Interface name on the device
  - **Note**: Each Link has exactly two CONNECTS relationships, one to each endpoint device
  - **Important**: Use undirected pattern `(:Device)-[:CONNECTS]-(:Link)` for traversal queries
- `(:Device)-[:LOCATED_IN]->(:Metro)`: Device location
- `(:Device)-[:OPERATES]->(:Contributor)`: Device operator
- `(:Link)-[:OWNED_BY]->(:Contributor)`: Link owner
- `(:Device)-[:ISIS_ADJACENT]->(:Device)`: ISIS control plane adjacency
  - `metric` (int): ISIS metric
  - `neighbor_addr` (string): Neighbor address
  - `adj_sids` (list): Adjacency SIDs

## CRITICAL: Always Look Up Device Codes First

**Device codes are NOT predictable.** You cannot construct a device code from a metro code. There is no guaranteed pattern like `{metro}-dzd1`.

When a user asks about "the Hong Kong device" or "devices in Tokyo":
1. **First query for devices in that metro** to get actual device codes
2. **Then use those real codes** in subsequent queries

**WRONG - guessing device codes:**
```cypher
// User asks: "What's connected to the Hong Kong device?"
// WRONG: Guessing that hkg-dzd1 exists
MATCH (d:Device {code: 'hkg-dzd1'})-[:CONNECTS]-(:Link)-[:CONNECTS]-(neighbor:Device)
RETURN neighbor.code
```

**CORRECT - look up devices first:**
```cypher
// Step 1: Find devices in Hong Kong
MATCH (m:Metro {code: 'hkg'})<-[:LOCATED_IN]-(d:Device)
RETURN d.code AS device_code, d.status
// Results might show: hkg-dzd2, hkg-dzd3 (NOT hkg-dzd1!)

// Step 2: Use actual device codes from step 1
MATCH (d:Device {code: 'hkg-dzd2'})-[:CONNECTS]-(:Link)-[:CONNECTS]-(neighbor:Device)
RETURN neighbor.code
```

**Key rules:**
- A metro may have zero, one, or multiple devices
- Device codes do not follow a predictable numbering scheme
- NEVER assume a device exists - always verify with a lookup query first
- If a lookup returns no devices, report "no devices found in that metro"

## Common Cypher Patterns

### Find Shortest Path Between Devices
```cypher
MATCH (a:Device {code: 'nyc-dzd1'}), (b:Device {code: 'lon-dzd1'})
MATCH path = shortestPath((a)-[:CONNECTS*]-(b))
RETURN [n IN nodes(path) |
  CASE WHEN n:Device THEN {type: 'device', code: n.code, status: n.status}
       WHEN n:Link THEN {type: 'link', code: n.code, status: n.status}
  END
] AS segments
```

### Find Shortest Path Between Metros

When finding the shortest path between two metros (not specific devices), you must find the overall shortest path among all device pairs. This requires ordering by path length and limiting to 1.

```cypher
MATCH (ma:Metro {code: 'nyc'})<-[:LOCATED_IN]-(da:Device)
MATCH (mz:Metro {code: 'lon'})<-[:LOCATED_IN]-(dz:Device)
MATCH path = shortestPath((da)-[:CONNECTS*]-(dz))
WITH path, length(path) AS pathLength
ORDER BY pathLength
LIMIT 1
RETURN [n IN nodes(path) |
  CASE WHEN n:Device THEN {type: 'device', code: n.code, status: n.status}
       WHEN n:Link THEN {type: 'link', code: n.code, status: n.status}
  END
] AS segments
```

**Key points:**
- Without `ORDER BY length(path) LIMIT 1`, the query returns an arbitrary path from one device pair
- The `shortestPath()` function finds the shortest path between a single pair, but with multiple devices per metro you need to compare across all pairs
- Use `length(path)` to get the number of relationships in the path

### Find ALL Paths Between Metros

When the user asks for "paths" (plural), "all paths", or "confirm the paths", use `allShortestPaths()` to find all paths of the same shortest length:

```cypher
// Find ALL shortest paths (same length) between metros
MATCH (ma:Metro {code: 'sin'})<-[:LOCATED_IN]-(da:Device)
MATCH (mz:Metro {code: 'tyo'})<-[:LOCATED_IN]-(dz:Device)
MATCH path = allShortestPaths((da)-[:CONNECTS*]-(dz))
RETURN DISTINCT [n IN nodes(path) |
  CASE WHEN n:Device THEN {type: 'device', code: n.code}
       WHEN n:Link THEN {type: 'link', code: n.code}
  END
] AS segments,
length(path) AS hops
ORDER BY hops
```

**When to use which:**
- `shortestPath()` - Returns ONE arbitrary shortest path (use for "the path", "shortest path")
- `allShortestPaths()` - Returns ALL paths of the shortest length (use for "paths", "all paths", "confirm paths")

**WRONG:** Using `shortestPath()` when user asks for "paths" (plural) - only returns one path.

### Find Devices in a Metro
```cypher
MATCH (m:Metro {code: 'nyc'})<-[:LOCATED_IN]-(d:Device)
WHERE d.status = 'active'
RETURN d.code AS device_code, d.device_type, d.status
```

### Find Reachable Devices from a Metro
```cypher
MATCH (m:Metro {code: 'nyc'})<-[:LOCATED_IN]-(start:Device)
WHERE start.status = 'active'
OPTIONAL MATCH path = (start)-[:CONNECTS*1..10]-(other:Device)
WHERE other.status = 'active'
  AND ALL(n IN nodes(path) WHERE (n:Device) OR (n:Link AND n.status = 'activated'))
WITH DISTINCT coalesce(other, start) AS device
RETURN device.code AS device_code, device.status
```

### Find Network Around a Device (N hops)
```cypher
MATCH (center:Device {code: 'nyc-dzd1'})
OPTIONAL MATCH path = (center)-[:CONNECTS*1..2]-(neighbor)
WITH collect(path) AS paths, center
UNWIND CASE WHEN size(paths) = 0 THEN [null] ELSE paths END AS p
WITH DISTINCT CASE WHEN p IS NULL THEN center ELSE nodes(p) END AS nodeList
UNWIND nodeList AS n
WITH DISTINCT n WHERE n IS NOT NULL
RETURN
  CASE WHEN n:Device THEN 'device' ELSE 'link' END AS node_type,
  n.code AS code,
  n.status AS status
```

### Find ISIS Adjacencies for a Device
```cypher
MATCH (from:Device {code: 'nyc-dzd1'})-[r:ISIS_ADJACENT]->(to:Device)
RETURN from.code AS from_device, to.code AS to_device,
       r.metric AS isis_metric, r.neighbor_addr
```

### Impact Analysis: Find Devices That Lose Connectivity

**CRITICAL: When checking alternate paths, you must exclude paths that go THROUGH the failed device using NONE() on intermediate nodes.**

**Step 1: Find devices directly connected to the target**
```cypher
MATCH (target:Device {code: 'hkg-dzd1'})-[:CONNECTS]-(:Link)-[:CONNECTS]-(neighbor:Device)
RETURN DISTINCT neighbor.code AS connected_device
```

**Step 2: Check which devices have alternate paths NOT going through the target**
```cypher
// For each device connected to target, check if it can reach ANY other device
// via a path that does NOT include the target device
MATCH (target:Device {code: 'hkg-dzd1'})
MATCH (candidate:Device)-[:CONNECTS]-(:Link)-[:CONNECTS]-(target)
WHERE candidate <> target
// Try to find an alternate path from candidate to any other device
OPTIONAL MATCH path = (candidate)-[:CONNECTS*1..10]-(other:Device)
WHERE other <> target
  AND other <> candidate
  AND NONE(n IN nodes(path) WHERE n:Device AND n.code = target.code)  // <-- CRITICAL: exclude paths through target
WITH candidate, count(DISTINCT other) AS reachable_without_target
RETURN candidate.code AS device,
       CASE WHEN reachable_without_target > 0 THEN 'connected' ELSE 'isolated' END AS status
```

**Step 3: Simpler check - does device have any direct connection besides target?**
```cypher
MATCH (target:Device {code: 'hkg-dzd1'})
MATCH (candidate:Device)-[:CONNECTS]-(:Link)-[:CONNECTS]-(target)
WHERE candidate <> target
// Count non-target neighbors
OPTIONAL MATCH (candidate)-[:CONNECTS]-(:Link)-[:CONNECTS]-(other:Device)
WHERE other <> target AND other <> candidate
WITH candidate, count(DISTINCT other) AS other_neighbors
RETURN candidate.code AS device,
       CASE WHEN other_neighbors = 0 THEN 'ISOLATED' ELSE 'has_redundancy' END AS status
```

**Key insight:**
- Use `NONE(n IN nodes(path) WHERE n:Device AND n.code = 'target-code')` to exclude paths through the target
- A device is ISOLATED if it has ZERO connections to non-target devices
- Don't just filter the destination - filter ALL intermediate nodes in the path

### Find Drained Links
```cypher
MATCH (l:Link)
WHERE l.isis_delay_override_ns > 0 OR l.status IN ['soft-drained', 'hard-drained']
RETURN l.code AS link_code, l.status,
       l.isis_delay_override_ns > 0 AS is_isis_drained
```

### Links Between Two Metros
```cypher
MATCH (ma:Metro {code: 'nyc'})<-[:LOCATED_IN]-(da:Device)
MATCH (mz:Metro {code: 'lon'})<-[:LOCATED_IN]-(dz:Device)
MATCH (da)<-[:CONNECTS]-(l:Link)-[:CONNECTS]->(dz)
RETURN l.code AS link_code, l.status, l.committed_rtt_ns / 1000000.0 AS rtt_ms
```

## Query Tips

1. **Use lowercase metro codes**: `{code: 'nyc'}` not `{code: 'NYC'}`
2. **Filter by status early**: Add `WHERE status = 'activated'` close to MATCH for efficiency
3. **Limit path depth**: Use `*1..10` not `*` to avoid unbounded traversals
4. **Return structured data**: Use CASE expressions to return clean objects
5. **CONNECTS direction**: Links point TO devices (`(:Link)-[:CONNECTS]->(:Device)`). For traversal, use undirected: `(d1:Device)-[:CONNECTS]-(:Link)-[:CONNECTS]-(d2:Device)`
6. **Do NOT use APOC**: APOC procedures are not available. Use built-in Cypher only.
7. **ALL/ANY syntax**: Use `ALL(x IN list WHERE condition)` NOT `ALL(x IN list | condition)`
8. **WITH clause variable scope**: Variables from before a WITH clause are NOT accessible after it unless explicitly passed through.

**WRONG - loses `start` variable:**
```cypher
MATCH (start:Device {code: 'nyc-dzd1'})
MATCH path = (start)-[:CONNECTS*1..4]-(other:Device)
WITH DISTINCT other AS device
WHERE device.code <> start.code  // ERROR: start not accessible!
RETURN device.code
```

**CORRECT - carry variables through WITH:**
```cypher
MATCH (start:Device {code: 'nyc-dzd1'})
MATCH path = (start)-[:CONNECTS*1..4]-(other:Device)
WITH DISTINCT other AS device, start  // Carry start through
WHERE device.code <> start.code
RETURN device.code
```

**Alternative - filter before WITH:**
```cypher
MATCH (start:Device {code: 'nyc-dzd1'})
MATCH path = (start)-[:CONNECTS*1..4]-(other:Device)
WHERE other.code <> start.code  // Filter here instead
WITH DISTINCT other AS device
RETURN device.code
```
