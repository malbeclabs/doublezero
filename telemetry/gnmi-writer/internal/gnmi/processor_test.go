package gnmi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// loadGoldenPrototext loads a prototext file and unmarshals it into a SubscribeResponse.
func loadGoldenPrototext(t *testing.T, filename string) *gpb.SubscribeResponse {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v", filename, err)
	}

	var resp gpb.SubscribeResponse
	if err := prototext.Unmarshal(data, &resp); err != nil {
		t.Fatalf("failed to unmarshal prototext %s: %v", filename, err)
	}
	return &resp
}

// serializeAndDeserialize simulates the Kafka round-trip by serializing to binary
// and deserializing back.
func serializeAndDeserialize(t *testing.T, resp *gpb.SubscribeResponse) *gpb.SubscribeResponse {
	t.Helper()
	data, err := proto.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal to binary: %v", err)
	}

	var result gpb.SubscribeResponse
	if err := proto.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal from binary: %v", err)
	}
	return &result
}

func TestProcessor_IsisAdjacency(t *testing.T) {
	// Load golden prototext
	resp := loadGoldenPrototext(t, "isis_adjacency.prototext")

	// Simulate Kafka round-trip (binary serialization)
	resp = serializeAndDeserialize(t, resp)

	// Extract notification
	notification := resp.GetUpdate()
	if notification == nil {
		t.Fatal("expected update in SubscribeResponse")
	}

	// Verify basic notification fields
	if got := notification.GetPrefix().GetTarget(); got != "CHiDN1111111111111111111111111111111111111111" {
		t.Errorf("expected target CHiDN1111111111111111111111111111111111111111, got %s", got)
	}
	if got := notification.GetTimestamp(); got != 1767996400924668639 {
		t.Errorf("expected timestamp 1767996400924668639, got %d", got)
	}
	if got := len(notification.GetUpdate()); got != 2 {
		t.Errorf("expected 2 updates, got %d", got)
	}

	// Create processor with ISIS extractor only
	var buf bytes.Buffer
	writer := NewFlatStdoutRecordWriter(WithFlatWriter(&buf))

	processor, err := NewProcessor(
		WithRecordWriter(writer),
		WithProcessorMetrics(newTestMetrics()),
		WithExtractors([]ExtractorDef{
			{"isis_adjacencies", PathContains("isis", "adjacencies"), extractIsisAdjacencies},
		}),
	)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Process the notification
	ctx := context.Background()
	records := processor.ProcessNotifications(ctx, []*gpb.Notification{notification})

	// Should produce 2 records (one per adjacency update)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Check first record (Switch1/11/2)
	record1, ok := records[0].(IsisAdjacencyRecord)
	if !ok {
		t.Fatalf("expected IsisAdjacencyRecord, got %T", records[0])
	}

	if record1.DevicePubkey != "CHiDN1111111111111111111111111111111111111111" {
		t.Errorf("expected DevicePubkey CHiDN1111111111111111111111111111111111111111, got %s", record1.DevicePubkey)
	}
	if record1.InterfaceID != "Switch1/11/2" {
		t.Errorf("expected InterfaceID Switch1/11/2, got %s", record1.InterfaceID)
	}
	if record1.Level != 2 {
		t.Errorf("expected Level 2, got %d", record1.Level)
	}
	if record1.SystemID != "ac10.0001.0000" {
		t.Errorf("expected SystemID ac10.0001.0000, got %s", record1.SystemID)
	}
	if record1.AdjacencyState != "UP" {
		t.Errorf("expected AdjacencyState UP, got %s", record1.AdjacencyState)
	}
	if record1.LocalCircuitID != 197 {
		t.Errorf("expected LocalCircuitID 197, got %d", record1.LocalCircuitID)
	}

	// Check second record (Switch1/11/4)
	record2, ok := records[1].(IsisAdjacencyRecord)
	if !ok {
		t.Fatalf("expected IsisAdjacencyRecord, got %T", records[1])
	}

	if record2.InterfaceID != "Switch1/11/4" {
		t.Errorf("expected InterfaceID Switch1/11/4, got %s", record2.InterfaceID)
	}
	if record2.SystemID != "ac10.000d.0000" {
		t.Errorf("expected SystemID ac10.000d.0000, got %s", record2.SystemID)
	}
	if record2.NeighborIPv4 != "172.16.0.23" {
		t.Errorf("expected NeighborIPv4 172.16.0.23, got %s", record2.NeighborIPv4)
	}
	if record2.NeighborCircuitType != "LEVEL_2" {
		t.Errorf("expected NeighborCircuitType LEVEL_2, got %s", record2.NeighborCircuitType)
	}
	if record2.AreaAddress != "49.0000" {
		t.Errorf("expected AreaAddress 49.0000, got %s", record2.AreaAddress)
	}
	if record2.UpTimestamp != 1766077107000000000 {
		t.Errorf("expected UpTimestamp 1766077107000000000, got %d", record2.UpTimestamp)
	}
	if record2.LocalCircuitID != 218 {
		t.Errorf("expected LocalCircuitID 218, got %d", record2.LocalCircuitID)
	}
	if record2.NeighborCircuitID != 106 {
		t.Errorf("expected NeighborCircuitID 106, got %d", record2.NeighborCircuitID)
	}

	// Write to buffer and verify JSON output
	if err := writer.WriteRecords(ctx, records); err != nil {
		t.Fatalf("failed to write records: %v", err)
	}

	// Verify we have valid JSON output with _table field
	decoder := json.NewDecoder(&buf)
	var outputRecord map[string]any
	if err := decoder.Decode(&outputRecord); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}

	if outputRecord["_table"] != "isis_adjacencies" {
		t.Errorf("expected _table isis_adjacencies, got %v", outputRecord["_table"])
	}
	if outputRecord["system_id"] != "ac10.0001.0000" {
		t.Errorf("expected system_id ac10.0001.0000, got %v", outputRecord["system_id"])
	}
}

