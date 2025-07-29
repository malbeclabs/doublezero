package wheresitup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

// mockLocationsFetcher returns a mock location fetcher for testing
func mockLocationsFetcher(locations []collector.LocationMatch) locationFetcher {
	return func(ctx context.Context, log *slog.Logger) []collector.LocationMatch {
		return locations
	}
}

// MockWheresitupClient implements wheresitupClientInterface for testing
type MockWheresitupClient struct {
	GetAllSourcesFunc                 func(ctx context.Context) ([]Source, error)
	GetNearestSourcesFunc             func(ctx context.Context, latitude, longitude float64, count int) ([]Source, error)
	GetNearestSourcesForLocationsFunc func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error)
	CreateJobFunc                     func(ctx context.Context, url string) (string, error)
	CreateJobWithRequestFunc          func(ctx context.Context, request any, debug bool) (*JobResponse, error)
	GetJobResultsFunc                 func(ctx context.Context, jobID string) (*JobResultResponse, error)
	GetAllJobsFunc                    func(ctx context.Context) ([]JobDetails, error)
	GetCreditFunc                     func(ctx context.Context) (int, error)
}

func (m *MockWheresitupClient) GetAllSources(ctx context.Context) ([]Source, error) {
	if m.GetAllSourcesFunc != nil {
		return m.GetAllSourcesFunc(ctx)
	}
	return []Source{}, nil
}

func (m *MockWheresitupClient) GetNearestSources(ctx context.Context, latitude, longitude float64, count int) ([]Source, error) {
	if m.GetNearestSourcesFunc != nil {
		return m.GetNearestSourcesFunc(ctx, latitude, longitude, count)
	}
	return []Source{}, nil
}

func (m *MockWheresitupClient) GetNearestSourcesForLocations(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
	if m.GetNearestSourcesForLocationsFunc != nil {
		return m.GetNearestSourcesForLocationsFunc(ctx, locations)
	}
	return []LocationSourceMatch{}, nil
}

func (m *MockWheresitupClient) CreateJob(ctx context.Context, url string) (string, error) {
	if m.CreateJobFunc != nil {
		return m.CreateJobFunc(ctx, url)
	}
	return "test-job-id", nil
}

func (m *MockWheresitupClient) CreateJobWithRequest(ctx context.Context, request any, debug bool) (*JobResponse, error) {
	if m.CreateJobWithRequestFunc != nil {
		return m.CreateJobWithRequestFunc(ctx, request, debug)
	}
	return &JobResponse{ID: "test-job-id", Status: "pending"}, nil
}

func (m *MockWheresitupClient) GetJobResults(ctx context.Context, jobID string) (*JobResultResponse, error) {
	if m.GetJobResultsFunc != nil {
		return m.GetJobResultsFunc(ctx, jobID)
	}
	return &JobResultResponse{}, nil
}

func (m *MockWheresitupClient) GetAllJobs(ctx context.Context) ([]JobDetails, error) {
	if m.GetAllJobsFunc != nil {
		return m.GetAllJobsFunc(ctx)
	}
	return []JobDetails{}, nil
}

func (m *MockWheresitupClient) GetCredit(ctx context.Context) (int, error) {
	if m.GetCreditFunc != nil {
		return m.GetCreditFunc(ctx)
	}
	return 10000, nil
}

func TestInternetLatency_Wheresitup_ListSources_Success(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-NYC",
							Latitude:     40.7128,
							Longitude:    -74.0060,
						},
						NearestSources: []Source{
							{
								ID:        "nyc-1",
								Name:      "new_york",
								Title:     "New York",
								Location:  "New York, NY",
								Latitude:  "40.7128",
								Longitude: "-74.0060",
							},
						},
						SourceCount: 1,
					},
				}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{
			LocationCode: "US-NYC",
			Latitude:     40.7128,
			Longitude:    -74.0060,
		},
	}

	err := c.PrintSources(t.Context(), locations)
	require.NoError(t, err, "ListSources() error = %v, want nil", err)
}

func TestInternetLatency_Wheresitup_ListSources_NoDevices(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{},
		log:    log,
	}

	err := c.PrintSources(t.Context(), []collector.LocationMatch{})
	require.ErrorIs(t, err, collector.ErrNoDevicesFound, "ListSources() error = %v, want %v", err, collector.ErrNoDevicesFound)
}

