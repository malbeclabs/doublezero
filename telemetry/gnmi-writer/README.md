# gNMI Writer

Consumes gNMI telemetry from network devices, extracts structured state records, and writes them to ClickHouse for analysis.

## System Overview

This service processes device telemetry through a three-stage pipeline:

1. **Kafka/Redpanda** - gNMI Subscribe notifications arrive as protobuf messages
2. **Processor** - Unmarshals OpenConfig data models and extracts structured records
3. **ClickHouse** - Stores records in time-series tables with automated retention

The processor uses registered extractors that pattern-match against gNMI paths. When a notification arrives (e.g., `/network-instances/network-instance/protocols/protocol/isis/...`), matching extractors unmarshal the payload into OpenConfig types and extract normalized records.

**Supported Collections:**
- ISIS adjacencies (neighbor relationships, state)
- BGP neighbors (session state, peer AS, local AS, peer type, description, established transitions, session timing, UPDATE message counts)
- System state (CPU, memory, hostname)
- Interface mappings (name to ifindex)
- Transceiver state (optical power metrics: input power, output power, laser bias current per channel)
- Transceiver thresholds (alarm thresholds per severity: input/output power, laser bias current, module temperature, supply voltage)
- Interface state (admin/oper status, counters: in/out octets, packets, errors, discards)

## Developer Guide

### Adding a New gNMI Collection

Follow these steps to add collection for a new OpenConfig path:

#### 1. Define the Record Type

