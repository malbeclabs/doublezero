package isis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3SourceConfig(t *testing.T) {
	t.Run("defaults applied", func(t *testing.T) {
		assert.Equal(t, "doublezero-mn-beta-isis-db", DefaultBucket)
		assert.Equal(t, "us-east-1", DefaultRegion)
	})
}

func TestMockSource(t *testing.T) {
	t.Run("fetch latest returns dump", func(t *testing.T) {
		data := createTestISISDump(t, []struct {
			hostname  string
			systemID  string
			routerID  string
			neighbors []struct {
				systemID     string
				metric       uint32
				neighborAddr string
				adjSIDs      []uint32
			}
		}{
			{
				hostname: "DZ-NY7-SW01",
				systemID: "ac10.0001.0000.00-00",
				routerID: "172.16.0.1",
				neighbors: []struct {
					systemID     string
					metric       uint32
					neighborAddr string
					adjSIDs      []uint32
				}{
					{
						systemID:     "ac10.0002.0000",
						metric:       1000,
						neighborAddr: "172.16.0.117",
						adjSIDs:      []uint32{100001},
					},
				},
			},
		})

		source := NewMockSource(data, "2024-01-15T12-00-00Z_upload_data.json")

		ctx := context.Background()
		dump, err := source.FetchLatest(ctx)
		require.NoError(t, err)
		assert.NotNil(t, dump)
		assert.Equal(t, "2024-01-15T12-00-00Z_upload_data.json", dump.FileName)
		assert.Equal(t, data, dump.RawJSON)
		assert.False(t, dump.FetchedAt.IsZero())

		// Parse the dump
		lsps, err := Parse(dump.RawJSON)
		require.NoError(t, err)
		assert.Len(t, lsps, 1)
		assert.Equal(t, "DZ-NY7-SW01", lsps[0].Hostname)
	})

	t.Run("fetch latest returns error", func(t *testing.T) {
		source := &MockSource{
			FetchErr: errors.New("network error"),
		}

		ctx := context.Background()
		dump, err := source.FetchLatest(ctx)
		assert.Error(t, err)
		assert.Nil(t, dump)
		assert.Contains(t, err.Error(), "network error")
	})

	t.Run("close marks source as closed", func(t *testing.T) {
		source := NewMockSource([]byte("{}"), "test.json")
		assert.False(t, source.Closed)

		err := source.Close()
		assert.NoError(t, err)
		assert.True(t, source.Closed)
	})

	t.Run("source implements interface", func(t *testing.T) {
		var _ Source = (*MockSource)(nil)
		var _ Source = (*S3Source)(nil)
	})
}

func TestParseISISJSONStructure(t *testing.T) {
	// Test the JSON structure that we expect from the S3 bucket
	sampleJSON := map[string]any{
		"vrfs": map[string]any{
			"default": map[string]any{
				"isisInstances": map[string]any{
					"1": map[string]any{
						"level": map[string]any{
							"2": map[string]any{
								"lsps": map[string]any{
									"ac10.0001.0000.00-00": map[string]any{
										"hostname": map[string]any{
											"name": "DZ-NY7-SW01",
										},
										"routerCapabilities": []any{map[string]any{
											"routerId":  "172.16.0.1",
											"srgbBase":  16000,
											"srgbRange": 8000,
										}},
										"neighbors": []any{
											map[string]any{
												"systemId":     "ac10.0002.0000",
												"metric":       float64(1000),
												"neighborAddr": "172.16.0.117",
												"adjSids":      []any{map[string]any{"adjSid": float64(100001)}},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(sampleJSON)
	require.NoError(t, err)

	lsps, err := Parse(data)
	require.NoError(t, err)
	assert.Len(t, lsps, 1)
	assert.Equal(t, "DZ-NY7-SW01", lsps[0].Hostname)
	assert.Equal(t, "172.16.0.1", lsps[0].RouterID)
	assert.Len(t, lsps[0].Neighbors, 1)
	assert.Equal(t, uint32(1000), lsps[0].Neighbors[0].Metric)
	assert.Equal(t, "172.16.0.117", lsps[0].Neighbors[0].NeighborAddr)
}

func TestS3SourceClose(t *testing.T) {
	// S3Source.Close() should be a no-op
	source := &S3Source{
		client: nil,
		bucket: "test-bucket",
	}

	err := source.Close()
	assert.NoError(t, err)
}

// Helper function for creating test data
func createTestISISDump(t *testing.T, devices []struct {
	hostname  string
	systemID  string
	routerID  string
	neighbors []struct {
		systemID     string
		metric       uint32
		neighborAddr string
		adjSIDs      []uint32
	}
}) []byte {
	t.Helper()

	lsps := make(map[string]any)
	for _, device := range devices {
		neighbors := make([]any, 0, len(device.neighbors))
		for _, n := range device.neighbors {
			adjSids := make([]any, 0, len(n.adjSIDs))
			for _, sid := range n.adjSIDs {
				adjSids = append(adjSids, map[string]any{"adjSid": float64(sid)})
			}
			neighbors = append(neighbors, map[string]any{
				"systemId":     n.systemID,
				"metric":       float64(n.metric),
				"neighborAddr": n.neighborAddr,
				"adjSids":      adjSids,
			})
		}

		lsps[device.systemID] = map[string]any{
			"hostname": map[string]any{
				"name": device.hostname,
			},
			"routerCapabilities": []any{map[string]any{
				"routerId": device.routerID,
			}},
			"neighbors": neighbors,
		}
	}

	dump := map[string]any{
		"vrfs": map[string]any{
			"default": map[string]any{
				"isisInstances": map[string]any{
					"1": map[string]any{
						"level": map[string]any{
							"2": map[string]any{
								"lsps": lsps,
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(dump)
	require.NoError(t, err)
	return data
}

func TestCreateTestISISDump(t *testing.T) {
	data := createTestISISDump(t, []struct {
		hostname  string
		systemID  string
		routerID  string
		neighbors []struct {
			systemID     string
			metric       uint32
			neighborAddr string
			adjSIDs      []uint32
		}
	}{
		{
			hostname: "DZ-NY7-SW01",
			systemID: "ac10.0001.0000.00-00",
			routerID: "172.16.0.1",
			neighbors: []struct {
				systemID     string
				metric       uint32
				neighborAddr string
				adjSIDs      []uint32
			}{
				{
					systemID:     "ac10.0002.0000",
					metric:       1000,
					neighborAddr: "172.16.0.117",
					adjSIDs:      []uint32{100001},
				},
			},
		},
	})

	lsps, err := Parse(data)
	require.NoError(t, err)
	assert.Len(t, lsps, 1)
	assert.Equal(t, "DZ-NY7-SW01", lsps[0].Hostname)
}