func TestInternetLatency_Wheresitup_ListSources_APIError(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return nil, errors.New("API error")
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "US-NYC"},
	}

	err := c.PrintSources(t.Context(), locations)
	require.Error(t, err, "ListSources() expected error, got nil")
}

func TestInternetLatency_Wheresitup_RunJobCreation_Success(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	jobIDsFile := filepath.Join(tempDir, "jobs.json")

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 15000, nil
			},
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-NYC",
							Latitude:     40.7128,
							Longitude:    -74.0060,
						},
						NearestSources: []Source{
							{Name: "new_york", Title: "New York"},
						},
						SourceCount: 1,
					},
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-LAX",
							Latitude:     34.0522,
							Longitude:    -118.2437,
						},
						NearestSources: []Source{
							{Name: "los_angeles", Title: "Los Angeles"},
						},
						SourceCount: 1,
					},
				}, nil
			},
			CreateJobWithRequestFunc: func(ctx context.Context, request any, debug bool) (*JobResponse, error) {
				return &JobResponse{
					ID:      "job-123",
					Status:  "pending",
					Created: time.Now().UTC().Format(time.RFC3339),
				}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "US-NYC", Latitude: 40.7128, Longitude: -74.0060},
		{LocationCode: "US-LAX", Latitude: 34.0522, Longitude: -118.2437},
	}

	err := c.RunJobCreation(t.Context(), locations, false, jobIDsFile)
	require.NoError(t, err, "RunJobCreation() error = %v, want nil", err)

	// Verify job was saved
	state := NewState(jobIDsFile)
	err = state.Load()
	require.NoError(t, err, "state.Load() error = %v", err)
	jobIDs := state.GetJobIDs()
	require.Len(t, jobIDs, 1, "Expected 1 job ID to be saved, got %v", jobIDs)
	require.Equal(t, "job-123", jobIDs[0], "Expected job ID to be job-123, got %v", jobIDs[0])
}

func TestInternetLatency_Wheresitup_RunJobCreation_LowCredit(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	jobIDsFile := filepath.Join(tempDir, "jobs.json")

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 500, nil // Low credit
			},
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{LocationMatch: collector.LocationMatch{LocationCode: "US-NYC"}, SourceCount: 1},
					{LocationMatch: collector.LocationMatch{LocationCode: "US-LAX"}, SourceCount: 1},
				}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "US-NYC"},
		{LocationCode: "US-LAX"},
	}

	// Should still succeed but log warning
	err := c.RunJobCreation(t.Context(), locations, false, jobIDsFile)
	require.NoError(t, err, "RunJobCreation() error = %v, want nil", err)
}

func TestInternetLatency_Wheresitup_RunJobCreation_InsufficientSources(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 15000, nil
			},
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{LocationMatch: collector.LocationMatch{LocationCode: "US-NYC"}, SourceCount: 1},
					// Only one location with sources
				}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "US-NYC"},
	}

	err := c.RunJobCreation(t.Context(), locations, false, "")
	require.Error(t, err, "RunJobCreation() expected error for insufficient sources")
}

func TestInternetLatency_Wheresitup_CreateJobsBetweenLocations_Success(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	callCount := 0
	c := &Collector{
		client: &MockWheresitupClient{
			CreateJobWithRequestFunc: func(ctx context.Context, request any, debug bool) (*JobResponse, error) {
				callCount++
				return &JobResponse{
					ID:      fmt.Sprintf("job-%d", callCount),
					Status:  "pending",
					Created: time.Now().UTC().Format(time.RFC3339),
				}, nil
			},
		},
		log: log,
	}

	locations := []LocationSourceMatch{
		{
			LocationMatch:  collector.LocationMatch{LocationCode: "US-LAX"},
			NearestSources: []Source{{Name: "los_angeles"}},
		},
		{
			LocationMatch:  collector.LocationMatch{LocationCode: "US-NYC"},
			NearestSources: []Source{{Name: "new_york"}},
		},
		{
			LocationMatch:  collector.LocationMatch{LocationCode: "US-SFO"},
			NearestSources: []Source{{Name: "san_francisco"}},
		},
	}

	jobs, err := c.CreateJobsBetweenLocations(t.Context(), locations, false, false)
	require.NoError(t, err, "CreateJobsBetweenLocations() error = %v", err)

	// Should create 3 jobs: LAX->NYC, LAX->SFO, NYC->SFO (alphabetical ordering)
	require.Len(t, jobs, 3, "Expected 3 jobs, got %d", len(jobs))
}