func TestProcessor_SystemHostname(t *testing.T) {
	// Load golden prototext
	resp := loadGoldenPrototext(t, "system_hostname.prototext")

	// Simulate Kafka round-trip
	resp = serializeAndDeserialize(t, resp)

	notification := resp.GetUpdate()
	if notification == nil {
		t.Fatal("expected update in SubscribeResponse")
	}

	// Verify basic fields
	if got := notification.GetPrefix().GetTarget(); got != "DZd011111111111111111111111111111111111111111" {
		t.Errorf("expected target DZd011111111111111111111111111111111111111111, got %s", got)
	}

	// Create processor with System extractor only
	var buf bytes.Buffer
	writer := NewFlatStdoutRecordWriter(WithFlatWriter(&buf))

	processor, err := NewProcessor(
		WithRecordWriter(writer),
		WithProcessorMetrics(newTestMetrics()),
		WithExtractors([]ExtractorDef{
			{"system_state", PathContains("/system/", "/state"), extractSystemState},
		}),
	)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	ctx := context.Background()
	records := processor.ProcessNotifications(ctx, []*gpb.Notification{notification})

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record, ok := records[0].(SystemStateRecord)
	if !ok {
		t.Fatalf("expected SystemStateRecord, got %T", records[0])
	}

	// Verify system hostname
	if record.DevicePubkey != "DZd011111111111111111111111111111111111111111" {
		t.Errorf("expected DevicePubkey DZd011111111111111111111111111111111111111111, got %s", record.DevicePubkey)
	}
	if record.Hostname != "e76554a34f51" {
		t.Errorf("expected Hostname e76554a34f51, got %s", record.Hostname)
	}

	// Write and verify JSON
	if err := writer.WriteRecords(ctx, records); err != nil {
		t.Fatalf("failed to write records: %v", err)
	}

	var outputRecord map[string]any
	if err := json.NewDecoder(&buf).Decode(&outputRecord); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}

	if outputRecord["_table"] != "system_state" {
		t.Errorf("expected _table system_state, got %v", outputRecord["_table"])
	}
	if outputRecord["hostname"] != "e76554a34f51" {
		t.Errorf("expected hostname e76554a34f51, got %v", outputRecord["hostname"])
	}
}

func TestProcessor_MultipleExtractors(t *testing.T) {
	// Test that the processor correctly routes to multiple extractors
	processor, err := NewProcessor(
		WithProcessorMetrics(newTestMetrics()),
		WithExtractors([]ExtractorDef{
			{"isis_adjacencies", PathContains("isis", "adjacencies"), extractIsisAdjacencies},
			{"system_state", PathContains("/system/", "/state"), extractSystemState},
		}),
	)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Load both test files
	isisResp := loadGoldenPrototext(t, "isis_adjacency.prototext")
	systemResp := loadGoldenPrototext(t, "system_hostname.prototext")

	isisNotification := isisResp.GetUpdate()
	systemNotification := systemResp.GetUpdate()

	ctx := context.Background()
	records := processor.ProcessNotifications(ctx, []*gpb.Notification{isisNotification, systemNotification})

	// Should have 2 ISIS records + 1 system record = 3 total
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Count record types
	isisCount := 0
	systemCount := 0
	for _, r := range records {
		switch r.(type) {
		case IsisAdjacencyRecord:
			isisCount++
		case SystemStateRecord:
			systemCount++
		}
	}

	if isisCount != 2 {
		t.Errorf("expected 2 ISIS records, got %d", isisCount)
	}
	if systemCount != 1 {
		t.Errorf("expected 1 system record, got %d", systemCount)
	}
}

