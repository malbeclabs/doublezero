package isis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocator(t *testing.T) {
	locator, err := NewLocator()
	if err != nil {
		t.Fatalf("failed to create locator: %v", err)
	}

	tests := []struct {
		hostname string
		want     string
	}{
		{"DZ-NY7-SW01", "NYC"},
		{"DZ-NY5-SW01", "NYC"},
		{"njwb-dz001", "NYC"},
		{"DZ-LD4-SW01", "London"},
		{"fra-router-01", "Frankfurt"},
		{"ams-sw-01", "Amsterdam"},
		{"chi-core-01", "Chicago"},
		{"lax-edge-01", "Los Angeles"},
		{"dal-sw-01", "Dallas"},
		{"sea-router-01", "Seattle"},
		{"tor-sw-01", "Toronto"},
		{"mtl-sw-01", "Montreal"},
		{"sg1-router-01", "Singapore"},
		{"tyo-sw-01", "Tokyo"},
		{"hk1-router-01", "Hong Kong"},
		{"dub-sw-01", "Dublin"},
		{"syd-router-01", "Sydney"},
		{"slc-sw-01", "Salt Lake City"},
		{"ash-router-01", "Ashburn"},
		{"unknown-router", "Other"},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := locator.Infer(tt.hostname)
			if got != tt.want {
				t.Errorf("Infer(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestLocatorDescription(t *testing.T) {
	locator, err := NewLocator()
	if err != nil {
		t.Fatalf("failed to create locator: %v", err)
	}

	desc := locator.Description("NYC")
	if desc != "New York / New Jersey" {
		t.Errorf("Description(NYC) = %q, want %q", desc, "New York / New Jersey")
	}

	// Unknown location should return itself
	desc = locator.Description("Unknown")
	if desc != "Unknown" {
		t.Errorf("Description(Unknown) = %q, want %q", desc, "Unknown")
	}
}

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{
			"2026-01-06T15-42-13Z_upload_data.json",
			"2026-01-06 15:42:13 UTC",
		},
		{
			"/path/to/2025-11-13T15-42-03Z_upload_data.json",
			"2025-11-13 15:42:03 UTC",
		},
		{
			"invalid_filename.json",
			"invalid_filename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractTimestamp(tt.path)
			if got != tt.want {
				t.Errorf("extractTimestamp(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseAndEnrich(t *testing.T) {
	// Create test JSON
	testJSON := `{
		"vrfs": {
			"default": {
				"isisInstances": {
					"1": {
						"level": {
							"2": {
								"lsps": {
									"test.0001.0000.00-00": {
										"hostname": {"name": "DZ-NY7-SW01"},
										"intermediateSystemType": "L2",
										"areaAddresses": [{"address": "49.0000"}],
										"flags": {"dbOverload": false},
										"sequence": 1234,
										"interfaceAddresses": [
											{"ipv4Address": "172.16.0.1"},
											{"ipv4Address": "172.16.0.2"}
										],
										"neighbors": [
											{
												"systemId": "DZ-NY5-SW01.00",
												"metric": 200,
												"neighborAddr": "172.16.0.3",
												"adjSids": [{"adjSid": 100001}]
											}
										],
										"reachabilities": [
											{
												"reachabilityV4Addr": "172.16.0.1",
												"maskLength": 32,
												"metric": 10,
												"srPrefixReachabilities": [
													{
														"sid": 101,
														"options": {"nodeSID": true}
													}
												]
											}
										],
										"routerCapabilities": [
											{
												"routerId": "172.16.0.1",
												"srCapabilities": [
													{
														"srCapabilitySrgb": [
															{"srgbBase": 900000, "srgbRange": 65536}
														]
													}
												],
												"srlb": {
													"srlbRanges": [
														{"srlbBase": 965536, "srlbRange": 65536}
													]
												},
												"msd": {"baseMplsImposition": 12}
											}
										]
									},
									"test.0002.0000.00-00": {
										"hostname": {"name": "DZ-NY5-SW01"},
										"intermediateSystemType": "L2",
										"areaAddresses": [{"address": "49.0000"}],
										"flags": {"dbOverload": false},
										"sequence": 5678,
										"interfaceAddresses": [
											{"ipv4Address": "172.16.0.3"}
										],
										"neighbors": [
											{
												"systemId": "DZ-NY7-SW01.00",
												"metric": 200,
												"neighborAddr": "172.16.0.1",
												"adjSids": [{"adjSid": 100002}]
											}
										],
										"reachabilities": [
											{
												"reachabilityV4Addr": "172.16.0.3",
												"maskLength": 32,
												"metric": 10,
												"srPrefixReachabilities": [
													{
														"sid": 102,
														"options": {"nodeSID": true}
													}
												]
											}
										],
										"routerCapabilities": [
											{
												"routerId": "172.16.0.3",
												"srCapabilities": [
													{
														"srCapabilitySrgb": [
															{"srgbBase": 900000, "srgbRange": 65536}
														]
													}
												],
												"srlb": {},
												"msd": {}
											}
										]
									}
								}
							}
						}
					}
				}
			}
		}
	}`

	enricher, err := NewEnricher(EnricherConfig{Level: 2})
	if err != nil {
		t.Fatalf("failed to create enricher: %v", err)
	}

	result, err := enricher.EnrichFromReader(
		context.Background(),
		strings.NewReader(testJSON),
		"2026-01-06 15:42:13 UTC",
	)
	if err != nil {
		t.Fatalf("failed to enrich: %v", err)
	}

	// Verify routers
	if len(result.Routers) != 2 {
		t.Errorf("expected 2 routers, got %d", len(result.Routers))
	}

	router1, ok := result.Routers["DZ-NY7-SW01"]
	if !ok {
		t.Fatal("router DZ-NY7-SW01 not found")
	}

	if router1.RouterID != "172.16.0.1" {
		t.Errorf("expected router ID 172.16.0.1, got %s", router1.RouterID)
	}

	if router1.Location != "NYC" {
		t.Errorf("expected location NYC, got %s", router1.Location)
	}

	if router1.NodeSID == nil || *router1.NodeSID != 101 {
		t.Errorf("expected node SID 101, got %v", router1.NodeSID)
	}

	if len(router1.Neighbors) != 1 {
		t.Errorf("expected 1 neighbor, got %d", len(router1.Neighbors))
	}

	// Verify stats
	if result.Stats.TotalRouters != 2 {
		t.Errorf("expected 2 total routers, got %d", result.Stats.TotalRouters)
	}

	if result.Stats.TotalLinks != 1 {
		t.Errorf("expected 1 link, got %d", result.Stats.TotalLinks)
	}

	if result.Stats.SREnabledRouters != 2 {
		t.Errorf("expected 2 SR-enabled routers, got %d", result.Stats.SREnabledRouters)
	}

	if !result.Stats.SRGBConsistent {
		t.Error("expected SRGB to be consistent")
	}

	// Verify markdown contains expected sections
	if !strings.Contains(result.Markdown, "# ISIS Network - 2026-01-06 15:42:13 UTC") {
		t.Error("markdown missing header")
	}

	if !strings.Contains(result.Markdown, "## Network Summary") {
		t.Error("markdown missing Network Summary section")
	}

	if !strings.Contains(result.Markdown, "## Quick Reference") {
		t.Error("markdown missing Quick Reference section")
	}

	if !strings.Contains(result.Markdown, "## Adjacency List") {
		t.Error("markdown missing Adjacency List section")
	}

	if !strings.Contains(result.Markdown, "## Router Details") {
		t.Error("markdown missing Router Details section")
	}

	if !strings.Contains(result.Markdown, "### DZ-NY7-SW01") {
		t.Error("markdown missing router detail for DZ-NY7-SW01")
	}
}

func TestComputeStats(t *testing.T) {
	routers := map[string]Router{
		"router1": {
			Hostname:     "router1",
			IsOverloaded: false,
			NodeSID:      ptrInt(100),
			SRGBBase:     ptrInt(900000),
			SRGBEnd:      ptrInt(965535),
			Neighbors: []Neighbor{
				{Hostname: "router2", Metric: 200},
				{Hostname: "router3", Metric: 60000}, // High cost
			},
		},
		"router2": {
			Hostname:     "router2",
			IsOverloaded: true,
			NodeSID:      ptrInt(101),
			SRGBBase:     ptrInt(900000),
			SRGBEnd:      ptrInt(965535),
			Neighbors: []Neighbor{
				{Hostname: "router1", Metric: 200},
			},
		},
		"router3": {
			Hostname:     "router3",
			IsOverloaded: false,
			Neighbors: []Neighbor{
				{Hostname: "router1", Metric: 60000},
			},
		},
		"isolated": {
			Hostname:     "isolated",
			IsOverloaded: false,
			Neighbors:    []Neighbor{},
		},
	}

	stats := computeStats(routers)

	if stats.TotalRouters != 4 {
		t.Errorf("expected 4 routers, got %d", stats.TotalRouters)
	}

	if stats.HealthyRouters != 3 {
		t.Errorf("expected 3 healthy routers, got %d", stats.HealthyRouters)
	}

	if stats.OverloadedRouters != 1 {
		t.Errorf("expected 1 overloaded router, got %d", stats.OverloadedRouters)
	}

	if stats.IsolatedRouters != 1 {
		t.Errorf("expected 1 isolated router, got %d", stats.IsolatedRouters)
	}

	if stats.SREnabledRouters != 2 {
		t.Errorf("expected 2 SR-enabled routers, got %d", stats.SREnabledRouters)
	}

	if stats.TotalLinks != 2 {
		t.Errorf("expected 2 unique links, got %d", stats.TotalLinks)
	}

	if len(stats.HighCostLinks) != 1 {
		t.Errorf("expected 1 high cost link, got %d", len(stats.HighCostLinks))
	}

	if stats.MinSID != 100 || stats.MaxSID != 101 {
		t.Errorf("expected SID range 100-101, got %d-%d", stats.MinSID, stats.MaxSID)
	}
}

func TestFindLatestJSON(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	files := []string{
		"2025-11-13T15-42-03Z_upload_data.json",
		"2025-11-14T15-42-03Z_upload_data.json",
		"2025-11-12T15-42-03Z_upload_data.json",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	latest, err := FindLatestJSON(tmpDir)
	if err != nil {
		t.Fatalf("failed to find latest JSON: %v", err)
	}

	expected := filepath.Join(tmpDir, "2025-11-14T15-42-03Z_upload_data.json")
	if latest != expected {
		t.Errorf("expected %s, got %s", expected, latest)
	}
}

func TestFindLatestJSONEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	latest, err := FindLatestJSON(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if latest != "" {
		t.Errorf("expected empty string for empty directory, got %s", latest)
	}
}

// Integration test that uses real data files if available
func TestIntegrationWithRealData(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Look for data directory relative to test
	dataDir := "../../../../data"
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Skip("data directory not found, skipping integration test")
	}

	latestFile, err := FindLatestJSON(dataDir)
	if err != nil || latestFile == "" {
		t.Skip("no test data files found")
	}

	enricher, err := NewEnricher(EnricherConfig{Level: 2})
	if err != nil {
		t.Fatalf("failed to create enricher: %v", err)
	}

	result, err := enricher.EnrichFromFile(context.Background(), latestFile)
	if err != nil {
		t.Fatalf("failed to enrich real data: %v", err)
	}

	// Basic sanity checks
	if result.Stats.TotalRouters == 0 {
		t.Error("expected at least one router from real data")
	}

	if len(result.Markdown) == 0 {
		t.Error("expected non-empty markdown output")
	}

	t.Logf("Processed %d routers, %d links from %s",
		result.Stats.TotalRouters, result.Stats.TotalLinks, latestFile)
}
