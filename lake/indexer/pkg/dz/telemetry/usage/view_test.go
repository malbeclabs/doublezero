package dztelemusage

import (
	"context"
	"testing"
	"time"

	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"

	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

type mockInfluxDBClient struct {
	querySQLFunc func(ctx context.Context, sqlQuery string) ([]map[string]any, error)
	closeFunc    func() error
}

func (m *mockInfluxDBClient) QuerySQL(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
	if m.querySQLFunc != nil {
		return m.querySQLFunc(ctx, sqlQuery)
	}
	return []map[string]any{}, nil
}

func (m *mockInfluxDBClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestLake_TelemetryUsage_View_ViewConfig_Validate(t *testing.T) {
	t.Parallel()

	t.Run("returns error when logger is missing", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "logger is required")
	})

	t.Run("returns error when db is missing", func(t *testing.T) {
		t.Parallel()
		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			ClickHouse:      nil,
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "clickhouse connection is required")
	})

	t.Run("returns error when influxdb client is missing", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "influxdb client is required")
	})

	t.Run("returns error when bucket is empty", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			RefreshInterval: time.Second,
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "influxdb bucket is required")
	})

	t.Run("returns error when refresh interval is zero", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: 0,
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "refresh interval must be greater than 0")
	})

	t.Run("sets default query window when zero", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     0,
		}
		err := cfg.Validate()
		require.NoError(t, err)
		require.Equal(t, 1*time.Hour, cfg.QueryWindow)
	})

	t.Run("sets default clock when nil", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			Clock:           nil,
		}
		err := cfg.Validate()
		require.NoError(t, err)
		require.NotNil(t, cfg.Clock)
	})

	t.Run("validates successfully with all required fields", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		cfg := ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     2 * time.Hour,
		}
		err := cfg.Validate()
		require.NoError(t, err)
	})
}

func TestLake_TelemetryUsage_View_NewView(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()
		view, err := NewView(ViewConfig{})
		require.Error(t, err)
		require.Nil(t, view)
	})

	t.Run("creates view successfully", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)
		require.NotNil(t, view)
		require.NotNil(t, view.Store())
	})
}

func TestLake_TelemetryUsage_View_extractTunnelIDFromInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected *int64
	}{
		{
			name:     "valid tunnel interface",
			input:    "Tunnel501",
			expected: int64Ptr(501),
		},
		{
			name:     "valid tunnel interface with large number",
			input:    "Tunnel12345",
			expected: int64Ptr(12345),
		},
		{
			name:     "interface without Tunnel prefix",
			input:    "eth0",
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "just Tunnel prefix",
			input:    "Tunnel",
			expected: nil,
		},
		{
			name:     "Tunnel with non-numeric suffix",
			input:    "Tunnelabc",
			expected: nil,
		},
		{
			name:     "Tunnel with mixed suffix",
			input:    "Tunnel501abc",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractTunnelIDFromInterface(tt.input)
			if tt.expected == nil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestLake_TelemetryUsage_View_buildLinkLookup(t *testing.T) {
	t.Parallel()

	t.Run("builds link lookup map successfully", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		// Insert test link data using serviceability store
		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: mockDB,
		})
		require.NoError(t, err)

		links := []dzsvc.Link{
			{
				PK:             "link1",
				SideAPK:        "device1",
				SideAIfaceName: "eth0",
				SideZPK:        "device2",
				SideZIfaceName: "eth1",
			},
			{
				PK:             "link2",
				SideAPK:        "device3",
				SideAIfaceName: "eth0",
				SideZPK:        "device4",
				SideZIfaceName: "eth0",
			},
		}
		err = svcStore.ReplaceLinks(context.Background(), links)
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		lookup, err := view.buildLinkLookup(context.Background())
		require.NoError(t, err)
		require.NotNil(t, lookup)

		// Verify side A mappings
		link1SideA, ok := lookup["device1:eth0"]
		require.True(t, ok)
		require.Equal(t, "link1", link1SideA.LinkPK)
		require.Equal(t, "A", link1SideA.LinkSide)

		link2SideA, ok := lookup["device3:eth0"]
		require.True(t, ok)
		require.Equal(t, "link2", link2SideA.LinkPK)
		require.Equal(t, "A", link2SideA.LinkSide)

		// Verify side Z mappings
		link1SideZ, ok := lookup["device2:eth1"]
		require.True(t, ok)
		require.Equal(t, "link1", link1SideZ.LinkPK)
		require.Equal(t, "Z", link1SideZ.LinkSide)

		link2SideZ, ok := lookup["device4:eth0"]
		require.True(t, ok)
		require.Equal(t, "link2", link2SideZ.LinkPK)
		require.Equal(t, "Z", link2SideZ.LinkSide)
	})

	t.Run("handles empty links table", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		// Tables are created via migrations, no need to create them here
		// This test verifies that buildLinkLookup works with empty tables
		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		lookup, err := view.buildLinkLookup(context.Background())
		require.NoError(t, err)
		require.NotNil(t, lookup)
		require.Equal(t, 0, len(lookup))
	})
}