func TestProcessor_DefaultExtractors(t *testing.T) {
	// Test that DefaultExtractors are used by default
	processor, err := NewProcessor(
		WithProcessorMetrics(newTestMetrics()),
	)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Load both test files
	isisResp := loadGoldenPrototext(t, "isis_adjacency.prototext")
	systemResp := loadGoldenPrototext(t, "system_hostname.prototext")

	isisNotification := isisResp.GetUpdate()
	systemNotification := systemResp.GetUpdate()

	ctx := context.Background()
	records := processor.ProcessNotifications(ctx, []*gpb.Notification{isisNotification, systemNotification})

	// Should have 2 ISIS records + 1 system record = 3 total (using DefaultExtractors)
	if len(records) != 3 {
		t.Fatalf("expected 3 records with default extractors, got %d", len(records))
	}
}

func TestProcessor_BinaryRoundTrip(t *testing.T) {
	// Test that binary protobuf serialization works correctly for ISIS data
	resp := loadGoldenPrototext(t, "isis_adjacency.prototext")

	// Serialize to binary
	data, err := proto.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	t.Logf("Binary size: %d bytes", len(data))

	// Deserialize
	var decoded gpb.SubscribeResponse
	if err := proto.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify the JSON value survived
	notification := decoded.GetUpdate()
	if notification == nil {
		t.Fatal("no notification after round-trip")
	}

	updates := notification.GetUpdate()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	// Check JSON value
	jsonVal := updates[0].GetVal().GetJsonIetfVal()
	if jsonVal == nil {
		t.Fatal("expected json_ietf_val")
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(jsonVal, &parsed); err != nil {
		t.Fatalf("json_ietf_val is not valid JSON: %v", err)
	}

	if _, ok := parsed["openconfig-network-instance:adjacency"]; !ok {
		t.Error("expected openconfig-network-instance:adjacency key in JSON")
	}
}

func TestPathContains(t *testing.T) {
	tests := []struct {
		name     string
		elems    []string
		path     *gpb.Path
		expected bool
	}{
		{
			name:  "isis adjacency path matches",
			elems: []string{"isis", "adjacencies"},
			path: &gpb.Path{
				Elem: []*gpb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "default"}},
					{Name: "protocols"},
					{Name: "protocol", Key: map[string]string{"identifier": "ISIS", "name": "1"}},
					{Name: "isis"},
					{Name: "interfaces"},
					{Name: "interface", Key: map[string]string{"interface-id": "eth0"}},
					{Name: "levels"},
					{Name: "level", Key: map[string]string{"level-number": "2"}},
					{Name: "adjacencies"},
				},
			},
			expected: true,
		},
		{
			name:  "interface path does not match isis",
			elems: []string{"isis", "adjacencies"},
			path: &gpb.Path{
				Elem: []*gpb.PathElem{
					{Name: "interfaces"},
					{Name: "interface", Key: map[string]string{"name": "eth0"}},
					{Name: "state"},
				},
			},
			expected: false,
		},
		{
			name:  "system path matches",
			elems: []string{"/system/", "/state"},
			path: &gpb.Path{
				Elem: []*gpb.PathElem{
					{Name: "system"},
					{Name: "state"},
					{Name: "hostname"},
				},
			},
			expected: true,
		},
		{
			name:  "system memory path matches",
			elems: []string{"/system/", "/state"},
			path: &gpb.Path{
				Elem: []*gpb.PathElem{
					{Name: "system"},
					{Name: "memory"},
					{Name: "state"},
					{Name: "physical"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := PathContains(tt.elems...)
			got := matcher(tt.path)
			if got != tt.expected {
				t.Errorf("PathContains(%v)(%v) = %v, want %v", tt.elems, tt.path, got, tt.expected)
			}
		})
	}
}

func TestRecordTableName(t *testing.T) {
	isisRecord := IsisAdjacencyRecord{}
	if got := isisRecord.TableName(); got != "isis_adjacencies" {
		t.Errorf("IsisAdjacencyRecord.TableName() = %s, want isis_adjacencies", got)
	}

	systemRecord := SystemStateRecord{}
	if got := systemRecord.TableName(); got != "system_state" {
		t.Errorf("SystemStateRecord.TableName() = %s, want system_state", got)
	}
}

