package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const validNodeFile = `[
  {"code": "us-east-1", "cloud": "aws", "lat": 39.0438, "lng": -77.4874, "atlas_probe_ids": [1003385], "ping_target": "34.192.0.54"},
  {"code": "eu-central-1", "cloud": "aws", "lat": 50.1109, "lng": 8.6821, "atlas_probe_ids": [1000566, 1000567], "ping_target": "3.64.0.0"}
]`

func TestInternetLatency_NodeFile_LoadNodesFromJSON(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	testFile := filepath.Join(t.TempDir(), "nodes.json")
	require.NoError(t, os.WriteFile(testFile, []byte(validNodeFile), 0644))

	nodes, err := LoadNodesFromJSON(log, testFile)
	require.NoError(t, err, "LoadNodesFromJSON() failed")

	require.Len(t, nodes, 2)

	require.Equal(t, "us-east-1", nodes[0].Code)
	require.Equal(t, "aws", nodes[0].Cloud)
	require.InDelta(t, 39.0438, nodes[0].Latitude, 0.0001)
	require.InDelta(t, -77.4874, nodes[0].Longitude, 0.0001)
	require.Equal(t, []int{1003385}, nodes[0].AtlasProbeIDs)
	require.Equal(t, "34.192.0.54", nodes[0].PingTarget)

	require.Equal(t, "eu-central-1", nodes[1].Code)
	require.Equal(t, []int{1000566, 1000567}, nodes[1].AtlasProbeIDs)
	require.Equal(t, "3.64.0.0", nodes[1].PingTarget)
}

func TestInternetLatency_NodeFile_LoadNodesFromJSON_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		jsonContent string
		wantReason  string
	}{
		{
			name:        "Empty array",
			jsonContent: `[]`,
			wantReason:  "node file contains empty array",
		},
		{
			name:        "Missing code",
			jsonContent: `[{"cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "1.2.3.4"}]`,
			wantReason:  "missing code",
		},
		{
			name:        "Missing cloud",
			jsonContent: `[{"code": "us-east-1", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "1.2.3.4"}]`,
			wantReason:  "missing cloud",
		},
		{
			name:        "Zero coordinates",
			jsonContent: `[{"code": "us-east-1", "cloud": "aws", "lat": 0.0, "lng": 0.0, "atlas_probe_ids": [1], "ping_target": "1.2.3.4"}]`,
			wantReason:  "invalid coordinates",
		},
		{
			name:        "No probe ids",
			jsonContent: `[{"code": "us-east-1", "cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [], "ping_target": "1.2.3.4"}]`,
			wantReason:  "no atlas probe ids",
		},
		{
			name:        "Ping target not an address",
			jsonContent: `[{"code": "us-east-1", "cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "not-an-address"}]`,
			wantReason:  "ping target is not an IPv4 address",
		},
		{
			name:        "Ping target is IPv6",
			jsonContent: `[{"code": "us-east-1", "cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "2001:db8::1"}]`,
			wantReason:  "ping target is not an IPv4 address",
		},
		{
			name:        "Ping target is not routable",
			jsonContent: `[{"code": "us-east-1", "cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "10.0.0.1"}]`,
			wantReason:  "ping target is not a routable address",
		},
		{
			name: "Duplicate code",
			jsonContent: `[
  {"code": "us-east-1", "cloud": "aws", "lat": 1.0, "lng": 2.0, "atlas_probe_ids": [1], "ping_target": "1.2.3.4"},
  {"code": "us-east-1", "cloud": "aws", "lat": 3.0, "lng": 4.0, "atlas_probe_ids": [2], "ping_target": "5.6.7.8"}
]`,
			wantReason: "duplicate code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			testFile := filepath.Join(t.TempDir(), "nodes.json")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.jsonContent), 0644))

			_, err := LoadNodesFromJSON(log, testFile)
			require.Error(t, err, "expected an error but got none")

			var collectorErr *CollectorError
			require.True(t, isCollectorErrorLocation(err, &collectorErr),
				"error should be CollectorError, got %T", err)
			require.Equal(t, "node_file_validation", collectorErr.Operation)
			require.Equal(t, tt.wantReason, collectorErr.GetContext("reason"))
		})
	}
}

func TestInternetLatency_NodeFile_LoadNodesFromJSON_InvalidJSON(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	testFile := filepath.Join(t.TempDir(), "nodes.json")
	require.NoError(t, os.WriteFile(testFile, []byte(`{"code": "us-east-1"}`), 0644))

	_, err := LoadNodesFromJSON(log, testFile)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid JSON format")
}

func TestInternetLatency_NodeFile_LoadNodesFromJSON_NonexistentFile(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	_, err := LoadNodesFromJSON(log, "nonexistent_nodes.json")
	require.Error(t, err)
	require.ErrorContains(t, err, "no such file or directory")
}