func TestLake_TelemetryUsage_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		require.False(t, view.Ready())
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		// With mock, we can't create tables - they're created via migrations
		// The buildLinkLookup will query the mock, which should return empty results

		clock := clockwork.NewFakeClock()

		mockInflux := &mockInfluxDBClient{
			querySQLFunc: func(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
				// Return empty result for baseline queries and main query
				return []map[string]any{}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clock,
			ClickHouse:      mockDB,
			InfluxDB:        mockInflux,
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		require.False(t, view.Ready())

		err = view.Refresh(t.Context())
		require.NoError(t, err)

		require.True(t, view.Ready())
	})
}

func TestLake_TelemetryUsage_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when already ready", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		// With mock, we can't create tables - they're created via migrations

		clock := clockwork.NewFakeClock()
		mockInflux := &mockInfluxDBClient{
			querySQLFunc: func(ctx context.Context, sqlQuery string) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clock,
			ClickHouse:      mockDB,
			InfluxDB:        mockInflux,
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		err = view.Refresh(t.Context())
		require.NoError(t, err)

		// Should return immediately
		err = view.WaitReady(t.Context())
		require.NoError(t, err)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		// With mock, we can't create tables - they're created via migrations

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestLake_TelemetryUsage_View_Store(t *testing.T) {
	t.Parallel()

	t.Run("returns the underlying store", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		store := view.Store()
		require.NotNil(t, store)
	})
}