Add a struct in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/records.go`:

```go
// LldpNeighborRecord represents an LLDP neighbor for storage in ClickHouse.
type LldpNeighborRecord struct {
    Timestamp     time.Time `json:"timestamp" ch:"timestamp"`
    DevicePubkey  string    `json:"device_pubkey" ch:"device_pubkey"`
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
- Embed `Timestamp time.Time` and `DevicePubkey string` (populated from notification metadata)
- Use `ch` struct tags matching ClickHouse column names
- Implement `TableName() string` to return the destination table
- Use `omitempty` for optional fields

#### 2. Create the Extractor Function

Add an extractor in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/extractors.go`:

```go
// extractLldpNeighbors extracts LLDP neighbor records from an oc.Device.
func extractLldpNeighbors(device *oc.Device, meta Metadata) []Record {
    var records []Record

    if device.Lldp == nil || device.Lldp.Interfaces == nil {
        return nil
    }

    for ifName, iface := range device.Lldp.Interfaces.Interface {
        if iface.Neighbors == nil {
            continue
        }
        for neighborID, neighbor := range iface.Neighbors.Neighbor {
            record := LldpNeighborRecord{
                Timestamp:     meta.Timestamp,
                DevicePubkey:  meta.DevicePubkey,
                InterfaceName: ifName,
                ChassisID:     neighborID,
            }

            // All OpenConfig state fields are accessed through explicit State containers
            if neighbor.State != nil {
                if neighbor.State.PortId != nil {
                    record.PortID = *neighbor.State.PortId
                }
                if neighbor.State.SystemName != nil {
                    record.SystemName = *neighbor.State.SystemName
                }
            }

            records = append(records, record)
        }
    }

    return records
}
```

**Pattern:**
- Iterate through the `oc.Device` structure to find your data
- Access OpenConfig state data through explicit `.State` containers (due to uncompressed path generation)
- Handle nil pointers at both the container level (e.g., `neighbor.State`) and field level (e.g., `neighbor.State.PortId`)
- Populate `Timestamp` and `DevicePubkey` from `meta` parameter
- Return nil if no meaningful data is found

**State Container Access:**
OpenConfig uses uncompressed paths, meaning all operational state fields are accessed through explicit `.State` containers. For example:
- BGP neighbor peer AS: `neighbor.State.PeerAs` (not `neighbor.PeerAs`)
- Interface ifindex: `iface.State.Ifindex` (not `iface.Ifindex`)
- System hostname: `device.System.State.Hostname` (not `device.System.Hostname`)

Always check if the `.State` container exists before accessing its fields to avoid nil pointer panics.

#### 3. Register the Extractor

Add your extractor to `DefaultExtractors` in `/workspaces/doublezero/telemetry/gnmi-writer/internal/gnmi/extractors.go`:

```go
var DefaultExtractors = []ExtractorDef{
    {Name: "isis_adjacencies", Match: PathContains("isis", "adjacencies"), Extract: extractIsisAdjacencies},
    {Name: "system_state", Match: PathContains("system", "state"), Extract: extractSystemState},
    {Name: "bgp_neighbors", Match: PathContains("bgp", "neighbors"), Extract: extractBgpNeighbors},
    {Name: "interface_ifindex", Match: PathContains("interfaces", "ifindex"), Extract: extractInterfaceIfindex},
    {Name: "transceiver_state", Match: PathContains("transceiver", "physical-channels"), Extract: extractTransceiverState},
    {Name: "transceiver_thresholds", Match: PathContains("transceiver", "thresholds"), Extract: extractTransceiverThresholds},
    {Name: "interface_state", Match: PathContains("interfaces", "interface", "state"), Extract: extractInterfaceState},
    {Name: "lldp_neighbors", Match: PathContains("lldp", "neighbors"), Extract: extractLldpNeighbors},
}
```

**Extractor Ordering:**

Order matters in `DefaultExtractors`. When multiple extractors match a path, only the first one executes. Place more specific matchers before less specific ones to avoid collisions.

For example, `interface_ifindex` matches paths containing `["interfaces", "ifindex"]`, but ifindex paths also contain `"interface"` and `"state"`. If `interface_state` (which matches `["interfaces", "interface", "state"]`) were registered first, it would capture ifindex updates and `interface_ifindex` would never run.

When adding a new collection, consider whether your path matcher overlaps with existing ones and place it appropriately in the list.

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
    device_pubkey LowCardinality(String),
    interface_name String,
    chassis_id String,
    port_id String,
    system_name String
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (device_pubkey, interface_name, chassis_id, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- View for latest snapshot per device (returns complete state from most recent timestamp)
CREATE VIEW IF NOT EXISTS lldp_neighbors_latest AS
SELECT *
FROM lldp_neighbors
WHERE (device_pubkey, timestamp) IN (
    SELECT device_pubkey, max(timestamp)
    FROM lldp_neighbors
    GROUP BY device_pubkey
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
    target: "RTR011111111111111111111111111111111111111111"
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
        DevicePubkey  string
        InterfaceName string
        ChassisID     string
        PortID        string
        SystemName    string
    }

    rows := queryRows(h, fmt.Sprintf(`
        SELECT device_pubkey, interface_name, chassis_id, port_id, system_name
        FROM %s.lldp_neighbors
    `, chDbname), func(r *sql.Rows) (lldpRow, error) {
        var row lldpRow
        err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.ChassisID,
            &row.PortID, &row.SystemName)
        return row, err
    })

    t.Logf("found %d LLDP neighbor records", len(rows))
    require.GreaterOrEqual(t, len(rows), 1)

    require.Equal(t, "RTR011111111111111111111111111111111111111111", rows[0].DevicePubkey)
    require.Equal(t, "Ethernet1", rows[0].InterfaceName)
    require.Equal(t, "00:11:22:33:44:55", rows[0].ChassisID)
    require.Equal(t, "Ethernet1", rows[0].PortID)
    require.Equal(t, "neighbor-switch", rows[0].SystemName)
}
```

## Running Tests

### Unit Tests

Run unit tests for the gNMI Writer package:

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

### Uncompressed Path Structure

The ygot code generation uses uncompressed paths (`-compress_paths=false`). This means the Go struct hierarchy matches gNMI paths exactly, including explicit `/state` containers. For example, a BGP neighbor's peer AS is accessed as `neighbor.State.PeerAs` rather than `neighbor.PeerAs`.

While this produces larger generated code (177K vs 110K lines), it eliminates unmarshalling ambiguity. With compressed paths, ygot's SetNode silently failed when gNMI paths ended at `/state` containers, requiring custom workarounds. Uncompressed paths ensure the path structure in notifications matches the struct hierarchy precisely, making unmarshalling reliable and extraction code straightforward.

### Why ClickHouse Views?

Each table has a `_latest` view that returns the most recent snapshot per device. This is useful for dashboards showing current state without time-range queries. The view uses subquery filtering to find the latest timestamp per device, then returns all records at that timestamp.

### Extractor Selection

Only one extractor processes each update. When multiple extractors match a path, the first registered extractor wins. Order extractors from most specific to least specific in `DefaultExtractors`.

### Metrics

The service exposes Prometheus metrics for monitoring pipeline health:

**Consumer Metrics:**
- `gnmi_writer_notifications_consumed_total` - gNMI notifications consumed from Kafka
- `gnmi_writer_fetch_errors_total` - Kafka fetch errors
- `gnmi_writer_unmarshal_errors_total` - Protobuf unmarshal errors

**Processor Metrics:**
- `gnmi_writer_records_processed_total` - Records successfully extracted and processed
- `gnmi_writer_processing_duration_seconds` - Time spent processing notification batches
- `gnmi_writer_processing_errors_total` - Notification processing failures (unmarshal/extraction errors)
- `gnmi_writer_write_errors_total` - Record write failures
- `gnmi_writer_commit_errors_total` - Kafka offset commit errors

**ClickHouse Metrics:**
- `gnmi_writer_clickhouse_insert_duration_seconds` - Time spent inserting batches into ClickHouse
- `gnmi_writer_clickhouse_insert_errors_total` - ClickHouse insert errors
- `gnmi_writer_clickhouse_records_written_total` - Records successfully written to ClickHouse

## Tools

### gnmi-prototext-convert

Converts raw gNMI GET responses into SubscribeResponse format for testdata files.

Raw gNMI GET responses return Notification messages directly, but gnmi-writer tests expect SubscribeResponse wrappers. This tool handles the conversion and adds the required `prefix.target` field.

**Usage:**

```bash
# From file
go run ./tools/gnmi-prototext-convert --target DEVICE_PUBKEY --input raw.prototext > formatted.prototext

# From stdin
cat raw.prototext | go run ./tools/gnmi-prototext-convert --target DEVICE_PUBKEY > formatted.prototext
```

**Supported Input Formats:**
- Raw Notification prototext (starts with `timestamp:`, `prefix:`, etc.)
- Non-standard `notification: { ... }` wrapper format (common from some gNMI tools)
- Already-formatted SubscribeResponse (updates target only)

**Example:**

```bash
# Convert raw gNMI GET output for interface ifindex data
go run ./tools/gnmi-prototext-convert \
  --target "DZd011111111111111111111111111111111111111111" \
  --input raw_ifindexes.prototext \
  > internal/gnmi/testdata/interfaces_ifindex.prototext
```

## File Reference

| Path | Purpose |
|------|---------|
| `internal/gnmi/records.go` | Record type definitions with ClickHouse struct tags |
| `internal/gnmi/extractors.go` | Extractor functions and DefaultExtractors registry |
| `internal/gnmi/types.go` | Core types (PathMatcher, ExtractFunc, Record interface) |
| `internal/gnmi/processor.go` | Main processor orchestrating consume/extract/write |
| `internal/gnmi/processor_integration_test.go` | End-to-end tests with containers |
| `clickhouse/*.sql` | ClickHouse table schemas and views |
| `internal/gnmi/testdata/*.prototext` | Test gNMI notifications in prototext format |
| `tools/gnmi-prototext-convert/` | Tool to convert raw gNMI GET responses to testdata format |