func TestInternetLatency_Wheresitup_CreateJobsBetweenLocations_DryRun(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			CreateJobWithRequestFunc: func(ctx context.Context, request any, debug bool) (*JobResponse, error) {
				require.Fail(t, "CreateJobWithRequest should not be called in dry run mode")
				return nil, nil
			},
		},
		log: log,
	}

	locations := []LocationSourceMatch{
		{
			LocationMatch:  collector.LocationMatch{LocationCode: "US-NYC"},
			NearestSources: []Source{{Name: "new_york"}},
		},
		{
			LocationMatch:  collector.LocationMatch{LocationCode: "US-LAX"},
			NearestSources: []Source{{Name: "los_angeles"}},
		},
	}

	jobs, err := c.CreateJobsBetweenLocations(t.Context(), locations, true, false)
	require.NoError(t, err, "CreateJobsBetweenLocations() error = %v", err)

	require.Empty(t, jobs, "Expected 0 jobs in dry run, got %d", len(jobs))
}

func TestInternetLatency_Wheresitup_ExportJobResults_Success(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	jobIDsFile := filepath.Join(tempDir, "jobs.json")
	outputDir := filepath.Join(tempDir, "output")

	// Create test locations
	testLocations := []collector.LocationMatch{
		{LocationCode: "US-NYC", Latitude: 40.7128, Longitude: -74.0060},
		{LocationCode: "US-LAX", Latitude: 34.0522, Longitude: -118.2437},
	}

	e, err := exporter.NewCSVExporter(log, "wheresitup_measurements", outputDir)
	require.NoError(t, err)

	c := &Collector{
		exporter: e,
		client: &MockWheresitupClient{
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-NYC",
						},
						NearestSources: []Source{
							{Name: "new_york"},
						},
					},
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-LAX",
						},
						NearestSources: []Source{
							{Name: "los_angeles"},
						},
					},
				}, nil
			},
			GetJobResultsFunc: func(ctx context.Context, jobID string) (*JobResultResponse, error) {
				return &JobResultResponse{
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
						URL:       "http://los_angeles.wonderproxy.com",
						StartTime: time.Now().UTC().Unix(),
						Expiry: struct {
							Sec  int64 `json:"sec"`
							Usec int   `json:"usec"`
						}{},
					},
					Response: struct {
						Complete   map[string]ServiceResult `json:"complete"`
						InProgress []any                    `json:"in_progress"`
						Error      []any                    `json:"error"`
					}{
						Complete: map[string]ServiceResult{
							"new_york": {
								Ping: PingResult{
									Summary: struct {
										Pings   []PingSummary  `json:"pings"`
										Summary PingStatistics `json:"summary"`
									}{
										Summary: PingStatistics{
											Min: "10.5",
											Avg: "12.3",
											Max: "15.1",
										},
									},
								},
							},
						},
					},
				}, nil
			},
		},
		log:              log,
		locationsFetcher: mockLocationsFetcher(testLocations),
	}

	// Save a job ID to process
	state := NewState(jobIDsFile)
	err = state.AddJobIDs([]string{"job-123"})
	require.NoError(t, err, "state.AddJobIDs() error = %v", err)

	err = c.ExportJobResults(t.Context(), jobIDsFile, outputDir)
	require.NoError(t, err, "ExportJobResults() error = %v", err)

	// Verify job was removed after successful export
	state2 := NewState(jobIDsFile)
	err = state2.Load()
	require.NoError(t, err, "state.Load() error = %v", err)
	remainingJobs := state2.GetJobIDs()
	require.Empty(t, remainingJobs, "Expected job to be removed after export, got %v", remainingJobs)
}

func TestInternetLatency_Wheresitup_ExportJobResults_NoJobs(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	jobIDsFile := filepath.Join(tempDir, "jobs.json")
	outputDir := filepath.Join(tempDir, "output")

	c := &Collector{
		client: &MockWheresitupClient{},
		log:    log,
	}

	err := c.ExportJobResults(t.Context(), jobIDsFile, outputDir)
	require.NoError(t, err, "ExportJobResults() error = %v for empty job list", err)
}

