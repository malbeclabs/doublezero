package wheresitup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

// MockHTTPClient implements HTTPClient interface for testing
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
	}, nil
}

func TestNewWheresitupClient(t *testing.T) {
	// Set test API token
	os.Setenv("WHERESITUP_API_TOKEN", "CLIENT_ID test-token-123")
	defer os.Unsetenv("WHERESITUP_API_TOKEN")

	log := logger.With("test", t.Name())

	client := NewClient(log)

	require.NotNil(t, client, "NewWheresitupClient() returned nil")
	require.Equal(t, "https://api.wheresitup.com/v4", client.BaseURL)
	require.Equal(t, "CLIENT_ID test-token-123", client.APIToken)
	require.NotNil(t, client.HTTPClient, "HTTPClient should not be nil")
}

func TestNewWheresitupClient_NoAPIToken(t *testing.T) {
	// Ensure no API token is set
	os.Unsetenv("WHERESITUP_API_TOKEN")

	log := logger.With("test", t.Name())

	client := NewClient(log)

	require.Empty(t, client.APIToken, "APIToken should be empty when not set")
}

func TestNewWheresitupClientWithConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config ClientConfig
		check  func(t *testing.T, client *Client)
	}{
		{
			name: "Custom config",
			config: ClientConfig{
				BaseURL:    "https://custom.api.com",
				APIToken:   "custom-token",
				HTTPClient: &http.Client{Timeout: 5 * time.Second},
			},
			check: func(t *testing.T, client *Client) {
				require.Equal(t, "https://custom.api.com", client.BaseURL)
				require.Equal(t, "custom-token", client.APIToken)
			},
		},
		{
			name:   "Empty config uses defaults",
			config: ClientConfig{},
			check: func(t *testing.T, client *Client) {
				require.Equal(t, "https://api.wheresitup.com/v4", client.BaseURL, "BaseURL should be default")
				require.NotNil(t, client.HTTPClient, "HTTPClient should not be nil")
			},
		},
		{
			name: "Partial config",
			config: ClientConfig{
				APIToken: "partial-token",
			},
			check: func(t *testing.T, client *Client) {
				require.Equal(t, "https://api.wheresitup.com/v4", client.BaseURL, "BaseURL = %s, want default", client.BaseURL)
				require.Equal(t, "partial-token", client.APIToken, "APIToken = %s, want partial-token", client.APIToken)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, tt.config)
			require.NotNil(t, client, "NewWheresitupClientWithConfig() returned nil")
			tt.check(t, client)
		})
	}
}

func TestSetCommonHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		apiToken string
		check    func(t *testing.T, req *http.Request)
	}{
		{
			name:     "With API token",
			apiToken: "test-token",
			check: func(t *testing.T, req *http.Request) {
				auth := req.Header.Get("Auth")
				require.Equal(t, "Bearer test-token", auth, "Auth header = %s, want Bearer test-token", auth)
				accept := req.Header.Get("Accept")
				require.Equal(t, "application/json", accept, "Accept header = %s, want application/json", accept)
				ua := req.Header.Get("User-Agent")
				require.Equal(t, "DoubleZero-Collector/1.0", ua, "User-Agent header = %s, want DoubleZero-Collector/1.0", ua)
			},
		},
		{
			name:     "Without API token",
			apiToken: "",
			check: func(t *testing.T, req *http.Request) {
				auth := req.Header.Get("Auth")
				require.Empty(t, auth, "Auth header = %s, want empty", auth)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{APIToken: tt.apiToken}
			req, _ := http.NewRequest("GET", "http://test.com", nil)
			client.setCommonHeaders(req)
			tt.check(t, req)
		})
	}
}

func TestGetAllSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mockFunc func(req *http.Request) (*http.Response, error)
		want     []Source
		wantErr  bool
	}{
		{
			name: "Successful sources retrieval",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				require.Equal(t, "GET", req.Method, "Method = %s, want GET", req.Method)
				require.True(t, strings.HasSuffix(req.URL.Path, "/sources"), "URL path = %s, want to end with /sources", req.URL.Path)

				response := SourcesResponse{
					Sources: []Source{
						{
							ID:        "1",
							Name:      "nyc",
							Title:     "New York, NY, USA",
							Location:  "New York",
							State:     "NY",
							Latitude:  "40.7128",
							Longitude: "-74.0060",
						},
						{
							ID:        "2",
							Name:      "lon",
							Title:     "London, UK",
							Location:  "London",
							Latitude:  "51.5074",
							Longitude: "-0.1278",
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want: []Source{
				{
					ID:        "1",
					Name:      "nyc",
					Title:     "New York, NY, USA",
					Location:  "New York",
					State:     "NY",
					Latitude:  "40.7128",
					Longitude: "-74.0060",
				},
				{
					ID:        "2",
					Name:      "lon",
					Title:     "London, UK",
					Location:  "London",
					Latitude:  "51.5074",
					Longitude: "-0.1278",
				},
			},
			wantErr: false,
		},
		{
			name: "Empty sources list",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := SourcesResponse{
					Sources: []Source{},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want:    []Source{},
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

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetAllSources(t.Context())
			if tt.wantErr {
				require.Error(t, err, "GetAllSources() error = %v", err)
			} else {
				require.NoError(t, err, "GetAllSources() error = %v", err)
				if got != nil {
					require.Len(t, got, len(tt.want), "GetAllSources() length = %d, want %d", len(got), len(tt.want))
					for i, source := range got {
						if i < len(tt.want) {
							require.Equal(t, tt.want[i].ID, source.ID, "Source[%d].ID = %s, want %s", i, source.ID, tt.want[i].ID)
							require.Equal(t, tt.want[i].Name, source.Name, "Source[%d].Name = %s, want %s", i, source.Name, tt.want[i].Name)
						}
					}
				}
			}
		})
	}
}

func TestGetNearestSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat      float64
		lng      float64
		count    int
		mockFunc func(req *http.Request) (*http.Response, error)
		wantLen  int
		wantErr  bool
	}{
		{
			name:  "Find nearest sources to NYC",
			lat:   40.7128,
			lng:   -74.0060,
			count: 2,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := SourcesResponse{
					Sources: []Source{
						{
							ID:        "1",
							Name:      "nyc",
							Title:     "New York, NY, USA",
							Latitude:  "40.7128",
							Longitude: "-74.0060",
						},
						{
							ID:        "2",
							Name:      "newark",
							Title:     "Newark, NJ, USA",
							Latitude:  "40.7357",
							Longitude: "-74.1724",
						},
						{
							ID:        "3",
							Name:      "london",
							Title:     "London, UK",
							Latitude:  "51.5074",
							Longitude: "-0.1278",
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantLen: 2,
			wantErr: false,
		},
		{
			name:  "No sources available",
			lat:   40.7128,
			lng:   -74.0060,
			count: 5,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := SourcesResponse{
					Sources: []Source{},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantLen: 0,
			wantErr: false,
		},
		{
			name:  "API error",
			lat:   40.7128,
			lng:   -74.0060,
			count: 2,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("Server error")),
				}, nil
			},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetNearestSources(t.Context(), tt.lat, tt.lng, tt.count)
			if tt.wantErr {
				require.Error(t, err, "GetNearestSources() error = %v", err)
			} else {
				require.NoError(t, err, "GetNearestSources() error = %v", err)
			}
			require.Len(t, got, tt.wantLen, "GetNearestSources() length = %d, want %d", len(got), tt.wantLen)
		})
	}
}

func TestGetNearestSourcesForLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		locations []collector.LocationMatch
		mockFunc  func(req *http.Request) (*http.Response, error)
		check     func(t *testing.T, matches []LocationSourceMatch)
		wantErr   bool
	}{
		{
			name: "Successful source discovery for locations",
			locations: []collector.LocationMatch{
				{
					LocationCode: "NYC",
					Latitude:     40.7128,
					Longitude:    -74.0060,
				},
				{
					LocationCode: "LAX",
					Latitude:     34.0522,
					Longitude:    -118.2437,
				},
			},
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := SourcesResponse{
					Sources: []Source{
						{ID: "1", Name: "nyc", Latitude: "40.7128", Longitude: "-74.0060"},
						{ID: "2", Name: "lax", Latitude: "34.0522", Longitude: "-118.2437"},
						{ID: "3", Name: "newark", Latitude: "40.7357", Longitude: "-74.1724"},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			check: func(t *testing.T, matches []LocationSourceMatch) {
				require.Len(t, matches, 2, "Got %d matches, want 2", len(matches))
				// First location should have sources nearby
				require.NotEmpty(t, matches[0].NearestSources, "First location should have nearby sources")
			},
			wantErr: false,
		},
		{
			name: "Location with no coordinates",
			locations: []collector.LocationMatch{
				{
					LocationCode: "Unknown",
					Latitude:     0,
					Longitude:    0,
				},
			},
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"sources":[]}`)),
				}, nil
			},
			check: func(t *testing.T, matches []LocationSourceMatch) {
				require.Len(t, matches, 1, "Got %d matches, want 1", len(matches))
				require.Empty(t, matches[0].NearestSources, "Location with no coordinates should have no sources")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetNearestSourcesForLocations(t.Context(), tt.locations)
			if tt.wantErr {
				require.Error(t, err, "GetNearestSourcesForLocations() error = %v", err)
			} else {
				require.NoError(t, err, "GetNearestSourcesForLocations() error = %v", err)
				if got != nil {
					tt.check(t, got)
				}
			}
		})
	}
}

func TestCreateJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		url          string
		mockFunc     func(req *http.Request) (*http.Response, error)
		wantJobID    string
		wantErr      bool
		checkRequest bool
	}{
		{
			name: "Successful job creation",
			url:  "8.8.8.8",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request method and URL
				require.Equal(t, "GET", req.Method, "Method = %s, want GET", req.Method)
				require.True(t, strings.Contains(req.URL.Path, "/jobs"), "URL path = %s, want to contain /jobs", req.URL.Path)
				// Verify URL parameter
				require.True(t, strings.Contains(req.URL.RawQuery, "url=8.8.8.8"), "Query = %s, want to contain url=8.8.8.8", req.URL.RawQuery)

				response := JobResponse{
					ID:      "abc123",
					Status:  "pending",
					Created: "2023-01-01 12:00:00",
					Expires: "2023-01-01 13:00:00",
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantJobID: "abc123",
			wantErr:   false,
		},
		{
			name: "API error response",
			url:  "8.8.8.8",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader("Invalid request")),
				}, nil
			},
			wantJobID: "",
			wantErr:   true,
		},
		{
			name: "Network error",
			url:  "8.8.8.8",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
			wantJobID: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			gotID, err := client.CreateJob(t.Context(), tt.url)
			if tt.wantErr {
				require.Error(t, err, "CreateJob() error = %v", err)
			} else {
				require.NoError(t, err, "CreateJob() error = %v", err)
			}
			require.Equal(t, tt.wantJobID, gotID, "CreateJob() ID = %s, want %s", gotID, tt.wantJobID)
		})
	}
}

func TestGetJobResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jobID    string
		mockFunc func(req *http.Request) (*http.Response, error)
		wantErr  bool
		check    func(t *testing.T, result *JobResultResponse)
	}{
		{
			name:  "Successful results retrieval",
			jobID: "abc123",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				require.Equal(t, "GET", req.Method, "Method = %s, want GET", req.Method)
				require.True(t, strings.Contains(req.URL.Path, "/jobs/abc123"), "URL path = %s, want to contain /jobs/abc123", req.URL.Path)

				response := JobResultResponse{
					Request: struct {
						URL       string `json:"url"`
						IP        string `json:"ip"`
						StartTime int64  `json:"start_time"`
						EasyTime  string `json:"easy_time"`
						Expiry    struct {
							Sec  int64 `json:"sec"`
							Usec int   `json:"usec"`
						} `json:"expiry"`
					}{
						URL: "8.8.8.8",
						IP:  "8.8.8.8",
					},
					Response: struct {
						Complete   map[string]ServiceResult `json:"complete"`
						InProgress []any                    `json:"in_progress"`
						Error      []any                    `json:"error"`
					}{
						Complete: map[string]ServiceResult{
							"nyc": {
								Ping: PingResult{
									Summary: struct {
										Pings   []PingSummary  `json:"pings"`
										Summary PingStatistics `json:"summary"`
									}{
										Summary: PingStatistics{
											Avg: "25.5",
										},
									},
								},
							},
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantErr: false,
			check: func(t *testing.T, result *JobResultResponse) {
				require.NotNil(t, result, "Expected non-nil result")
				require.Equal(t, "8.8.8.8", result.Request.URL, "Request.URL = %s, want 8.8.8.8", result.Request.URL)
				require.Len(t, result.Response.Complete, 1, "Complete results count = %d, want 1", len(result.Response.Complete))
			},
		},
		{
			name:  "Job not found",
			jobID: "invalid-job",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("Job not found")),
				}, nil
			},
			wantErr: true,
		},
		{
			name:  "Invalid JSON response",
			jobID: "abc123",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("invalid json")),
				}, nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetJobResults(t.Context(), tt.jobID)
			if tt.wantErr {
				require.Error(t, err, "GetJobResults() error = %v", err)
			} else {
				require.NoError(t, err, "GetJobResults() error = %v", err)
				if tt.check != nil {
					tt.check(t, got)
				}
			}
		})
	}
}

func TestMakeRequest_ContextCancellation(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	client := NewClientWithConfig(log, ClientConfig{
		BaseURL: "https://api.wheresitup.com/v4",
		HTTPClient: &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate delay
				time.Sleep(100 * time.Millisecond)
				return nil, errors.New("request cancelled")
			},
		},
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	_, err := client.makeRequest(ctx, "/test")
	require.Error(t, err, "Expected error for cancelled context")
}

func TestAPIErrorResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"400 Bad Request", http.StatusBadRequest, "Bad request"},
		{"401 Unauthorized", http.StatusUnauthorized, "Unauthorized"},
		{"403 Forbidden", http.StatusForbidden, "Forbidden"},
		{"404 Not Found", http.StatusNotFound, "Not found"},
		{"429 Too Many Requests", http.StatusTooManyRequests, "Rate limited"},
		{"500 Internal Server Error", http.StatusInternalServerError, "Server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL: "https://api.wheresitup.com/v4",
				HTTPClient: &MockHTTPClient{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(strings.NewReader(tt.body)),
						}, nil
					},
				},
			})

			_, err := client.GetAllSources(t.Context())
			require.Error(t, err, "Expected error for non-200 status code")
			require.Contains(t, err.Error(), fmt.Sprintf("status: %d", tt.statusCode), "Error should contain status code %d", tt.statusCode)
		})
	}
}

func TestCreateJobWithRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		request      any
		debug        bool
		mockFunc     func(req *http.Request) (*http.Response, error)
		wantResponse *JobResponse
		wantErr      bool
		checkRequest func(t *testing.T, req *http.Request, body []byte)
	}{
		{
			name: "Successful job creation with debug",
			request: map[string]any{
				"uri":     "http://example.com",
				"tests":   []string{"ping", "traceroute"},
				"sources": []string{"nyc", "lax"},
				"options": map[string]any{
					"expire_after": "1 hour",
					"label":        "Test Job",
				},
			},
			debug: true,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request method
				require.Equal(t, "POST", req.Method, "Method = %s, want POST", req.Method)
				// Verify URL path
				require.True(t, strings.HasSuffix(req.URL.Path, "/jobs"), "URL path = %s, want to end with /jobs", req.URL.Path)
				// Verify content type
				ct := req.Header.Get("Content-Type")
				require.Equal(t, "application/json", ct, "Content-Type = %s, want application/json", ct)

				response := JobResponse{
					ID:      "job-new-123",
					Status:  "pending",
					Created: "2023-01-01 12:00:00",
					Expires: "2023-01-01 13:00:00",
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantResponse: &JobResponse{
				ID:      "job-new-123",
				Status:  "pending",
				Created: "2023-01-01 12:00:00",
				Expires: "2023-01-01 13:00:00",
			},
			wantErr: false,
			checkRequest: func(t *testing.T, req *http.Request, body []byte) {
				var requestData map[string]any
				err := json.Unmarshal(body, &requestData)
				require.NoError(t, err, "Failed to unmarshal request body")
				require.Equal(t, "http://example.com", requestData["uri"], "Request URI = %v, want http://example.com", requestData["uri"])
			},
		},
		{
			name: "Successful job creation without debug",
			request: map[string]any{
				"uri":   "http://test.com",
				"tests": []string{"ping"},
			},
			debug: false,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := JobResponse{
					ID:     "job-456",
					Status: "pending",
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			wantResponse: &JobResponse{
				ID:     "job-456",
				Status: "pending",
			},
			wantErr: false,
		},
		{
			name:    "Request marshaling error",
			request: make(chan int), // unmarshalable type
			debug:   false,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				t.Error("Should not reach HTTP call")
				return nil, nil
			},
			wantResponse: nil,
			wantErr:      true,
		},
		{
			name:    "HTTP error response",
			request: map[string]any{"uri": "http://test.com"},
			debug:   true,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"error": "Invalid request"}`)),
				}, nil
			},
			wantResponse: nil,
			wantErr:      true,
		},
		{
			name:    "Network error",
			request: map[string]any{"uri": "http://test.com"},
			debug:   false,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network timeout")
			},
			wantResponse: nil,
			wantErr:      true,
		},
		{
			name:    "Invalid JSON response",
			request: map[string]any{"uri": "http://test.com"},
			debug:   false,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("not json")),
				}, nil
			},
			wantResponse: nil,
			wantErr:      true,
		},
		{
			name:    "Rate limiting response",
			request: map[string]any{"uri": "http://test.com"},
			debug:   false,
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(strings.NewReader("Rate limit exceeded")),
				}, nil
			},
			wantResponse: nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			var capturedBody []byte
			client := NewClientWithConfig(log, ClientConfig{
				BaseURL:  "https://api.wheresitup.com/v4",
				APIToken: "test-token",
				HTTPClient: &MockHTTPClient{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						// Capture request body if needed
						if req.Body != nil && tt.checkRequest != nil {
							body, _ := io.ReadAll(req.Body)
							capturedBody = body
							req.Body = io.NopCloser(bytes.NewReader(body))
						}
						return tt.mockFunc(req)
					},
				},
			})

			got, err := client.CreateJobWithRequest(t.Context(), tt.request, tt.debug)
			if tt.wantErr {
				require.Error(t, err, "CreateJobWithRequest() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				require.NoError(t, err, "CreateJobWithRequest() error = %v, wantErr %v", err, tt.wantErr)
				if got != nil {
					require.Equal(t, tt.wantResponse.ID, got.ID, "Response ID = %s, want %s", got.ID, tt.wantResponse.ID)
					require.Equal(t, tt.wantResponse.Status, got.Status, "Response Status = %s, want %s", got.Status, tt.wantResponse.Status)
				}
			}

			// Check request body if validator provided
			if tt.checkRequest != nil && len(capturedBody) > 0 {
				tt.checkRequest(t, nil, capturedBody)
			}
		})
	}
}

func TestGetJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jobID    string
		mockFunc func(req *http.Request) (*http.Response, error)
		want     *JobResult
		wantErr  bool
	}{
		{
			name:  "Successful job retrieval",
			jobID: "job-123",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				require.Equal(t, "GET", req.Method, "Method = %s, want GET", req.Method)
				require.True(t, strings.Contains(req.URL.Path, "/jobs/job-123"), "URL path = %s, want to contain /jobs/job-123", req.URL.Path)

				response := JobResult{
					ID:      "job-123",
					Status:  "completed",
					Created: "2023-01-01 12:00:00",
					Expires: "2023-01-01 13:00:00",
					Results: map[string]any{
						"nyc": map[string]any{
							"ping": map[string]any{
								"status": "success",
							},
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want: &JobResult{
				ID:      "job-123",
				Status:  "completed",
				Created: "2023-01-01 12:00:00",
				Expires: "2023-01-01 13:00:00",
				Results: map[string]any{
					"nyc": map[string]any{
						"ping": map[string]any{
							"status": "success",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:  "Job not found",
			jobID: "non-existent",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("Job not found")),
				}, nil
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:  "API error",
			jobID: "job-error",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("Internal server error")),
				}, nil
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:  "Network error",
			jobID: "job-123",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:  "Invalid JSON response",
			jobID: "job-123",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("invalid{json")),
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

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL:  "https://api.wheresitup.com/v4",
				APIToken: "test-token",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetJob(t.Context(), tt.jobID)
			if tt.wantErr {
				require.Error(t, err, "GetJob() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				require.NoError(t, err, "GetJob() error = %v, wantErr %v", err, tt.wantErr)
				if got != nil {
					require.Equal(t, tt.want.ID, got.ID, "GetJob() ID = %s, want %s", got.ID, tt.want.ID)
					require.Equal(t, tt.want.Status, got.Status, "GetJob() Status = %s, want %s", got.Status, tt.want.Status)
				}
			}
		})
	}
}

func TestGetAllJobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mockFunc func(req *http.Request) (*http.Response, error)
		want     []JobDetails
		wantErr  bool
	}{
		{
			name: "Multiple jobs returned",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				require.Equal(t, "GET", req.Method, "Method = %s, want GET", req.Method)
				require.True(t, strings.HasSuffix(req.URL.Path, "/jobs"), "URL path = %s, want to end with /jobs", req.URL.Path)

				// API returns a map with job IDs as keys
				response := map[string]JobDetails{
					"job-1": {
						URL:       "http://nyc.wonderproxy.com",
						IP:        "1.1.1.1",
						StartTime: 1609459200,
						EasyTime:  "2021-01-01 12:00:00",
						Expiry: struct {
							Sec  int64 `json:"sec"`
							Usec int   `json:"usec"`
						}{
							Sec: 1609462800,
						},
						Services: []struct {
							City   string   `json:"city"`
							Server string   `json:"server"`
							Checks []string `json:"checks"`
						}{
							{
								City:   "Los Angeles",
								Server: "lax.wonderproxy.com",
								Checks: []string{"ping"},
							},
						},
					},
					"job-2": {
						URL:       "http://london.wonderproxy.com",
						IP:        "2.2.2.2",
						StartTime: 1609459260,
						EasyTime:  "2021-01-01 12:01:00",
						Expiry: struct {
							Sec  int64 `json:"sec"`
							Usec int   `json:"usec"`
						}{
							Sec: 1609462860,
						},
						Services: []struct {
							City   string   `json:"city"`
							Server string   `json:"server"`
							Checks []string `json:"checks"`
						}{
							{
								City:   "Paris",
								Server: "paris.wonderproxy.com",
								Checks: []string{"ping", "traceroute"},
							},
						},
					},
				}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want: []JobDetails{
				{
					ID:        "job-1",
					URL:       "http://nyc.wonderproxy.com",
					IP:        "1.1.1.1",
					StartTime: 1609459200,
					EasyTime:  "2021-01-01 12:00:00",
				},
				{
					ID:        "job-2",
					URL:       "http://london.wonderproxy.com",
					IP:        "2.2.2.2",
					StartTime: 1609459260,
					EasyTime:  "2021-01-01 12:01:00",
				},
			},
			wantErr: false,
		},
		{
			name: "Empty jobs list",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				response := map[string]JobDetails{}
				body, _ := json.Marshal(response)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			},
			want:    []JobDetails{},
			wantErr: false,
		},
		{
			name: "API error response",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader("Unauthorized")),
				}, nil
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Network error",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection timeout")
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Invalid JSON response",
			mockFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("not a json object")),
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

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL:  "https://api.wheresitup.com/v4",
				APIToken: "test-token",
				HTTPClient: &MockHTTPClient{
					DoFunc: tt.mockFunc,
				},
			})

			got, err := client.GetAllJobs(t.Context())
			if tt.wantErr {
				require.Error(t, err, "GetAllJobs() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				require.NoError(t, err, "GetAllJobs() error = %v, wantErr %v", err, tt.wantErr)
				if got != nil {
					// Since map ordering is non-deterministic, we need to compare differently
					require.Len(t, got, len(tt.want), "GetAllJobs() returned %d jobs, want %d", len(got), len(tt.want))

					// Create a map to verify all expected jobs are present
					gotMap := make(map[string]JobDetails)
					for _, job := range got {
						gotMap[job.ID] = job
					}

					for _, wantJob := range tt.want {
						gotJob, exists := gotMap[wantJob.ID]
						require.True(t, exists, "Expected job %s not found in results", wantJob.ID)
						if exists {
							require.Equal(t, wantJob.URL, gotJob.URL, "Job %s URL = %s, want %s", wantJob.ID, gotJob.URL, wantJob.URL)
							require.Equal(t, wantJob.IP, gotJob.IP, "Job %s IP = %s, want %s", wantJob.ID, gotJob.IP, wantJob.IP)
						}
					}
				}
			}
		})
	}
}

