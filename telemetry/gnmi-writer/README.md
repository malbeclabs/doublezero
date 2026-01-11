# gNMI State Ingest Service

Consumes gNMI telemetry from network devices, extracts structured state records, and writes them to ClickHouse for analysis.

## System Overview

This service processes device telemetry through a three-stage pipeline:

1. **Kafka/Redpanda** - gNMI Subscribe notifications arrive as protobuf messages
2. **Processor** - Unmarshals OpenConfig data models and extracts structured records
3. **ClickHouse** - Stores records in time-series tables with automated retention

The processor uses registered extractors that pattern-match against gNMI paths. When a notification arrives (e.g., `/network-instances/network-instance/protocols/protocol/isis/...`), matching extractors unmarshal the payload into OpenConfig types and extract normalized records.

**Supported Collections:**
- ISIS adjacencies (neighbor relationships, state)
- BGP neighbors (session state, peer AS)
- System state (CPU, memory, hostname)
- Interface mappings (name to ifindex)

## Developer Guide

### Adding a New gNMI Collection

Follow these steps to add collection for a new OpenConfig path:

#### 1. Define the Record Type

Add a struct in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/records.go`:

```go
// LldpNeighborRecord represents an LLDP neighbor for storage in ClickHouse.
type LldpNeighborRecord struct {
    Timestamp     time.Time `json:"timestamp" ch:"timestamp"`
    DeviceCode    string    `json:"device_code" ch:"device_code"`
    InterfaceName string    `json:"interface_name" ch:"interface_name"`
    ChassisID     string    `json:"chassis_id" ch:"chassis_id"`
    PortID        string    `json:"port_id" ch:"port_id"`
    SystemName    string    `json:"system_name,omitempty" ch:"system_name"`
}

// TableName returns the ClickHouse table name for LLDP neighbors.
func (r LldpNeighborRecord) TableName() string {
    return "lldp_neighbors"
}
```

**Requirements:**
- Embed `Timestamp time.Time` and `DeviceCode string` (populated from notification metadata)
- Use `ch` struct tags matching ClickHouse column names
- Implement `TableName() string` to return the destination table
- Use `omitempty` for optional fields

#### 2. Create the Extractor Function

Add an extractor in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/extractors.go`:

```go
// extractLldpNeighbors extracts LLDP neighbor records from an oc.Device.
func extractLldpNeighbors(device *oc.Device, meta Metadata) []Record {
    var records []Record

    for ifName, iface := range device.Interface {
        if iface.Lldp == nil {
            continue
        }
        for chassisID, neighbor := range iface.Lldp.Neighbor {
            record := LldpNeighborRecord{
                Timestamp:     meta.Timestamp,
                DeviceCode:    meta.DeviceCode,
                InterfaceName: ifName,
                ChassisID:     chassisID,
            }

            // Extract fields safely (OpenConfig uses pointers)
            if neighbor.PortId != nil {
                record.PortID = *neighbor.PortId
            }
            if neighbor.SystemName != nil {
                record.SystemName = *neighbor.SystemName
            }

            records = append(records, record)
        }
    }

    return records
}
```

**Pattern:**
- Iterate through the `oc.Device` structure to find your data
- Handle nil pointers (OpenConfig uses pointer fields for optional data)
- Populate `Timestamp` and `DeviceCode` from `meta` parameter
- Return nil if no meaningful data is found

#### 3. Register the Extractor

Add your extractor to `DefaultExtractors` in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/extractors.go`:

```go
var DefaultExtractors = []ExtractorDef{
    {Name: "isis_adjacencies", Match: PathContains("isis", "adjacencies"), Extract: extractIsisAdjacencies},
    {Name: "system_state", Match: PathContains("system", "state"), Extract: extractSystemState},
    {Name: "bgp_neighbors", Match: PathContains("bgp", "neighbors"), Extract: extractBgpNeighbors},
    {Name: "interface_ifindex", Match: PathContains("interfaces", "ifindex"), Extract: extractInterfaceIfindex},
    {Name: "lldp_neighbors", Match: PathContains("lldp", "neighbors"), Extract: extractLldpNeighbors},
}
```

**Path Matchers:**
- `PathContains("isis", "adjacencies")` - Matches paths containing both "isis" AND "adjacencies" elements
- `PathContainsAny("isis", "bgp")` - Matches paths containing "isis" OR "bgp"
- Element names must match exactly (e.g., "network-instance", not "network_instance")

**How Path Matching Works:**
```
gNMI path: /network-instances/network-instance[name=default]/protocols/protocol[identifier=ISIS]/isis/levels/level[level=2]/adjacencies
Elements:  ["network-instances", "network-instance", "protocols", "protocol", "isis", "levels", "level", "adjacencies"]

PathContains("isis", "adjacencies")  → Match (both elements present)
PathContains("bgp", "neighbors")     → No match ("bgp" not present)
```

#### 4. Create the ClickHouse Schema

Add a schema file in `/workspaces/doublezero/telemetry/gnmi-writer/clickhouse/lldp_neighbors.sql`:

```sql
-- LLDP Neighbor State Table
-- Stores LLDP neighbor records from gNMI telemetry