func TestInternetLatency_Wheresitup_ParseLocationCodesFromJobDetails(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil)

	tests := []struct {
		name  string
		job   JobDetails
		wantA string
		wantZ string
	}{
		{
			name: "Normal case",
			job: JobDetails{
				URL: "http://los_angeles.wonderproxy.com",
				Services: []struct {
					City   string   `json:"city"`
					Server string   `json:"server"`
					Checks []string `json:"checks"`
				}{
					{City: "new_york"},
				},
			},
			wantA: "los_angeles",
			wantZ: "new_york",
		},
		{
			name: "Missing URL",
			job: JobDetails{
				Services: []struct {
					City   string   `json:"city"`
					Server string   `json:"server"`
					Checks []string `json:"checks"`
				}{
					{City: "new_york"},
				},
			},
			wantA: "Unknown",
			wantZ: "new_york",
		},
		{
			name: "Missing services",
			job: JobDetails{
				URL: "http://los_angeles.wonderproxy.com",
			},
			wantA: "los_angeles",
			wantZ: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotA, gotZ := c.parseLocationCodesFromJobDetails(tt.job)
			require.Equal(t, tt.wantA, gotA, "parseLocationCodesFromJobDetails() A = %s, want %s", gotA, tt.wantA)
			require.Equal(t, tt.wantZ, gotZ, "parseLocationCodesFromJobDetails() Z = %s, want %s", gotZ, tt.wantZ)
		})
	}
}

func TestInternetLatency_Wheresitup_ExtractChecksFromJobDetails(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil)

	tests := []struct {
		name string
		job  JobDetails
		want string
	}{
		{
			name: "Multiple checks",
			job: JobDetails{
				Services: []struct {
					City   string   `json:"city"`
					Server string   `json:"server"`
					Checks []string `json:"checks"`
				}{
					{Checks: []string{"ping", "http"}},
					{Checks: []string{"ping", "dns"}},
				},
			},
			want: "ping,http,dns",
		},
		{
			name: "No checks",
			job:  JobDetails{},
			want: "None",
		},
		{
			name: "Duplicate checks",
			job: JobDetails{
				Services: []struct {
					City   string   `json:"city"`
					Server string   `json:"server"`
					Checks []string `json:"checks"`
				}{
					{Checks: []string{"ping", "ping"}},
				},
			},
			want: "ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.extractChecksFromJobDetails(tt.job)
			require.Equal(t, tt.want, got, "extractChecksFromJobDetails() = %s, want %s", got, tt.want)
		})
	}
}

func TestInternetLatency_Wheresitup_FormatTimestampFromUnix(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil)

	// Test with a known timestamp
	unixTime := int64(1640995200) // 2022-01-01 00:00:00 UTC
	result := c.formatTimestampFromUnix(unixTime)

	// Should contain the date
	require.NotEmpty(t, result, "formatTimestampFromUnix() returned empty string")

	// Test with zero timestamp
	result = c.formatTimestampFromUnix(0)
	require.NotEmpty(t, result, "formatTimestampFromUnix() returned empty string for zero timestamp")
}

func TestInternetLatency_Wheresitup_BuildLocationMapping(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
				return []LocationSourceMatch{
					{
						LocationMatch: collector.LocationMatch{
							LocationCode: "US-NYC",
						},
						NearestSources: []Source{
							{Name: "new_york"},
							{Name: "newark"},
						},
					},
				}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "US-NYC"},
	}

	mapping, err := c.buildLocationMapping(t.Context(), locations)
	require.NoError(t, err, "buildLocationMapping() error = %v", err)

	// Check mapping
	info, exists := mapping["new_york"]
	require.True(t, exists, "Expected new_york in mapping")
	require.Equal(t, "US-NYC", info.LocationCode, "Expected LocationCode US-NYC, got %s", info.LocationCode)

	_, exists = mapping["newark"]
	require.True(t, exists, "Expected newark in mapping")
}

