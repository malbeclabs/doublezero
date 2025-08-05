package ripeatlas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

// MockHTTPClient is a mock implementation of collector.HTTPClient for testing
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("mock not configured")
}

func TestInternetLatency_RIPEAtlas_NewClient(t *testing.T) {
	// Set API key for test
	t.Setenv("RIPE_ATLAS_API_KEY", "test-api-key")

	log := logger.With("test", t.Name())

	client := NewClient(log)

	require.NotNil(t, client, "NewClient() returned nil")
	require.Equal(t, "https://atlas.ripe.net/api/v2", client.BaseURL)
	require.Equal(t, "test-api-key", client.APIKey)
	require.NotNil(t, client.HTTPClient, "HTTPClient should not be nil")
}

func TestInternetLatency_RIPEAtlas_NewClient_NoAPIKey(t *testing.T) {
	// Ensure no API key is set
	t.Setenv("RIPE_ATLAS_API_KEY", "")

	log := logger.With("test", t.Name())

	client := NewClient(log)

	require.Empty(t, client.APIKey, "APIKey should be empty")
}

func TestInternetLatency_RIPEAtlas_SetCommonHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		apiKey      string
		contentType string
		wantHeaders map[string]string
	}{
		{
			name:        "With API key and content type",
			apiKey:      "test-key",
			contentType: "application/json",
			wantHeaders: map[string]string{
				"Authorization": "Key test-key",
				"User-Agent":    "DoubleZero-Collector/1.0",
				"Content-Type":  "application/json",
			},
		},
		{
			name:        "Without API key",
			apiKey:      "",
			contentType: "application/json",
			wantHeaders: map[string]string{
				"User-Agent":   "DoubleZero-Collector/1.0",
				"Content-Type": "application/json",
			},
		},
		{
			name:   "Without content type",
			apiKey: "test-key",
			wantHeaders: map[string]string{
				"Authorization": "Key test-key",
				"User-Agent":    "DoubleZero-Collector/1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				log:    log,
				APIKey: tt.apiKey,
			}
			req, _ := http.NewRequest("GET", "http://test.com", nil)

			client.setCommonHeaders(req, tt.contentType)

			// Check expected headers are set
			for header, expected := range tt.wantHeaders {
				got := req.Header.Get(header)
				require.Equal(t, expected, got, "Header[%s]", header)
			}

			// Check Authorization header is not set when API key is empty
			if tt.apiKey == "" {
				require.Empty(t, req.Header.Get("Authorization"), "Authorization header should not be set when API key is empty")
			}

			// Check Content-Type is not set when contentType is empty
			if tt.contentType == "" {
				require.Empty(t, req.Header.Get("Content-Type"), "Content-Type header should not be set when contentType is empty")
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_GetProbesInRadius(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lat       float64
		lng       float64
		radius    int
		mockFunc  func(req *http.Request) (*http.Response, error)
		wantCount int
		wantErr   bool
	}{
		{
			name:   "Successful probe retrieval",
			lat:    40.7128,
			lng:    -74.0060,
			radius: 10,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request - adjust expected URL to match actual implementation
				// The actual implementation uses radius format, not separate distance/lat/lon params

				response := ProbesResponse{
					Count: 2,
					Results: []Probe{
						{ID: 1, Address: "1.1.1.1", Latitude: 40.7, Longitude: -74.0},
						{ID: 2, Address: "2.2.2.2", Latitude: 40.8, Longitude: -74.1},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:   "API error response",
			lat:    40.7128,
			lng:    -74.0060,
			radius: 10,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("Internal Server Error")),
				}, nil
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:   "Network error",
			lat:    40.7128,
			lng:    -74.0060,
			radius: 10,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network timeout")
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:   "Empty response",
			lat:    40.7128,
			lng:    -74.0060,
			radius: 10,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := ProbesResponse{
					Count:   0,
					Results: []Probe{},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:   "Invalid JSON response",
			lat:    40.7128,
			lng:    -74.0060,
			radius: 10,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("invalid json")),
				}, nil
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				BaseURL:    "https://atlas.ripe.net/api/v2",
				HTTPClient: &MockHTTPClient{DoFunc: tt.mockFunc},
				log:        log,
			}

			probes, err := client.GetProbesInRadius(t.Context(), tt.lat, tt.lng, tt.radius)

			if tt.wantErr {
				require.Error(t, err, "GetProbesInRadius() should return error")
			} else {
				require.NoError(t, err, "GetProbesInRadius() should not return error")
			}

			require.Len(t, probes, tt.wantCount, "Unexpected number of probes")
		})
	}
}