func TestGetCredit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mockResponse string
		mockStatus   int
		mockError    error
		wantCredit   int
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:         "Successful credit retrieval",
			mockResponse: `{"current": 50000, "used": {"today": 100, "yesterday": 50, "week": 300}}`,
			mockStatus:   http.StatusOK,
			wantCredit:   50000,
			wantErr:      false,
		},
		{
			name:         "Zero credits",
			mockResponse: `{"current": 0, "used": {"today": 0, "yesterday": 0, "week": 0}}`,
			mockStatus:   http.StatusOK,
			wantCredit:   0,
			wantErr:      false,
		},
		{
			name:         "Large credit amount",
			mockResponse: `{"current": 1000000, "used": {"today": 1000, "yesterday": 500, "week": 3000}}`,
			mockStatus:   http.StatusOK,
			wantCredit:   1000000,
			wantErr:      false,
		},
		{
			name:         "API error response",
			mockResponse: `{"error": "Invalid API key"}`,
			mockStatus:   http.StatusUnauthorized,
			wantErr:      true,
			wantErrMsg:   "API request failed with status: 401",
		},
		{
			name:         "Invalid JSON response",
			mockResponse: `invalid json`,
			mockStatus:   http.StatusOK,
			wantErr:      true,
			wantErrMsg:   "failed to decode credit response",
		},
		{
			name:         "Missing current field",
			mockResponse: `{"balance": 5000, "used": {"today": 100}}`,
			mockStatus:   http.StatusOK,
			wantCredit:   0, // Should default to 0
			wantErr:      false,
		},
		{
			name:       "Network error",
			mockError:  errors.New("connection timeout"),
			wantErr:    true,
			wantErrMsg: "connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := logger.With("test", t.Name())

			client := NewClientWithConfig(log, ClientConfig{
				BaseURL:  "https://api.wheresitup.com/v4",
				APIToken: "test-token",
				HTTPClient: &MockHTTPClient{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						if tt.mockError != nil {
							return nil, tt.mockError
						}

						// Verify the request
						require.Equal(t, "/v4/credits", req.URL.Path, "Request path = %s, want /v4/credits", req.URL.Path)

						// Verify authorization header
						authHeader := req.Header.Get("Auth")
						require.Equal(t, "Bearer test-token", authHeader, "Auth header = %s, want Bearer test-token", authHeader)

						return &http.Response{
							StatusCode: tt.mockStatus,
							Body:       io.NopCloser(strings.NewReader(tt.mockResponse)),
						}, nil
					},
				},
			})

			credit, err := client.GetCredit(t.Context())

			if tt.wantErr {
				require.Error(t, err, "Expected error but got none")
				if tt.wantErrMsg != "" {
					require.Contains(t, err.Error(), tt.wantErrMsg, "Error = %v, want to contain %s", err, tt.wantErrMsg)
				}
			} else {
				require.NoError(t, err, "GetCredit() error = %v", err)
				require.Equal(t, tt.wantCredit, credit, "GetCredit() = %d, want %d", credit, tt.wantCredit)
			}
		})
	}
}
