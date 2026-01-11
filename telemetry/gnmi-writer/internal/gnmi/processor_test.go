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
	if got := notification.GetPrefix().GetTarget(); got != "chi-dn-dzd1" {
		t.Errorf("expected target chi-dn-dzd1, got %s", got)
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

	if record1.DeviceCode != "chi-dn-dzd1" {
		t.Errorf("expected DeviceCode chi-dn-dzd1, got %s", record1.DeviceCode)
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
	if got := notification.GetPrefix().GetTarget(); got != "dzd01" {
		t.Errorf("expected target dzd01, got %s", got)
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
	if record.DeviceCode != "dzd01" {
		t.Errorf("expected DeviceCode dzd01, got %s", record.DeviceCode)
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
		DeviceCode: "test-device",
		Timestamp:  time.Unix(0, notification.GetTimestamp()),
	}

	records := extractIsisAdjacencies(device, meta)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0].(IsisAdjacencyRecord)
	if record.DeviceCode != "test-device" {
		t.Errorf("expected DeviceCode test-device, got %s", record.DeviceCode)
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
		DeviceCode: "test-device",
		Timestamp:  time.Unix(0, notification.GetTimestamp()),
	}

	records := extractSystemState(device, meta)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0].(SystemStateRecord)
	if record.DeviceCode != "test-device" {
		t.Errorf("expected DeviceCode test-device, got %s", record.DeviceCode)
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

	// Verify the BGP neighbors were unmarshalled correctly
	ni, ok := device.NetworkInstance["default"]
	if !ok {
		t.Fatal("expected network instance 'default'")
	}

	// Find the BGP protocol
	var bgp *struct {
		neighbors map[string]any
	}
	for key, proto := range ni.Protocol {
		if key.Identifier.String() == "BGP" && proto.Bgp != nil {
			if len(proto.Bgp.Neighbor) == 0 {
				t.Fatal("expected BGP neighbors to be populated")
			}
			t.Logf("Found %d BGP neighbors", len(proto.Bgp.Neighbor))

			// Verify specific neighbors
			if neighbor, ok := proto.Bgp.Neighbor["10.0.0.1"]; ok {
				if neighbor.NeighborAddress == nil || *neighbor.NeighborAddress != "10.0.0.1" {
					t.Errorf("expected neighbor address 10.0.0.1, got %v", neighbor.NeighborAddress)
				}
				t.Logf("Neighbor 10.0.0.1: PeerAs=%v", neighbor.PeerAs)
			} else {
				t.Error("expected neighbor 10.0.0.1 to exist")
			}

			if neighbor, ok := proto.Bgp.Neighbor["10.0.0.2"]; ok {
				if neighbor.NeighborAddress == nil || *neighbor.NeighborAddress != "10.0.0.2" {
					t.Errorf("expected neighbor address 10.0.0.2, got %v", neighbor.NeighborAddress)
				}
			} else {
				t.Error("expected neighbor 10.0.0.2 to exist")
			}

			bgp = &struct{ neighbors map[string]any }{}
			break
		}
	}

	if bgp == nil {
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

	update := notification.GetUpdate()[0]
	device, err := processor.unmarshalNotification(notification, update)
	if err != nil {
		t.Fatalf("failed to unmarshal interfaces: %v", err)
	}

	// Verify the interfaces were unmarshalled correctly
	if len(device.Interface) == 0 {
		t.Fatal("expected interfaces to be populated")
	}

	t.Logf("Found %d interfaces", len(device.Interface))

	// Verify Ethernet1
	if iface, ok := device.Interface["Ethernet1"]; ok {
		if iface.Name == nil || *iface.Name != "Ethernet1" {
			t.Errorf("expected interface name Ethernet1, got %v", iface.Name)
		}
		if iface.Mtu != nil {
			t.Logf("Ethernet1 MTU: %d", *iface.Mtu)
		}
	} else {
		t.Error("expected interface Ethernet1 to exist")
	}

	// Verify Ethernet2
	if iface, ok := device.Interface["Ethernet2"]; ok {
		if iface.Name == nil || *iface.Name != "Ethernet2" {
			t.Errorf("expected interface name Ethernet2, got %v", iface.Name)
		}
	} else {
		t.Error("expected interface Ethernet2 to exist")
	}
}

// TestUnmarshal_InterfacesIfindex tests unmarshalling scalar values (uint_val) at leaf paths.
// This is different from JSON blobs - the path goes all the way to the leaf and the value is a scalar.
func TestUnmarshal_InterfacesIfindex(t *testing.T) {
	resp := loadGoldenPrototext(t, "interfaces_ifindex.prototext")
	resp = serializeAndDeserialize(t, resp)
	notification := resp.GetUpdate()

	processor, err := NewProcessor()
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	// Process each update (there are 2 - one for Ethernet1, one for Tunnel1)
	for i, update := range notification.GetUpdate() {
		device, err := processor.unmarshalNotification(notification, update)
		if err != nil {
			t.Fatalf("failed to unmarshal update %d: %v", i, err)
		}

		// Check that the interface was created
		if len(device.Interface) == 0 {
			t.Fatalf("update %d: expected interface to be populated", i)
		}

		for name, iface := range device.Interface {
			t.Logf("update %d: interface %s", i, name)

			// Check subinterface exists
			if len(iface.Subinterface) == 0 {
				t.Errorf("update %d: expected subinterface for %s", i, name)
				continue
			}

			// Get subinterface 0
			subif, ok := iface.Subinterface[0]
			if !ok {
				t.Errorf("update %d: expected subinterface index 0 for %s", i, name)
				continue
			}

			// Check ifindex was set
			if subif.Ifindex != nil {
				t.Logf("update %d: %s subinterface 0 ifindex: %d", i, name, *subif.Ifindex)
			} else {
				t.Errorf("update %d: expected ifindex for %s subinterface 0", i, name)
			}
		}
	}
}