func TestLake_TelemetryUsage_View_convertRowsToUsage(t *testing.T) {
	t.Parallel()

	t.Run("converts rows with tunnel ID extraction", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		// Use sparse counters (errors) so first row is not skipped
		rows := []map[string]any{
			{
				"time":       now.Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "Tunnel501",
				"model_name": "ModelX",
				"in-errors":  int64(1), // Sparse counter
			},
			{
				"time":       now.Add(time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device2",
				"intf":       "eth0",
				"in-errors":  int64(2), // Sparse counter
			},
		}

		baselines := &CounterBaselines{
			InErrors: make(map[string]*int64),
		}

		linkLookup := map[string]LinkInfo{
			"device1:Tunnel501": {LinkPK: "link1", LinkSide: "A"},
		}

		usage, err := view.convertRowsToUsage(rows, baselines, linkLookup, nil)
		require.NoError(t, err)
		require.Len(t, usage, 2)

		// Check first row with tunnel ID
		require.NotNil(t, usage[0].UserTunnelID)
		require.Equal(t, int64(501), *usage[0].UserTunnelID)
		require.NotNil(t, usage[0].LinkPK)
		require.Equal(t, "link1", *usage[0].LinkPK)
		require.NotNil(t, usage[0].LinkSide)
		require.Equal(t, "A", *usage[0].LinkSide)

		// Check second row without tunnel ID
		require.Nil(t, usage[1].UserTunnelID)
		require.Nil(t, usage[1].LinkPK)
	})

	t.Run("handles first row as baseline for non-sparse counters", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		rows := []map[string]any{
			{
				"time":       now.Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-octets":  int64(1000), // Non-sparse counter
			},
			{
				"time":       now.Add(time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-octets":  int64(2000), // Second row should have delta
			},
		}

		baselines := &CounterBaselines{
			InDiscards:  make(map[string]*int64),
			InErrors:    make(map[string]*int64),
			InFCSErrors: make(map[string]*int64),
			OutDiscards: make(map[string]*int64),
			OutErrors:   make(map[string]*int64),
		}

		usage, err := view.convertRowsToUsage(rows, baselines, make(map[string]LinkInfo), nil)
		require.NoError(t, err)
		// First row should be skipped (used as baseline), so only second row should be stored
		require.Len(t, usage, 1)
		require.NotNil(t, usage[0].InOctetsDelta)
		require.Equal(t, int64(1000), *usage[0].InOctetsDelta) // 2000 - 1000
	})

	t.Run("computes delta duration", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		rows := []map[string]any{
			{
				"time":       now.Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(1), // Sparse counter, so first row is stored
			},
			{
				"time":       now.Add(60 * time.Second).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(2),
			},
		}

		baselines := &CounterBaselines{
			InErrors: make(map[string]*int64),
		}

		usage, err := view.convertRowsToUsage(rows, baselines, make(map[string]LinkInfo), nil)
		require.NoError(t, err)
		require.Len(t, usage, 2)

		// First row should not have delta_duration
		require.Nil(t, usage[0].DeltaDuration)

		// Second row should have delta_duration of 60 seconds
		require.NotNil(t, usage[1].DeltaDuration)
		require.InDelta(t, 60.0, *usage[1].DeltaDuration, 0.01)
	})

	t.Run("skips already-written rows", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		rows := []map[string]any{
			{
				"time":       now.Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(1),
			},
			{
				"time":       now.Add(time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(2),
			},
			{
				"time":       now.Add(2 * time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(3),
			},
		}

		baselines := &CounterBaselines{
			InErrors: make(map[string]*int64),
		}

		// Mark the first row's timestamp as already written
		alreadyWritten := MaxTimestampsByKey{
			"device1:eth0": now,
		}

		usage, err := view.convertRowsToUsage(rows, baselines, make(map[string]LinkInfo), alreadyWritten)
		require.NoError(t, err)
		// First row should be skipped (already written), so only rows 2 and 3 should be stored
		require.Len(t, usage, 2)

		// Verify we got the second and third rows (timestamps after the already-written max)
		require.Equal(t, now.Add(time.Minute), usage[0].Time)
		require.Equal(t, now.Add(2*time.Minute), usage[1].Time)
	})

	t.Run("skips all rows up to and including already-written timestamp", func(t *testing.T) {
		t.Parallel()
		mockDB := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			ClickHouse:      mockDB,
			InfluxDB:        &mockInfluxDBClient{},
			Bucket:          "test-bucket",
			RefreshInterval: time.Second,
			QueryWindow:     time.Hour,
		})
		require.NoError(t, err)

		now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		rows := []map[string]any{
			{
				"time":       now.Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(1),
			},
			{
				"time":       now.Add(time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(2),
			},
			{
				"time":       now.Add(2 * time.Minute).Format(time.RFC3339Nano),
				"dzd_pubkey": "device1",
				"intf":       "eth0",
				"in-errors":  int64(3),
			},
		}

		baselines := &CounterBaselines{
			InErrors: make(map[string]*int64),
		}

		// Mark up to the second row's timestamp as already written
		alreadyWritten := MaxTimestampsByKey{
			"device1:eth0": now.Add(time.Minute),
		}

		usage, err := view.convertRowsToUsage(rows, baselines, make(map[string]LinkInfo), alreadyWritten)
		require.NoError(t, err)
		// First two rows should be skipped (at or before already-written timestamp), only third row stored
		require.Len(t, usage, 1)

		// Verify we got only the third row
		require.Equal(t, now.Add(2*time.Minute), usage[0].Time)
	})
}