func TestInternetLatency_Wheresitup_ListJobs_Success(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetAllJobsFunc: func(ctx context.Context) ([]JobDetails, error) {
				return []JobDetails{
					{
						ID:        "job-1",
						URL:       "http://los_angeles.wonderproxy.com",
						StartTime: time.Now().UTC().Unix(),
						Expiry: struct {
							Sec  int64 `json:"sec"`
							Usec int   `json:"usec"`
						}{Sec: time.Now().UTC().Add(time.Hour).Unix()},
						Services: []struct {
							City   string   `json:"city"`
							Server string   `json:"server"`
							Checks []string `json:"checks"`
						}{
							{City: "new_york", Checks: []string{"ping"}},
						},
					},
				}, nil
			},
		},
		log: log,
	}

	// This function prints to stdout, so we just verify it doesn't error
	err := c.ListJobs(t.Context())
	require.NoError(t, err, "ListJobs() error = %v", err)
}

func TestInternetLatency_Wheresitup_ListJobs_Empty(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetAllJobsFunc: func(ctx context.Context) ([]JobDetails, error) {
				return []JobDetails{}, nil
			},
		},
		log: log,
	}

	err := c.ListJobs(t.Context())
	require.NoError(t, err, "ListJobs() error = %v for empty list", err)
}

func TestInternetLatency_Wheresitup_ListJobs_Error(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := &Collector{
		client: &MockWheresitupClient{
			GetAllJobsFunc: func(ctx context.Context) ([]JobDetails, error) {
				return nil, errors.New("API error")
			},
		},
		log: log,
	}

	err := c.ListJobs(t.Context())
	require.Error(t, err, "ListJobs() expected error")
}

func TestInternetLatency_Wheresitup_ParseLocationFromUrl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url  string
		want string
	}{
		{"http://new_york.wonderproxy.com", "new_york"},
		{"https://los_angeles.wonderproxy.com", "los_angeles"},
		{"http://san_francisco.wonderproxy.com/path", "san_francisco"},
		{"invalid-url", "Unknown"},
		{"http://example.com", "Unknown"},
		{"", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()

			got := parseLocationFromUrl(tt.url)
			require.Equal(t, tt.want, got, "parseLocationFromUrl(%s) = %s, want %s", tt.url, got, tt.want)
		})
	}
}

func TestInternetLatency_Wheresitup_ExportJobResults_ErrorScenarios(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	jobIDsFile := filepath.Join(tempDir, "jobs.json")
	outputDir := filepath.Join(tempDir, "output")

	t.Run("BuildLocationMapping Error", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		c := &Collector{
			client: &MockWheresitupClient{
				GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
					return nil, errors.New("mapping error")
				},
			},
			log: log,
		}

		err := c.ExportJobResults(t.Context(), jobIDsFile, outputDir)
		require.Error(t, err, "Expected error from buildLocationMapping failure")
	})

	t.Run("JobResults In Progress", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		c := &Collector{
			client: &MockWheresitupClient{
				GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
					return []LocationSourceMatch{}, nil
				},
				GetJobResultsFunc: func(ctx context.Context, jobID string) (*JobResultResponse, error) {
					return &JobResultResponse{
						Response: struct {
							Complete   map[string]ServiceResult `json:"complete"`
							InProgress []any                    `json:"in_progress"`
							Error      []any                    `json:"error"`
						}{
							Complete:   map[string]ServiceResult{},
							InProgress: []any{"something"},
						},
					}, nil
				},
			},
			log: log,
		}

		// Save a job ID
		state := NewState(jobIDsFile)
		_ = state.AddJobIDs([]string{"job-in-progress"})

		err := c.ExportJobResults(t.Context(), jobIDsFile, outputDir)
		require.NoError(t, err, "ExportJobResults() error = %v", err)

		// Job should still be in the file
		state2 := NewState(jobIDsFile)
		_ = state2.Load()
		remainingJobs := state2.GetJobIDs()
		require.Len(t, remainingJobs, 1, "Expected job to remain for in-progress status, got %v", remainingJobs)
	})
}