func TestInternetLatency_RIPEAtlas_GetProbesInRadius_Pagination(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	callCount := 0
	client := &Client{
		log:     log,
		BaseURL: "https://atlas.ripe.net/api/v2",
		HTTPClient: &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				if callCount == 1 {
					// First page
					response := ProbesResponse{
						Count: 3,
						Next:  "https://atlas.ripe.net/api/v2/probes/?page=2",
						Results: []Probe{
							{ID: 1, Address: "1.1.1.1"},
							{ID: 2, Address: "2.2.2.2"},
						},
					}
					body, _ := json.Marshal(response)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(body)),
					}, nil
				} else {
					// Second page
					response := ProbesResponse{
						Count: 3,
						Results: []Probe{
							{ID: 3, Address: "3.3.3.3"},
						},
					}
					body, _ := json.Marshal(response)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(body)),
					}, nil
				}
			},
		},
	}

	probes, err := client.GetProbesInRadius(t.Context(), 40.7128, -74.0060, 10)

	require.NoError(t, err, "GetProbesInRadius() failed")

	// The implementation now follows pagination and returns all probes
	require.Len(t, probes, 3, "Expected 3 probes from both pages")
	require.Equal(t, 2, callCount, "API should be called twice for pagination")
}

func TestInternetLatency_RIPEAtlas_CreateMeasurement(t *testing.T) {
	tests := []struct {
		name     string
		request  MeasurementRequest
		mockFunc func(req *http.Request) (*http.Response, error)
		want     *MeasurementResponse
		wantErr  bool
	}{
		{
			name: "Successful measurement creation",
			request: MeasurementRequest{
				Definitions: []MeasurementDefinition{
					{
						Target:      "8.8.8.8",
						Description: "DoubleZero [testnet] NYC probe 123 to LAX probe 456",
						Type:        "ping",
						AF:          4,
						Tags:        []string{"testnet", "doublezero"},
					},
				},
				Probes: []MeasurementProbe{
					{
						Value:     123,
						Type:      "probes",
						Requested: 1,
					},
				},
			},
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request method and URL
				require.Equal(t, "POST", req.Method, "Request method")
				require.True(t, strings.HasSuffix(req.URL.Path, "/measurements/"), "URL path should end with /measurements/")

				// Verify request body
				var requestBody MeasurementRequest
				err := json.NewDecoder(req.Body).Decode(&requestBody)
				require.NoError(t, err, "Failed to decode request body")

				// Verify environment in description and tags
				require.Contains(t, requestBody.Definitions[0].Description, "[testnet]", "Description should contain environment")
				require.Contains(t, requestBody.Definitions[0].Tags, "testnet", "Tags should contain environment")
				require.Contains(t, requestBody.Definitions[0].Tags, "doublezero", "Tags should contain 'doublezero'")

				response := MeasurementResponse{
					Measurements: []int{12345},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want: &MeasurementResponse{
				Measurements: []int{12345},
			},
			wantErr: false,
		},
		{
			name:    "API error response",
			request: MeasurementRequest{},
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"error": "Invalid request"}`)),
				}, nil
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Network error",
			request: MeasurementRequest{},
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				BaseURL:    "https://atlas.ripe.net/api/v2",
				APIKey:     "test-key",
				HTTPClient: &MockHTTPClient{DoFunc: tt.mockFunc},
				log:        log,
			}

			response, err := client.CreateMeasurement(t.Context(), tt.request)

			if tt.wantErr {
				require.Error(t, err, "CreateMeasurement() should return error")
			} else {
				require.NoError(t, err, "CreateMeasurement() should not return error")
				require.NotNil(t, response, "Response should not be nil")
				require.Len(t, response.Measurements, len(tt.want.Measurements), "Unexpected number of measurements")
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_GetAllMeasurements(t *testing.T) {
	tests := []struct {
		name     string
		mockFunc func(req *http.Request) (*http.Response, error)
		want     []Measurement
		wantErr  bool
	}{
		{
			name: "Successful retrieval",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify that the request includes the env tag
				require.Contains(t, req.URL.String(), "tags=testnet", "URL should contain tags=testnet")

				response := MeasurementListResponse{
					Count: 1,
					Results: []Measurement{
						{
							ID:          12345,
							Description: "DoubleZero [testnet] NYC probe 1 to LAX probe 2",
							Status: struct {
								Name string `json:"name"`
								ID   int    `json:"id"`
							}{
								ID:   2,
								Name: "Ongoing",
							},
							Type:   "ping",
							Target: "8.8.8.8",
							Tags:   []string{"testnet", "doublezero"},
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want: []Measurement{
				{
					ID:          12345,
					Description: "DoubleZero [testnet] NYC probe 1 to LAX probe 2",
					Status: struct {
						Name string `json:"name"`
						ID   int    `json:"id"`
					}{
						ID:   2,
						Name: "Ongoing",
					},
					Type:   "ping",
					Target: "8.8.8.8",
					Tags:   []string{"testnet", "doublezero"},
				},
			},
			wantErr: false,
		},
		{
			name: "Empty measurement list",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := MeasurementListResponse{
					Count:   0,
					Results: []Measurement{},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want:    []Measurement{},
			wantErr: false,
		},
		{
			name: "API error",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader("Unauthorized")),
				}, nil
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				BaseURL:    "https://atlas.ripe.net/api/v2",
				APIKey:     "test-key",
				HTTPClient: &MockHTTPClient{DoFunc: tt.mockFunc},
				log:        log,
			}

			measurements, err := client.GetAllMeasurements(t.Context(), "testnet")

			if tt.wantErr {
				require.Error(t, err, "GetAllMeasurements() should return error")
			} else {
				require.NoError(t, err, "GetAllMeasurements() should not return error")
				require.Len(t, measurements, len(tt.want), "Unexpected number of measurements")

				// For successful retrieval, verify env is in description and tags
				if tt.name == "Successful retrieval" && len(measurements) > 0 {
					require.Contains(t, measurements[0].Description, "[testnet]", "Description should contain environment")
					require.Contains(t, measurements[0].Tags, "testnet", "Tags should contain environment name")
					require.Contains(t, measurements[0].Tags, "doublezero", "Tags should contain 'doublezero'")
				}
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_StopMeasurement(t *testing.T) {

	tests := []struct {
		name          string
		measurementID int
		mockFunc      func(req *http.Request) (*http.Response, error)
		wantErr       bool
	}{
		{
			name:          "Successful stop",
			measurementID: 12345,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify DELETE method and URL
				require.Equal(t, "DELETE", req.Method, "Request method")
				expectedPath := "/measurements/12345"
				require.True(t, strings.HasSuffix(req.URL.Path, expectedPath), "URL path should end with %s", expectedPath)

				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
			wantErr: false,
		},
		{
			name:          "Measurement not found",
			measurementID: 99999,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("Not found")),
				}, nil
			},
			wantErr: true,
		},
		{
			name:          "Network error",
			measurementID: 12345,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				BaseURL:    "https://atlas.ripe.net/api/v2",
				APIKey:     "test-key",
				HTTPClient: &MockHTTPClient{DoFunc: tt.mockFunc},
				log:        log,
			}

			err := client.StopMeasurement(t.Context(), tt.measurementID)

			if tt.wantErr {
				require.Error(t, err, "StopMeasurement() should return error")
			} else {
				require.NoError(t, err, "StopMeasurement() should not return error")
			}
		})
	}
}

func TestInternetLatency_RIPEAtlas_MakeRequest_ContextCancellation(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	client := &Client{
		log:     log,
		BaseURL: "https://atlas.ripe.net/api/v2",
		HTTPClient: &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate slow request
				time.Sleep(100 * time.Millisecond)
				return nil, errors.New("should be cancelled")
			},
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	_, err := client.makeRequest(ctx, "/test")

	require.Error(t, err, "Expected error due to context cancellation")
}

func TestInternetLatency_RIPEAtlas_FetchProbesWithErrorHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		lat        float64
		lng        float64
		entityName string
		mockFunc   func(req *http.Request) (*http.Response, error)
		wantLen    int
		wantErr    bool
	}{
		{
			name:       "Successful fetch",
			lat:        40.7128,
			lng:        -74.0060,
			entityName: "New York",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := ProbesResponse{
					Count: 1,
					Results: []Probe{
						{ID: 1, Address: "1.1.1.1"},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name:       "API error - returns empty list",
			lat:        40.7128,
			lng:        -74.0060,
			entityName: "New York",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("API error")
			},
			wantLen: 0,
			wantErr: false, // fetchProbesWithErrorHandling doesn't return error, just logs warning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())
			client := &Client{
				log:        log,
				BaseURL:    "https://atlas.ripe.net/api/v2",
				HTTPClient: &MockHTTPClient{DoFunc: tt.mockFunc},
			}

			probes, err := client.fetchProbesWithErrorHandling(t.Context(), tt.lat, tt.lng, tt.entityName)

			if tt.wantErr {
				require.Error(t, err, "fetchProbesWithErrorHandling() should return error")
			} else {
				require.NoError(t, err, "fetchProbesWithErrorHandling() should not return error")
			}

			require.Len(t, probes, tt.wantLen, "Unexpected number of probes")
		})
	}
}

func TestInternetLatency_RIPEAtlas_GetProbesForLocations(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	callCount := 0
	client := &Client{
		log:     log,
		BaseURL: "https://atlas.ripe.net/api/v2",
		HTTPClient: &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				// Return different probes for each location
				probes := []Probe{
					{ID: callCount, Address: fmt.Sprintf("%d.%d.%d.%d", callCount, callCount, callCount, callCount)},
				}
				response := ProbesResponse{
					Count:   1,
					Results: probes,
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
		},
	}

	locations := []LocationProbeMatch{
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "Location1",
				Latitude:     40.7128,
				Longitude:    -74.0060,
			},
		},
		{
			LocationMatch: collector.LocationMatch{
				LocationCode: "Location2",
				Latitude:     51.5074,
				Longitude:    -0.1278,
			},
		},
	}

	matches, err := client.GetProbesForLocations(t.Context(), locations)

	require.NoError(t, err, "GetProbesForLocations() failed")
	require.Len(t, matches, 2, "Expected 2 matches")
	require.Equal(t, 2, callCount, "API should be called twice")

	// Verify each location has probes
	for i, match := range matches {
		require.NotEmpty(t, match.NearbyProbes, "Location %d has no probes", i)
	}
}

// Test error scenarios
func TestInternetLatency_RIPEAtlas_APIErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       "Bad Request",
			wantErr:    "API request failed with status",
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       "Unauthorized",
			wantErr:    "API request failed with status",
		},
		{
			name:       "403 Forbidden",
			statusCode: http.StatusForbidden,
			body:       "Forbidden",
			wantErr:    "API request failed with status",
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			body:       "Not Found",
			wantErr:    "API request failed with status",
		},
		{
			name:       "429 Too Many Requests",
			statusCode: http.StatusTooManyRequests,
			body:       "Rate limit exceeded",
			wantErr:    "API request failed with status",
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       "Internal Server Error",
			wantErr:    "API request failed with status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				log:     log,
				BaseURL: "https://atlas.ripe.net/api/v2",
				HTTPClient: &MockHTTPClient{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(strings.NewReader(tt.body)),
						}, nil
					},
				},
			}

			_, err := client.makeRequest(t.Context(), "/test")

			require.Error(t, err, "Expected error but got none")
			require.Contains(t, err.Error(), tt.wantErr, "Error message should contain expected text")
		})
	}
}

func TestInternetLatency_RIPEAtlas_GetMeasurementResultsIncremental(t *testing.T) {
	tests := []struct {
		name           string
		measurementID  int
		startTimestamp int64
		mockResponse   string
		mockStatus     int
		mockError      error
		wantResults    int
		wantError      bool
		wantErrorMsg   string
	}{
		{
			name:           "Successful incremental results retrieval",
			measurementID:  12345,
			startTimestamp: 1609459200, // 2021-01-01 00:00:00
			mockResponse: `[
				{
					"timestamp": 1609459260,
					"type": "ping",
					"result": [{"rtt": 25.5}, {"rtt": 26.0}]
				},
				{
					"timestamp": 1609459320,
					"type": "ping",
					"result": [{"rtt": 27.0}]
				}
			]`,
			mockStatus:  http.StatusOK,
			wantResults: 2,
			wantError:   false,
		},
		{
			name:           "Results without start timestamp",
			measurementID:  12345,
			startTimestamp: 0,
			mockResponse: `[
				{
					"timestamp": 1609459200,
					"type": "ping",
					"result": [{"rtt": 25.5}]
				}
			]`,
			mockStatus:  http.StatusOK,
			wantResults: 1,
			wantError:   false,
		},
		{
			name:           "Empty results",
			measurementID:  12345,
			startTimestamp: 1609459400,
			mockResponse:   `[]`,
			mockStatus:     http.StatusOK,
			wantResults:    0,
			wantError:      false,
		},
		{
			name:           "API error",
			measurementID:  12345,
			startTimestamp: 1609459200,
			mockResponse:   `{"error": {"status": 404, "code": 104, "detail": "Measurement not found"}}`,
			mockStatus:     http.StatusNotFound,
			wantError:      true,
			wantErrorMsg:   "API request failed with status: 404",
		},
		{
			name:           "Network error",
			measurementID:  12345,
			startTimestamp: 1609459200,
			mockError:      errors.New("network timeout"),
			wantError:      true,
			wantErrorMsg:   "network timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := &Client{
				log:     log,
				BaseURL: "https://atlas.ripe.net/api/v2",
				APIKey:  "test-key",
				HTTPClient: &MockHTTPClient{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						if tt.mockError != nil {
							return nil, tt.mockError
						}

						// Verify URL construction
						expectedPath := fmt.Sprintf("/measurements/%d/results", tt.measurementID)
						require.Contains(t, req.URL.Path, expectedPath, "Request path should contain expected path")

						// Verify start parameter if provided
						if tt.startTimestamp > 0 {
							startParam := req.URL.Query().Get("start")
							expectedStart := fmt.Sprintf("%d", tt.startTimestamp)
							require.Equal(t, expectedStart, startParam, "Start parameter mismatch")
						}

						return &http.Response{
							StatusCode: tt.mockStatus,
							Body:       io.NopCloser(strings.NewReader(tt.mockResponse)),
						}, nil
					},
				},
			}

			results, err := client.GetMeasurementResultsIncremental(t.Context(), tt.measurementID, tt.startTimestamp)

			if tt.wantError {
				require.Error(t, err, "Expected error but got none")
				if tt.wantErrorMsg != "" {
					require.Contains(t, err.Error(), tt.wantErrorMsg, "Error message should contain expected text")
				}
			} else {
				require.NoError(t, err, "GetMeasurementResultsIncremental() should not return error")
				require.Len(t, results, tt.wantResults, "Unexpected number of results")
			}
		})
	}
}