CREATE TABLE IF NOT EXISTS lldp_neighbors (
    timestamp DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    device_code LowCardinality(String),
    interface_name String,
    chassis_id String,
    port_id String,
    system_name String
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_code, interface_name, chassis_id, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS lldp_neighbors_latest AS
SELECT *
FROM lldp_neighbors
WHERE (device_code, timestamp) IN (
    SELECT device_code, max(timestamp)
    FROM lldp_neighbors
    GROUP BY device_code
);
```

**Schema Guidelines:**
- Use `DateTime64(9)` for nanosecond precision timestamps with DoubleDelta compression
- Use `LowCardinality(String)` for enum-like fields with limited distinct values
- Partition by month: `PARTITION BY toYYYYMM(timestamp)`
- Order by device and natural keys for query efficiency
- Set TTL to 30 days for automatic cleanup
- Create a `_latest` view that returns most recent snapshot per device

#### 5. Add Test Data

Create a prototext file in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/testdata/lldp_neighbors.prototext`:

```prototext
update: {
  timestamp: 1767996400924668639
  prefix: {
    target: "router1"
  }
  update: {
    path: {
      elem: { name: "lldp" }
      elem: { name: "interfaces" }
      elem: {
        name: "interface"
        key: { key: "name" value: "Ethernet1" }
      }
      elem: { name: "neighbors" }
      elem: {
        name: "neighbor"
        key: { key: "id" value: "00:11:22:33:44:55" }
      }
      elem: { name: "state" }
    }
    val: {
      json_ietf_val: "{\"openconfig-lldp:chassis-id\":\"00:11:22:33:44:55\",\"openconfig-lldp:port-id\":\"Ethernet1\",\"openconfig-lldp:system-name\":\"neighbor-switch\"}"
    }
  }
}
```

**Tips:**
- Use realistic device codes (target field) matching your environment
- Timestamps are Unix nanoseconds
- `json_ietf_val` contains OpenConfig JSON representation
- Include multiple neighbors if your extractor processes lists

#### 6. Add Integration Tests

Add test cases to `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/processor_integration_test.go`:

```go
var integrationTests = []integrationTestCase{
    // ... existing tests ...
    {
        name:          "LldpNeighbors",
        prototext:     "lldp_neighbors.prototext",
        consumerGroup: "test-lldp",
        table:         "lldp_neighbors",
        minRows:       1,
        verify:        verifyLldpNeighbors,
    },
}

func verifyLldpNeighbors(t *testing.T, h *testHarness) {
    type lldpRow struct {
        DeviceCode    string
        InterfaceName string
        ChassisID     string
        PortID        string
        SystemName    string
    }

    rows := queryRows(h, fmt.Sprintf(`
        SELECT device_code, interface_name, chassis_id, port_id, system_name
        FROM %s.lldp_neighbors
    `, chDbname), func(r *sql.Rows) (lldpRow, error) {
        var row lldpRow
        err := r.Scan(&row.DeviceCode, &row.InterfaceName, &row.ChassisID,
            &row.PortID, &row.SystemName)
        return row, err
    })

    t.Logf("found %d LLDP neighbor records", len(rows))
    require.GreaterOrEqual(t, len(rows), 1)

    require.Equal(t, "router1", rows[0].DeviceCode)
    require.Equal(t, "Ethernet1", rows[0].InterfaceName)
    require.Equal(t, "00:11:22:33:44:55", rows[0].ChassisID)
    require.Equal(t, "Ethernet1", rows[0].PortID)
    require.Equal(t, "neighbor-switch", rows[0].SystemName)
}
```

## Running Tests

### Unit Tests

Run unit tests for the gNMI package:

```bash
go test ./telemetry/gnmi-writer/internal/gnmi/...
```

Unit tests validate path matching, record extraction, and OpenConfig unmarshaling without external dependencies.

### Integration Tests

Run full end-to-end tests with ClickHouse and Redpanda:

```bash
go test -tags integration ./telemetry/gnmi-writer/internal/gnmi/...
```

**Requirements:**
- Docker must be running (uses testcontainers)
- First run will pull ClickHouse and Redpanda images (1-2 GB)
- Tests create ephemeral containers, publish prototext data, and verify records in ClickHouse

**What Integration Tests Validate:**
- Kafka/Redpanda message consumption
- Protobuf deserialization
- OpenConfig unmarshaling with production schemas
- Record extraction and transformation
- ClickHouse table creation and insertion
- Complete pipeline end-to-end

Integration tests use table-driven design. Each test case:
1. Publishes prototext from `testdata/` to Redpanda
2. Runs the processor until expected rows appear in ClickHouse
3. Queries the table and validates field values

## Architecture Notes

### Why OpenConfig?

OpenConfig provides vendor-neutral models for network state. Devices stream telemetry as gNMI Subscribe notifications containing OpenConfig JSON. The processor uses ygot (YANG Go tools) to unmarshal this JSON into type-safe Go structs, making extraction straightforward and resilient to schema evolution.

### Why ClickHouse Views?

Each table has a `_latest` view that returns the most recent snapshot per device. This is useful for dashboards showing current state without time-range queries. The view uses subquery filtering to find the latest timestamp per device, then returns all records at that timestamp.

### Extractor Selection

Only one extractor processes each update. When multiple extractors match a path, the first registered extractor wins. Order extractors from most specific to least specific in `DefaultExtractors`.

### Metrics

The processor exposes Prometheus metrics:
- `gnmi_processor_records_processed_total` - Records successfully written
- `gnmi_processor_processing_duration_seconds` - Time spent processing batches
- `gnmi_processor_processing_errors_total` - Unmarshal/extraction failures
- `gnmi_processor_write_errors_total` - ClickHouse write failures

## File Reference

| Path | Purpose |
|------|---------|
| `pkg/gnmi/records.go` | Record type definitions with ClickHouse struct tags |
| `pkg/gnmi/extractors.go` | Extractor functions and DefaultExtractors registry |
| `pkg/gnmi/types.go` | Core types (PathMatcher, ExtractFunc, Record interface) |
| `pkg/gnmi/processor.go` | Main processor orchestrating consume/extract/write |
| `pkg/gnmi/processor_integration_test.go` | End-to-end tests with containers |
| `clickhouse/*.sql` | ClickHouse table schemas and views |
| `pkg/gnmi/testdata/*.prototext` | Test gNMI notifications in prototext format |