func TestExtractIsisAdjacencies_Isolation(t *testing.T) {
	// Test the extractor function in isolation
	resp := loadGoldenPrototext(t, "isis_adjacency.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Get the first update and unmarshal it
	update := notification.GetUpdate()[0]
	device, err := processor.unmarshalNotification(notification, update)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	meta := Metadata{
		DevicePubkey: "test-device",
		Timestamp:    time.Unix(0, notification.GetTimestamp()),
	}

	records := extractIsisAdjacencies(device, meta)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0].(IsisAdjacencyRecord)
	if record.DevicePubkey != "test-device" {
		t.Errorf("expected DevicePubkey test-device, got %s", record.DevicePubkey)
	}
	if record.InterfaceID != "Switch1/11/2" {
		t.Errorf("expected InterfaceID Switch1/11/2, got %s", record.InterfaceID)
	}
}

func TestExtractSystemState_Isolation(t *testing.T) {
	// Test the extractor function in isolation
	resp := loadGoldenPrototext(t, "system_hostname.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Get the update and unmarshal it
	update := notification.GetUpdate()[0]
	device, err := processor.unmarshalNotification(notification, update)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	meta := Metadata{
		DevicePubkey: "test-device",
		Timestamp:    time.Unix(0, notification.GetTimestamp()),
	}

	records := extractSystemState(device, meta)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0].(SystemStateRecord)
	if record.DevicePubkey != "test-device" {
		t.Errorf("expected DevicePubkey test-device, got %s", record.DevicePubkey)
	}
	if record.Hostname != "e76554a34f51" {
		t.Errorf("expected Hostname e76554a34f51, got %s", record.Hostname)
	}
}

// newTestMetrics creates metrics with no-op implementations for testing.
func newTestMetrics() *ProcessorMetrics {
	return &ProcessorMetrics{
		RecordsProcessed:   &testCounter{},
		ProcessingErrors:   &testCounter{},
		ProcessingDuration: &testHistogram{},
		WriteErrors:        &testCounter{},
		CommitErrors:       &testCounter{},
	}
}

type testCounter struct{ val float64 }

func (c *testCounter) Inc()                             { c.val++ }
func (c *testCounter) Add(v float64)                    { c.val += v }
func (c *testCounter) Desc() *prometheus.Desc           { return nil }
func (c *testCounter) Write(*dto.Metric) error          { return nil }
func (c *testCounter) Describe(chan<- *prometheus.Desc) {}
func (c *testCounter) Collect(chan<- prometheus.Metric) {}

type testHistogram struct{}

func (h *testHistogram) Observe(float64)                  {}
func (h *testHistogram) Desc() *prometheus.Desc           { return nil }
func (h *testHistogram) Write(*dto.Metric) error          { return nil }
func (h *testHistogram) Describe(chan<- *prometheus.Desc) {}
func (h *testHistogram) Collect(chan<- prometheus.Metric) {}

// TestUnmarshal_BGPNeighbors tests the generic list unmarshalling with BGP neighbors.
// This verifies the unmarshal logic works for list types beyond ISIS adjacencies.
func TestUnmarshal_BGPNeighbors(t *testing.T) {
	resp := loadGoldenPrototext(t, "bgp_neighbors.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	update := notification.GetUpdate()[0]
	device, err := processor.unmarshalNotification(notification, update)
	if err != nil {
		t.Fatalf("failed to unmarshal BGP neighbors: %v", err)
	}

	// Verify the BGP neighbors were unmarshalled correctly (uncompressed paths)
	if device.NetworkInstances == nil {
		t.Fatal("expected NetworkInstances to be populated")
	}

	ni, ok := device.NetworkInstances.NetworkInstance["default"]
	if !ok {
		t.Fatal("expected network instance 'default'")
	}

	if ni.Protocols == nil {
		t.Fatal("expected Protocols to be populated")
	}

	// Find the BGP protocol
	var foundBgp bool
	for key, proto := range ni.Protocols.Protocol {
		if key.Identifier.String() == "BGP" && proto.Bgp != nil && proto.Bgp.Neighbors != nil {
			if len(proto.Bgp.Neighbors.Neighbor) == 0 {
				t.Fatal("expected BGP neighbors to be populated")
			}
			t.Logf("Found %d BGP neighbors", len(proto.Bgp.Neighbors.Neighbor))

			// Verify specific neighbor from the first update
			// The first update in the prototext contains neighbor 11.1.2.5 state
			if neighbor, ok := proto.Bgp.Neighbors.Neighbor["11.1.2.5"]; ok {
				// With uncompressed paths, NeighborAddress is in State
				if neighbor.State != nil && neighbor.State.NeighborAddress != nil {
					t.Logf("Neighbor 11.1.2.5: NeighborAddress=%v", *neighbor.State.NeighborAddress)
				}
			} else {
				t.Error("expected neighbor 11.1.2.5 to exist")
			}

			foundBgp = true
			break
		}
	}

	if !foundBgp {
		t.Fatal("expected BGP protocol to be found")
	}
}

// TestUnmarshal_Interfaces tests the generic list unmarshalling with interfaces.
// This verifies the unmarshal logic works for top-level lists.
func TestUnmarshal_Interfaces(t *testing.T) {
	resp := loadGoldenPrototext(t, "interfaces.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Track all interfaces found across all updates
	foundInterfaces := make(map[string]bool)

	for _, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal interfaces: %v", err)
		}

		// Verify the interfaces were unmarshalled correctly (uncompressed paths)
		if device.Interfaces == nil || len(device.Interfaces.Interface) == 0 {
			continue
		}

		for name, iface := range device.Interfaces.Interface {
			foundInterfaces[name] = true
			// Verify interface name matches state
			if iface.State != nil && iface.State.Name != nil {
				if *iface.State.Name != name {
					t.Errorf("expected interface name %s, got %v", name, *iface.State.Name)
				}
			}
		}
	}

	t.Logf("Found %d interfaces", len(foundInterfaces))

	// Verify Ethernet1 was found
	if !foundInterfaces["Ethernet1"] {
		t.Error("expected interface Ethernet1 to exist")
	}

	// Verify Ethernet2 was found
	if !foundInterfaces["Ethernet2"] {
		t.Error("expected interface Ethernet2 to exist")
	}
}