func TestInternetLatency_Wheresitup_Run_TickerExecution(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Test that the Run function with ticker executes the expected sequence
	var jobCreationCalled, exportCalled bool
	var createdJobIDs []string
	var exportedJobID string
	var mu sync.Mutex

	mockClient := &MockWheresitupClient{
		GetCreditFunc: func(ctx context.Context) (int, error) {
			return 15000, nil
		},
		GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
			mu.Lock()
			jobCreationCalled = true
			mu.Unlock()
			// Return at least 2 locations for successful job creation
			return []LocationSourceMatch{
				{
					LocationMatch:  collector.LocationMatch{LocationCode: "NYC"},
					NearestSources: []Source{{ID: "src1", Name: "new_york"}},
					SourceCount:    1,
				},
				{
					LocationMatch:  collector.LocationMatch{LocationCode: "LON"},
					NearestSources: []Source{{ID: "src2", Name: "london"}},
					SourceCount:    1,
				},
			}, nil
		},
		CreateJobWithRequestFunc: func(ctx context.Context, request any, debug bool) (*JobResponse, error) {
			jobID := fmt.Sprintf("test-job-%d", time.Now().UTC().UnixNano())
			mu.Lock()
			createdJobIDs = append(createdJobIDs, jobID)
			mu.Unlock()
			return &JobResponse{ID: jobID}, nil
		},
		GetJobResultsFunc: func(ctx context.Context, jobID string) (*JobResultResponse, error) {
			mu.Lock()
			exportCalled = true
			exportedJobID = jobID
			mu.Unlock()
			return &JobResultResponse{
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
					URL:       "http://london.wonderproxy.com",
					StartTime: time.Now().UTC().Unix(),
				},
				Response: struct {
					Complete   map[string]ServiceResult `json:"complete"`
					InProgress []any                    `json:"in_progress"`
					Error      []any                    `json:"error"`
				}{
					Complete: map[string]ServiceResult{
						"new_york": {
							Ping: PingResult{
								Summary: struct {
									Pings   []PingSummary  `json:"pings"`
									Summary PingStatistics `json:"summary"`
								}{
									Summary: PingStatistics{
										Min: "25.5",
									},
								},
							},
						},
					},
					InProgress: []any{},
				},
			}, nil
		},
	}

	outputDir := t.TempDir()

	e, err := exporter.NewCSVExporter(log, "wheresitup_results", outputDir)
	require.NoError(t, err)

	c := &Collector{client: mockClient, log: log, exporter: e}
	// Set a very short wait timeout for testing
	c.SetJobWaitTimeout(1 * time.Millisecond)

	// Mock the location fetcher to avoid blockchain calls
	mockLocationsFetcher := func(ctx context.Context, log *slog.Logger) []collector.LocationMatch {
		return []collector.LocationMatch{
			{LocationCode: "NYC", Latitude: 40.7128, Longitude: -74.0060},
			{LocationCode: "LON", Latitude: 51.5074, Longitude: -0.1278},
		}
	}
	c.SetLocationsFetcher(mockLocationsFetcher)

	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0755))

	// Now that interval validation is removed, we can use a short interval
	interval := 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx, interval, false, "jobs.json", stateDir, outputDir)
	}()

	// Wait for completion
	<-done

	// Verify the operations were called
	mu.Lock()
	defer mu.Unlock()

	require.True(t, jobCreationCalled, "Job creation should have been called")
	require.True(t, exportCalled, "Export should have been called")

	// Verify that jobs were created
	require.Greater(t, len(createdJobIDs), 0, "At least one job should have been created")

	// Verify that the created job was exported
	require.NotEmpty(t, exportedJobID, "A job ID should have been exported")
	require.Contains(t, createdJobIDs, exportedJobID, "The exported job should be one of the created jobs")

	// Verify CSV file was created
	csvFiles, err := filepath.Glob(filepath.Join(outputDir, "wheresitup_results_*.csv"))
	require.NoError(t, err, "Failed to glob CSV files")
	require.Len(t, csvFiles, 1, "Expected exactly one CSV file to be created")

	// Read and verify CSV content
	csvContent, err := os.ReadFile(csvFiles[0])
	require.NoError(t, err, "Failed to read CSV file")
	csvStr := string(csvContent)

	// Check CSV header
	require.Contains(t, csvStr, "source_location_code,target_location_code,timestamp,latency",
		"CSV should contain the expected header")

	// Check that data row exists with the expected values
	require.Contains(t, csvStr, "NYC", "CSV should contain NYC location")
	require.Contains(t, csvStr, "LON", "CSV should contain LON location")
	require.Contains(t, csvStr, "25.5ms", "CSV should contain the latency value")

	// Verify job was removed from state after successful export
	stateFile := filepath.Join(stateDir, "jobs.json")
	state := NewState(stateFile)
	err = state.Load()
	require.NoError(t, err, "Failed to load state")
	remainingJobs := state.GetJobIDs()
	require.NotContains(t, remainingJobs, exportedJobID, "Exported job should be removed from state")
}
