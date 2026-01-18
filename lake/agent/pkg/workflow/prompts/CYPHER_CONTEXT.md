# Graph Database Context (Neo4j)

This document contains Cypher query patterns and guidance for the DZ network graph database.

## When to Use Graph Queries

Use `execute_cypher` for:
- **Path finding**: "What's the path between device A and device B?"
- **Reachability analysis**: "What devices are reachable from metro X?"
- **Impact analysis**: "What's affected if device X goes down?"
- **Topology traversal**: "Show devices connected to this link"
- **Connectivity questions**: "Are these two devices connected?"
- **Network structure**: "What's the topology around this device?"

Use `execute_sql` for:
- Time-series data and metrics
- Validator performance and stake data
- Historical analysis
- Aggregations and statistics

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

- `(:Device)-[:CONNECTS]->(:Link)`: Bidirectional connection between device and link
- `(:Link)-[:CONNECTS]->(:Device)`: Links connect to devices on both sides
- `(:Device)-[:LOCATED_IN]->(:Metro)`: Device location
- `(:Device)-[:ISIS_ADJACENT]->(:Device)`: ISIS control plane adjacency
  - `metric` (int): ISIS metric
  - `neighbor_addr` (string): Neighbor address
  - `adj_sids` (list): Adjacency SIDs

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

### Devices Affected if Another Goes Down
```cypher
MATCH (target:Device {code: 'chi-dzd1'})
MATCH (other:Device)
WHERE other.code <> target.code
  AND other.status = 'active'
WITH target, other
WHERE NOT EXISTS {
  MATCH path = (other)-[:CONNECTS*1..10]-(anyDevice:Device)
  WHERE anyDevice.code <> target.code
    AND anyDevice <> other
    AND NOT ANY(n IN nodes(path) WHERE n:Device AND n.code = target.code)
    AND ALL(link IN [n IN nodes(path) WHERE n:Link] | link.status = 'activated')
}
RETURN other.code AS affected_device, other.status
```

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
MATCH (da)-[:CONNECTS]->(l:Link)<-[:CONNECTS]-(dz)
RETURN l.code AS link_code, l.status, l.committed_rtt_ns / 1000000.0 AS rtt_ms
```

## Query Tips

1. **Use lowercase metro codes**: `{code: 'nyc'}` not `{code: 'NYC'}`
2. **Filter by status early**: Add `WHERE status = 'active'` close to MATCH for efficiency
3. **Limit path depth**: Use `*1..10` not `*` to avoid unbounded traversals
4. **Return structured data**: Use CASE expressions to return clean objects
5. **Check both directions**: Links connect bidirectionally through Link nodes