// TestUnmarshalNotifications_UncompressedPaths tests that with uncompressed paths,
// standard SetNode and UnmarshalNotifications work correctly without workarounds.
func TestUnmarshalNotifications_UncompressedPaths(t *testing.T) {
	resp := loadGoldenPrototext(t, "bgp_neighbors.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Find an update with full state data (peer-as, session-state, etc.)
	// Skip updates with supported-capabilities because those also contain Arista
	// vendor augments (neighbor-router-id, next-hop-self) which add noise to test output.
	var fullStateUpdateIdx = -1
	for i, update := range notification.GetUpdate() {
		if jsonVal := update.GetVal().GetJsonIetfVal(); jsonVal != nil {
			var data map[string]any
			if err := json.Unmarshal(jsonVal, &data); err == nil {
				_, hasPeerAs := data["openconfig-network-instance:peer-as"]
				_, hasCaps := data["openconfig-network-instance:supported-capabilities"]
				if hasPeerAs && !hasCaps {
					fullStateUpdateIdx = i
					t.Logf("Found full state update at index %d", i)
					break
				}
			}
		}
	}

	if fullStateUpdateIdx == -1 {
		t.Fatal("Could not find an update with full state data")
	}

	// Test SetNode approach
	update := notification.GetUpdate()[fullStateUpdateIdx]
	device, err := processor.unmarshalNotification(notification, update)
	if err != nil {
		t.Fatalf("SetNode approach failed: %v", err)
	}

	// With uncompressed paths, we access data through the full path including State container
	var hasFields bool
	if device.NetworkInstances != nil {
		for niName, ni := range device.NetworkInstances.NetworkInstance {
			if ni.Protocols == nil {
				continue
			}
			for key, proto := range ni.Protocols.Protocol {
				if proto.Bgp != nil && proto.Bgp.Neighbors != nil && len(proto.Bgp.Neighbors.Neighbor) > 0 {
					t.Logf("Found %d neighbors in %s/%s", len(proto.Bgp.Neighbors.Neighbor), niName, key.Identifier.String())
					for addr, neighbor := range proto.Bgp.Neighbors.Neighbor {
						t.Logf("  Neighbor %s:", addr)
						// With uncompressed paths, fields are in State container
						if neighbor.State != nil {
							if neighbor.State.PeerAs != nil {
								t.Logf("    PeerAs: %d", *neighbor.State.PeerAs)
								hasFields = true
							}
							if neighbor.State.SessionState != 0 {
								t.Logf("    SessionState: %s", neighbor.State.SessionState.String())
								hasFields = true
							}
							if neighbor.State.Description != nil {
								t.Logf("    Description: %s", *neighbor.State.Description)
								hasFields = true
							}
						}
					}
				}
			}
		}
	}

	if !hasFields {
		t.Error("Expected fields to be populated with uncompressed paths")
	} else {
		t.Log("SUCCESS: Fields are populated correctly with uncompressed paths!")
	}
}

// TestUnmarshal_InterfacesIfindex tests unmarshalling scalar values (uint_val) at leaf paths.
// This is different from JSON blobs - the path goes all the way to the leaf and the value is a scalar.
// Tests the /interfaces/interface/state/ifindex path.
func TestUnmarshal_InterfacesIfindex(t *testing.T) {
	resp := loadGoldenPrototext(t, "interfaces_ifindex.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Track which interfaces and ifindexes we've seen
	ifindexMap := make(map[string]uint32) // interface -> ifindex

	// Process each update
	for i, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal update %d: %v", i, err)
		}

		// Check that the interface was created (uncompressed paths)
		if device.Interfaces == nil || len(device.Interfaces.Interface) == 0 {
			t.Fatalf("update %d: expected interface to be populated", i)
		}

		for name, iface := range device.Interfaces.Interface {
			// Check interface-level ifindex
			if iface.State != nil && iface.State.Ifindex != nil {
				ifindexMap[name] = *iface.State.Ifindex
			}
		}
	}

	// Verify we found expected interfaces and ifindexes
	if idx, ok := ifindexMap["Ethernet1"]; !ok {
		t.Error("expected Ethernet1 to exist")
	} else if idx != 1 {
		t.Errorf("expected Ethernet1 ifindex=1, got %d", idx)
	}

	// Tunnel interfaces report ifindex=0 on some devices
	if idx, ok := ifindexMap["Tunnel500"]; !ok {
		t.Error("expected Tunnel500 to exist")
	} else if idx != 0 {
		t.Errorf("expected Tunnel500 ifindex=0, got %d", idx)
	}

	t.Logf("successfully unmarshalled %d interfaces", len(ifindexMap))
}

func TestExtractTransceiverState_Isolation(t *testing.T) {
	resp := loadGoldenPrototext(t, "transceiver_state.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	meta := Metadata{
		DevicePubkey: "test-device",
		Timestamp:    time.Unix(0, notification.GetTimestamp()),
	}

	// Unmarshal all updates and collect records
	var allRecords []Record
	for _, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		records := extractTransceiverState(device, meta)
		allRecords = append(allRecords, records...)
	}

	if len(allRecords) == 0 {
		t.Fatal("expected at least one transceiver state record")
	}

	// Verify first record has expected fields
	record := allRecords[0].(TransceiverStateRecord)
	if record.DevicePubkey != "test-device" {
		t.Errorf("expected DevicePubkey test-device, got %s", record.DevicePubkey)
	}

	// Find Ethernet1 channel 0 and verify values
	var eth1Found bool
	for _, r := range allRecords {
		rec := r.(TransceiverStateRecord)
		if rec.InterfaceName == "Ethernet1" && rec.ChannelIndex == 0 {
			eth1Found = true
			if rec.InputPower < -2.0 || rec.InputPower > -1.5 {
				t.Errorf("expected InputPower around -1.89, got %f", rec.InputPower)
			}
			break
		}
	}
	if !eth1Found {
		t.Error("expected Ethernet1 channel 0 record")
	}

	t.Logf("extracted %d transceiver state records", len(allRecords))
}

func TestExtractInterfaceState_Isolation(t *testing.T) {
	resp := loadGoldenPrototext(t, "interfaces.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	meta := Metadata{
		DevicePubkey: "test-device",
		Timestamp:    time.Unix(0, notification.GetTimestamp()),
	}

	// Unmarshal all updates and collect records
	var allRecords []Record
	for _, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		records := extractInterfaceState(device, meta)
		allRecords = append(allRecords, records...)
	}

	if len(allRecords) == 0 {
		t.Fatal("expected at least one interface state record")
	}

	// Build map for verification
	ifMap := make(map[string]InterfaceStateRecord)
	for _, r := range allRecords {
		rec := r.(InterfaceStateRecord)
		ifMap[rec.InterfaceName] = rec
	}

	// Verify Ethernet1
	if eth1, ok := ifMap["Ethernet1"]; !ok {
		t.Error("expected Ethernet1 record")
	} else {
		if eth1.AdminStatus != "UP" {
			t.Errorf("expected Ethernet1 AdminStatus UP, got %s", eth1.AdminStatus)
		}
		if eth1.OperStatus != "UP" {
			t.Errorf("expected Ethernet1 OperStatus UP, got %s", eth1.OperStatus)
		}
	}

	// Verify Ethernet2 (NOT_PRESENT)
	if eth2, ok := ifMap["Ethernet2"]; !ok {
		t.Error("expected Ethernet2 record")
	} else {
		if eth2.OperStatus != "NOT_PRESENT" {
			t.Errorf("expected Ethernet2 OperStatus NOT_PRESENT, got %s", eth2.OperStatus)
		}
	}

	t.Logf("extracted %d interface state records", len(allRecords))
}

func TestExtractTransceiverThresholds_Isolation(t *testing.T) {
	resp := loadGoldenPrototext(t, "transceiver_thresholds.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	meta := Metadata{
		DevicePubkey: "test-device",
		Timestamp:    time.Unix(0, notification.GetTimestamp()),
	}

	// Unmarshal all updates and collect records
	var allRecords []Record
	for _, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		records := extractTransceiverThresholds(device, meta)
		allRecords = append(allRecords, records...)
	}

	if len(allRecords) == 0 {
		t.Fatal("expected at least one transceiver threshold record")
	}

	// Find Ethernet50 WARNING threshold and verify
	var found bool
	for _, r := range allRecords {
		rec := r.(TransceiverThresholdRecord)
		if rec.InterfaceName == "Ethernet50" && rec.Severity == "WARNING" {
			found = true
			if rec.ModuleTemperatureUpper < 69.0 || rec.ModuleTemperatureUpper > 71.0 {
				t.Errorf("expected ModuleTemperatureUpper around 70.0, got %f", rec.ModuleTemperatureUpper)
			}
			break
		}
	}
	if !found {
		t.Error("expected Ethernet50 WARNING threshold record")
	}

	t.Logf("extracted %d transceiver threshold records", len(allRecords))
}

func TestAggregateTransceiverThresholds(t *testing.T) {
	timestamp := time.Now()

	// Create multiple records with the same key but different fields populated
	records := []Record{
		TransceiverThresholdRecord{
			Timestamp:       timestamp,
			DevicePubkey:    "device1",
			InterfaceName:   "Ethernet1",
			Severity:        "CRITICAL",
			InputPowerLower: -20.0,
		},
		TransceiverThresholdRecord{
			Timestamp:              timestamp,
			DevicePubkey:           "device1",
			InterfaceName:          "Ethernet1",
			Severity:               "CRITICAL",
			ModuleTemperatureUpper: 78.0,
		},
		TransceiverThresholdRecord{
			Timestamp:             timestamp,
			DevicePubkey:          "device1",
			InterfaceName:         "Ethernet1",
			Severity:              "CRITICAL",
			LaserBiasCurrentUpper: 13.0,
		},
		// Different severity - should not be merged
		TransceiverThresholdRecord{
			Timestamp:       timestamp,
			DevicePubkey:    "device1",
			InterfaceName:   "Ethernet1",
			Severity:        "WARNING",
			InputPowerLower: -15.0,
		},
		// Different interface - should not be merged
		TransceiverThresholdRecord{
			Timestamp:       timestamp,
			DevicePubkey:    "device1",
			InterfaceName:   "Ethernet2",
			Severity:        "CRITICAL",
			InputPowerLower: -18.0,
		},
		// Non-threshold record - should pass through unchanged
		IsisAdjacencyRecord{
			Timestamp:    timestamp,
			DevicePubkey: "device1",
			InterfaceID:  "Ethernet1",
		},
	}

	result := AggregateTransceiverThresholds(records)

	// Should have:
	// - 1 aggregated Ethernet1 CRITICAL
	// - 1 Ethernet1 WARNING
	// - 1 Ethernet2 CRITICAL
	// - 1 IsisAdjacencyRecord
	// Total: 4 records

	var thresholdCount, isisCount int
	var eth1Critical, eth1Warning, eth2Critical *TransceiverThresholdRecord

	for _, r := range result {
		switch rec := r.(type) {
		case TransceiverThresholdRecord:
			thresholdCount++
			if rec.InterfaceName == "Ethernet1" && rec.Severity == "CRITICAL" {
				eth1Critical = &rec
			} else if rec.InterfaceName == "Ethernet1" && rec.Severity == "WARNING" {
				eth1Warning = &rec
			} else if rec.InterfaceName == "Ethernet2" && rec.Severity == "CRITICAL" {
				eth2Critical = &rec
			}
		case IsisAdjacencyRecord:
			isisCount++
		}
	}

	if thresholdCount != 3 {
		t.Errorf("expected 3 threshold records, got %d", thresholdCount)
	}
	if isisCount != 1 {
		t.Errorf("expected 1 ISIS record, got %d", isisCount)
	}

	// Verify Ethernet1 CRITICAL has all merged fields
	if eth1Critical == nil {
		t.Fatal("expected Ethernet1 CRITICAL record")
	}
	if eth1Critical.InputPowerLower != -20.0 {
		t.Errorf("expected InputPowerLower=-20.0, got %f", eth1Critical.InputPowerLower)
	}
	if eth1Critical.ModuleTemperatureUpper != 78.0 {
		t.Errorf("expected ModuleTemperatureUpper=78.0, got %f", eth1Critical.ModuleTemperatureUpper)
	}
	if eth1Critical.LaserBiasCurrentUpper != 13.0 {
		t.Errorf("expected LaserBiasCurrentUpper=13.0, got %f", eth1Critical.LaserBiasCurrentUpper)
	}

	// Verify Ethernet1 WARNING is separate
	if eth1Warning == nil {
		t.Fatal("expected Ethernet1 WARNING record")
	}
	if eth1Warning.InputPowerLower != -15.0 {
		t.Errorf("expected InputPowerLower=-15.0, got %f", eth1Warning.InputPowerLower)
	}

	// Verify Ethernet2 CRITICAL is separate
	if eth2Critical == nil {
		t.Fatal("expected Ethernet2 CRITICAL record")
	}
	if eth2Critical.InputPowerLower != -18.0 {
		t.Errorf("expected InputPowerLower=-18.0, got %f", eth2Critical.InputPowerLower)
	}

	t.Logf("aggregated %d records into %d records", len(records), len(result))
}

func TestAggregateTransceiverState(t *testing.T) {
	timestamp := time.Now()

	// Create multiple records with the same key but different fields populated
	records := []Record{
		TransceiverStateRecord{
			Timestamp:     timestamp,
			DevicePubkey:  "device1",
			InterfaceName: "Ethernet1",
			ChannelIndex:  0,
			InputPower:    -1.89,
		},
		TransceiverStateRecord{
			Timestamp:     timestamp,
			DevicePubkey:  "device1",
			InterfaceName: "Ethernet1",
			ChannelIndex:  0,
			OutputPower:   -2.39,
		},
		TransceiverStateRecord{
			Timestamp:        timestamp,
			DevicePubkey:     "device1",
			InterfaceName:    "Ethernet1",
			ChannelIndex:     0,
			LaserBiasCurrent: 6.19,
		},
		// Different channel - should not be merged
		TransceiverStateRecord{
			Timestamp:     timestamp,
			DevicePubkey:  "device1",
			InterfaceName: "Ethernet1",
			ChannelIndex:  1,
			InputPower:    -1.75,
		},
		// Different interface - should not be merged
		TransceiverStateRecord{
			Timestamp:     timestamp,
			DevicePubkey:  "device1",
			InterfaceName: "Ethernet2",
			ChannelIndex:  0,
			InputPower:    -2.52,
		},
		// Non-transceiver record - should pass through unchanged
		IsisAdjacencyRecord{
			Timestamp:    timestamp,
			DevicePubkey: "device1",
			InterfaceID:  "Ethernet1",
		},
	}

	result := AggregateTransceiverState(records)

	// Should have:
	// - 1 aggregated Ethernet1 channel 0
	// - 1 Ethernet1 channel 1
	// - 1 Ethernet2 channel 0
	// - 1 IsisAdjacencyRecord
	// Total: 4 records

	var stateCount, isisCount int
	var eth1Ch0, eth1Ch1, eth2Ch0 *TransceiverStateRecord

	for _, r := range result {
		switch rec := r.(type) {
		case TransceiverStateRecord:
			stateCount++
			if rec.InterfaceName == "Ethernet1" && rec.ChannelIndex == 0 {
				eth1Ch0 = &rec
			} else if rec.InterfaceName == "Ethernet1" && rec.ChannelIndex == 1 {
				eth1Ch1 = &rec
			} else if rec.InterfaceName == "Ethernet2" && rec.ChannelIndex == 0 {
				eth2Ch0 = &rec
			}
		case IsisAdjacencyRecord:
			isisCount++
		}
	}

	if stateCount != 3 {
		t.Errorf("expected 3 transceiver state records, got %d", stateCount)
	}
	if isisCount != 1 {
		t.Errorf("expected 1 ISIS record, got %d", isisCount)
	}

	// Verify Ethernet1 channel 0 has all merged fields
	if eth1Ch0 == nil {
		t.Fatal("expected Ethernet1 channel 0 record")
	}
	if eth1Ch0.InputPower != -1.89 {
		t.Errorf("expected InputPower=-1.89, got %f", eth1Ch0.InputPower)
	}
	if eth1Ch0.OutputPower != -2.39 {
		t.Errorf("expected OutputPower=-2.39, got %f", eth1Ch0.OutputPower)
	}
	if eth1Ch0.LaserBiasCurrent != 6.19 {
		t.Errorf("expected LaserBiasCurrent=6.19, got %f", eth1Ch0.LaserBiasCurrent)
	}

	// Verify Ethernet1 channel 1 is separate
	if eth1Ch1 == nil {
		t.Fatal("expected Ethernet1 channel 1 record")
	}
	if eth1Ch1.InputPower != -1.75 {
		t.Errorf("expected InputPower=-1.75, got %f", eth1Ch1.InputPower)
	}

	// Verify Ethernet2 channel 0 is separate
	if eth2Ch0 == nil {
		t.Fatal("expected Ethernet2 channel 0 record")
	}
	if eth2Ch0.InputPower != -2.52 {
		t.Errorf("expected InputPower=-2.52, got %f", eth2Ch0.InputPower)
	}

	t.Logf("aggregated %d records into %d records", len(records), len(result))
}
