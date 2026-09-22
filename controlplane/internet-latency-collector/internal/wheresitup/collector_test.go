package wheresitup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

// mockLocationsFetcher returns a mock location fetcher for testing
func mockLocationsFetcher(locations []collector.LocationMatch) func(ctx context.Context) []collector.LocationMatch {
	return func(ctx context.Context) []collector.LocationMatch {
		return locations
	}
}

// MockExporter implements exporter.Exporter for testing
type MockExporter struct {
	WriteRecordsFunc func(ctx context.Context, records []exporter.Record) error
}

func (m *MockExporter) WriteRecords(ctx context.Context, records []exporter.Record) error {
	if m.WriteRecordsFunc != nil {
		return m.WriteRecordsFunc(ctx, records)
	}
	return nil
}

func (m *MockExporter) Close() error {
	return nil
}

// testLogHandler captures whole records so tests can assert on a log line's message, level,
// position and attributes. It honours WithAttrs and WithGroup rather than discarding them: a
// handler that drops them passes assertions a real one would emit nested, or not at all.
type testLogHandler struct {
	mu       *sync.Mutex
	recorded *[]slog.Record
	attrs    []slog.Attr
	groups   []string
}

func newTestLogHandler() *testLogHandler {
	return &testLogHandler{mu: &sync.Mutex{}, recorded: &[]slog.Record{}}
}

func (h *testLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

// nest wraps attrs in whichever groups are open, as a real handler would.
func (h *testLogHandler) nest(attrs []slog.Attr) []slog.Attr {
	for i := len(h.groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{{Key: h.groups[i], Value: slog.GroupValue(attrs...)}}
	}
	return attrs
}

func (h *testLogHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	record := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	record.AddAttrs(h.attrs...)
	record.AddAttrs(h.nest(attrs)...)

	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recorded = append(*h.recorded, record)
	return nil
}

func (h *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), h.nest(attrs)...)
	return &clone
}

func (h *testLogHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.groups = append(append([]string{}, h.groups...), name)
	return &clone
}

func (h *testLogHandler) all() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record{}, *h.recorded...)
}

func (h *testLogHandler) messages() []string {
	var messages []string
	for _, r := range h.all() {
		messages = append(messages, r.Message)
	}
	return messages
}

// only returns the single record with the given message, and its position in the stream. It
// fails on a duplicate as well as on an absent line, so no test is satisfied by a repeated
// log, and the position lets a test pin the order of two lines.
func (h *testLogHandler) only(t *testing.T, message string) (slog.Record, int) {
	t.Helper()

	var found slog.Record
	index := -1
	for i, r := range h.all() {
		if r.Message != message {
			continue
		}
		require.Equal(t, -1, index, "log message %q was logged more than once: %v", message, h.messages())
		found, index = r, i
	}
	require.NotEqual(t, -1, index, "expected a log line %q, got %v", message, h.messages())

	return found, index
}

// attr reads one top-level attribute off a record.
func attr(t *testing.T, r slog.Record, key string) slog.Value {
	t.Helper()

	var value slog.Value
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value, found = a.Value, true
			return false
		}
		return true
	})
	require.True(t, found, "expected attribute %q on log line %q", key, r.Message)

	return value
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
		getLocationsFunc: mockLocationsFetcher(testLocations),
	}

	// Save a job ID to process
	state := NewState(jobIDsFile)
	err = state.AddJobIDs([]string{"job-123"})
	require.NoError(t, err, "state.AddJobIDs() error = %v", err)

	err = c.ExportJobResults(t.Context(), jobIDsFile)
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

	c := &Collector{
		client:           &MockWheresitupClient{},
		log:              log,
		getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{}),
	}

	err := c.ExportJobResults(t.Context(), jobIDsFile)
	require.NoError(t, err, "ExportJobResults() error = %v for empty job list", err)
}

func TestInternetLatency_Wheresitup_ParseLocationCodesFromJobDetails(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	c := NewCollector(log, nil, "testnet", mockLocationsFetcher([]collector.LocationMatch{}))

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

	c := NewCollector(log, nil, "testnet", mockLocationsFetcher([]collector.LocationMatch{}))

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

	c := NewCollector(log, nil, "testnet", mockLocationsFetcher([]collector.LocationMatch{}))

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
		log:              log,
		getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{}),
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
		log:              log,
		getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{}),
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
		log:              log,
		getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{}),
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

	t.Run("BuildLocationMapping Error", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		jobIDsFile := filepath.Join(tempDir, "jobs.json")

		log := logger.With("test", t.Name())

		c := &Collector{
			client: &MockWheresitupClient{
				GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
					return nil, errors.New("mapping error")
				},
			},
			log:              log,
			getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{}),
		}

		// The mapping only labels records, so a pass with nothing to poll never builds it.
		state := NewState(jobIDsFile)
		require.NoError(t, state.AddJobIDs([]string{"job-123"}))

		err := c.ExportJobResults(t.Context(), jobIDsFile)
		require.Error(t, err, "Expected error from buildLocationMapping failure")
	})

	t.Run("JobResults In Progress", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		jobIDsFile := filepath.Join(tempDir, "jobs.json")

		log := logger.With("test", t.Name())

		c := &Collector{
			client: &MockWheresitupClient{
				// A usable mapping, or the pass stops before polling to protect the jobs.
				GetNearestSourcesForLocationsFunc: exportTestClient(nil).GetNearestSourcesForLocationsFunc,
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
			log:              log,
			getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{{LocationCode: "lax"}}),
		}

		// Save a job ID
		state := NewState(jobIDsFile)
		_ = state.AddJobIDs([]string{"job-in-progress"})

		err := c.ExportJobResults(t.Context(), jobIDsFile)
		require.NoError(t, err, "ExportJobResults() error = %v", err)

		// Job should still be in the file
		state2 := NewState(jobIDsFile)
		_ = state2.Load()
		remainingJobs := state2.GetJobIDs()
		require.Len(t, remainingJobs, 1, "Expected job to remain for in-progress status, got %v", remainingJobs)
	})

	t.Run("High Failure Rate Logs Error", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		jobIDsFile := filepath.Join(tempDir, "jobs.json")

		captureHandler := newTestLogHandler()
		log := slog.New(captureHandler)

		mockExporter := &MockExporter{
			WriteRecordsFunc: func(ctx context.Context, records []exporter.Record) error {
				return nil
			},
		}

		c := &Collector{
			client: &MockWheresitupClient{
				GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
					return []LocationSourceMatch{
						{
							LocationMatch:  collector.LocationMatch{LocationCode: "NYC"},
							NearestSources: []Source{{ID: "src1", Name: "new_york"}},
						},
					}, nil
				},
				GetJobResultsFunc: func(ctx context.Context, jobID string) (*JobResultResponse, error) {
					// Make most jobs fail to trigger the >10% failure rate
					if jobID == "job-success-1" || jobID == "job-success-2" {
						// 2 out of 15 succeed (13.3% success rate, 86.7% failure rate)
						// Create a successful response with minimal structure
						response := &JobResultResponse{}
						response.Request.StartTime = 1234567890

						// Create the ping result with min latency
						pingResult := PingResult{}
						pingResult.Summary.Summary.Min = "10.5"

						response.Response.Complete = map[string]ServiceResult{
							"new_york": {
								Ping: pingResult,
							},
						}
						return response, nil
					}
					// All other jobs fail
					return nil, errors.New("job failed")
				},
			},
			log:              log,
			exporter:         mockExporter,
			getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{{LocationCode: "NYC"}}),
		}

		// Save 15 job IDs (2 will succeed, 13 will fail = 86.7% failure rate)
		state := NewState(jobIDsFile)
		jobIDs := []string{
			"job-fail-1", "job-fail-2", "job-fail-3", "job-fail-4", "job-fail-5",
			"job-fail-6", "job-fail-7", "job-fail-8", "job-fail-9", "job-fail-10",
			"job-fail-11", "job-fail-12", "job-fail-13",
			"job-success-1", "job-success-2",
		}
		_ = state.AddJobIDs(jobIDs)

		err := c.ExportJobResults(t.Context(), jobIDsFile)
		require.NoError(t, err, "ExportJobResults() should not return error")

		// Check that an error was logged due to high failure rate
		record, _ := captureHandler.only(t, "High failure rate for Wheresitup job results")
		require.Equal(t, slog.LevelError, record.Level, "High failure rate should be logged at ERROR level")
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

	c := &Collector{client: mockClient, log: log, exporter: e, getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{})}
	// Set a very short wait timeout for testing
	c.SetJobWaitTimeout(1 * time.Millisecond)

	// Mock the location fetcher to avoid blockchain calls
	c.getLocationsFunc = func(ctx context.Context) []collector.LocationMatch {
		return []collector.LocationMatch{
			{LocationCode: "NYC", Latitude: 40.7128, Longitude: -74.0060},
			{LocationCode: "LON", Latitude: 51.5074, Longitude: -0.1278},
		}
	}

	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0755))

	// Now that interval validation is removed, we can use a short interval
	interval := 50 * time.Millisecond

	// A cycle can outrun the sampling interval on a loaded machine and ExportJobResults
	// stops polling once the context is done, so waiting for the export beats racing a
	// fixed deadline. This timeout is only a backstop against a hung Run.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx, interval, false, "jobs.json", stateDir)
	}()

	// Cancelling mid-cycle is safe for the assertions below: the rest of the cycle (CSV
	// write, state update) makes no context checks, and Run only returns between cycles.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return jobCreationCalled && exportCalled && exportedJobID != ""
	}, 20*time.Second, 5*time.Millisecond, "Run should have created and exported a job")

	cancel()
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
	require.Contains(t, csvStr, "source_exchange_code,target_exchange_code,timestamp,latency",
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

func TestInitializeCreditBalance(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		mockClient := &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 15000, nil
			},
		}

		c := &Collector{
			client: mockClient,
			log:    logger,
		}

		err := c.InitializeCreditBalance(context.Background())
		require.NoError(t, err)
	})

	t.Run("Low credit warning", func(t *testing.T) {
		t.Parallel()

		mockClient := &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 500, nil // Below CreditWarningThreshold
			},
		}

		c := &Collector{
			client: mockClient,
			log:    logger,
		}

		err := c.InitializeCreditBalance(context.Background())
		require.NoError(t, err)
	})

	t.Run("API error", func(t *testing.T) {
		t.Parallel()

		mockClient := &MockWheresitupClient{
			GetCreditFunc: func(ctx context.Context) (int, error) {
				return 0, errors.New("API error")
			},
		}

		c := &Collector{
			client: mockClient,
			log:    logger,
		}

		err := c.InitializeCreditBalance(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get Wheresitup credit balance")
	})
}

// writeJobState writes a state file directly so tests can set arbitrary per-job ages, which
// the Add* helpers cannot express: they stamp every job in a call from one clock.
func writeJobState(t *testing.T, filename string, jobs []JobEntry, circuits []string) {
	t.Helper()
	data, err := json.Marshal(struct {
		Jobs     []JobEntry `json:"jobs"`
		Circuits []string   `json:"circuits,omitempty"`
	}{Jobs: jobs, Circuits: circuits})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filename, data, 0o600))
}

// inProgressResults returns what GetJobResults really returns for a running or expired job:
// "complete" arrives as an empty array and "in_progress" as an object, so both fail to decode
// and production classifies the job by matching this error string.
func inProgressResults() error {
	return errors.New("failed to decode response: json: cannot unmarshal array into Go struct field .response.complete of type map[string]wheresitup.ServiceResult")
}

func completedResults(sourceName string, startTime int64, minLatencyMillis string) *JobResultResponse {
	results := &JobResultResponse{}
	results.Request.StartTime = startTime
	results.Request.URL = "http://los_angeles.wonderproxy.com"

	pingResult := PingResult{}
	pingResult.Summary.Summary.Min = minLatencyMillis
	results.Response.Complete = map[string]ServiceResult{sourceName: {Ping: pingResult}}

	return results
}

// exportTestCollector maps the source names used above onto exchange codes, so exported
// records carry real circuit labels.
func exportTestCollector(log *slog.Logger, client clientInterface, exp exporter.Exporter) *Collector {
	return &Collector{
		client:   client,
		log:      log,
		exporter: exp,
		env:      "test",
		getLocationsFunc: mockLocationsFetcher([]collector.LocationMatch{
			{LocationCode: "lax", Latitude: 34, Longitude: -118},
			{LocationCode: "nyc", Latitude: 40, Longitude: -74},
		}),
	}
}

func exportTestClient(getJobResults func(ctx context.Context, jobID string) (*JobResultResponse, error)) *MockWheresitupClient {
	return &MockWheresitupClient{
		GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
			return []LocationSourceMatch{
				{
					LocationMatch:  collector.LocationMatch{LocationCode: "lax"},
					NearestSources: []Source{{Name: "los_angeles"}},
					SourceCount:    1,
				},
				{
					LocationMatch:  collector.LocationMatch{LocationCode: "nyc"},
					NearestSources: []Source{{Name: "new_york"}},
					SourceCount:    1,
				},
			}, nil
		},
		GetJobResultsFunc: getJobResults,
	}
}

func TestInternetLatency_Wheresitup_ExportJobResults_DropsExpiredJobs(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	now := time.Now()
	writeJobState(t, jobIDsFile, []JobEntry{
		{JobID: "job-3h", CreatedAt: now.Add(-3 * time.Hour)},
		{JobID: "job-90m", CreatedAt: now.Add(-90 * time.Minute)},
		{JobID: "job-30m", CreatedAt: now.Add(-30 * time.Minute)},
		{JobID: "job-1m", CreatedAt: now.Add(-1 * time.Minute)},
	}, []string{"lax → nyc", "lax → sin"})

	var mu sync.Mutex
	var polled []string
	var written []exporter.Record
	client := exportTestClient(func(ctx context.Context, jobID string) (*JobResultResponse, error) {
		mu.Lock()
		polled = append(polled, jobID)
		mu.Unlock()
		if jobID == "job-1m" {
			return completedResults("new_york", now.Unix(), "12.5"), nil
		}
		return nil, inProgressResults()
	})
	exp := &MockExporter{WriteRecordsFunc: func(ctx context.Context, records []exporter.Record) error {
		mu.Lock()
		written = append(written, records...)
		mu.Unlock()
		return nil
	}}

	c := exportTestCollector(log, client, exp)
	require.NoError(t, c.ExportJobResults(t.Context(), jobIDsFile))

	// Newest first, so a pass cut short loses the stale end rather than the fresh one.
	require.Equal(t, []string{"job-1m", "job-30m"}, polled)

	require.Len(t, written, 1)
	require.Equal(t, "nyc", written[0].SourceExchangeCode)
	require.Equal(t, "lax", written[0].TargetExchangeCode)

	state := NewState(jobIDsFile)
	require.NoError(t, state.Load())
	require.Equal(t, []string{"job-30m"}, state.GetJobIDs())

	dropped, droppedAt := handler.only(t, "Wheresitup - Dropping expired jobs from tracking without polling")
	require.Equal(t, int64(2), attr(t, dropped, "expired_count").Int64())
	require.Equal(t, MaxJobAge, attr(t, dropped, "max_job_age").Duration())

	summary, summaryAt := handler.only(t, "Operation completed: Wheresitup export_job_results")
	require.Equal(t, int64(2), attr(t, summary, "expired_count").Int64())
	require.Equal(t, int64(2), attr(t, summary, "total_jobs").Int64())
	require.Equal(t, int64(1), attr(t, summary, "processed_count").Int64())
	require.Less(t, droppedAt, summaryAt, "expired jobs must be dropped before the cycle summary")

	// lax → sin produced nothing, and must be reported missing even though the same cycle
	// did export a sample for the other circuit.
	missing, _ := handler.only(t, "Wheresitup - Tracked missing samples")
	require.Equal(t, int64(1), attr(t, missing, "missing_samples").Int64())
}

// A job crossing the cutoff between two passes must survive creation's save, which runs
// roughly 30s ahead of the export pass, or its expiry is never counted.
func TestInternetLatency_Wheresitup_ExportJobResults_SaveDoesNotEvictBeforeExpiryIsCounted(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	state := NewState(jobIDsFile)
	require.NoError(t, state.AddJobIDsWithCircuits([]string{"job-expired"}, []string{"lax → nyc"}, time.Now().Add(-2*time.Hour)))

	saved := NewState(jobIDsFile)
	require.NoError(t, saved.Load())
	require.Equal(t, []string{"job-expired"}, saved.GetJobIDs(), "the save must not have pruned the expired job")

	client := exportTestClient(func(ctx context.Context, jobID string) (*JobResultResponse, error) {
		t.Errorf("expired job %s must not be polled", jobID)
		return nil, inProgressResults()
	})

	c := exportTestCollector(log, client, &MockExporter{})
	require.NoError(t, c.ExportJobResults(t.Context(), jobIDsFile))

	dropped, _ := handler.only(t, "Wheresitup - Dropping expired jobs from tracking without polling")
	require.Equal(t, int64(1), attr(t, dropped, "expired_count").Int64())

	after := NewState(jobIDsFile)
	require.NoError(t, after.Load())
	require.Empty(t, after.GetJobIDs())
}

// A cycle in which every tracked job has expired still has to report the samples it owed,
// which is the signal that was absent throughout the incident this behaviour comes from.
func TestInternetLatency_Wheresitup_ExportJobResults_AllExpiredReportsMissingSamples(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	now := time.Now()
	writeJobState(t, jobIDsFile, []JobEntry{
		{JobID: "job-2h", CreatedAt: now.Add(-2 * time.Hour)},
		{JobID: "job-3h", CreatedAt: now.Add(-3 * time.Hour)},
	}, []string{"lax → nyc", "lax → sin"})

	client := exportTestClient(func(ctx context.Context, jobID string) (*JobResultResponse, error) {
		t.Errorf("expired job %s must not be polled", jobID)
		return nil, inProgressResults()
	})

	c := exportTestCollector(log, client, &MockExporter{})
	require.NoError(t, c.ExportJobResults(t.Context(), jobIDsFile))

	missing, _ := handler.only(t, "Wheresitup - Tracked missing samples")
	require.Equal(t, int64(2), attr(t, missing, "missing_samples").Int64())
	require.Equal(t, int64(2), attr(t, missing, "expected_circuits").Int64())

	state := NewState(jobIDsFile)
	require.NoError(t, state.Load())
	require.Empty(t, state.GetJobIDs())
}

// After a stall the vendor completes several of a circuit's jobs in one pass. Each stands for
// the interval it was created in, so all are exported, in the order the positional encoding
// downstream assumes.
func TestInternetLatency_Wheresitup_ExportJobResults_ExportsEveryCompletedJobInTimeOrder(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	now := time.Now()
	writeJobState(t, jobIDsFile, []JobEntry{
		{JobID: "job-18m", CreatedAt: now.Add(-18 * time.Minute)},
		{JobID: "job-12m", CreatedAt: now.Add(-12 * time.Minute)},
		{JobID: "job-6m", CreatedAt: now.Add(-6 * time.Minute)},
	}, []string{"lax → nyc"})

	startTimes := map[string]time.Time{
		"job-18m": now.Add(-18 * time.Minute),
		"job-12m": now.Add(-12 * time.Minute),
		"job-6m":  now.Add(-6 * time.Minute),
	}

	var mu sync.Mutex
	var written []exporter.Record
	client := exportTestClient(func(ctx context.Context, jobID string) (*JobResultResponse, error) {
		return completedResults("new_york", startTimes[jobID].Unix(), "12.5"), nil
	})
	exp := &MockExporter{WriteRecordsFunc: func(ctx context.Context, records []exporter.Record) error {
		mu.Lock()
		written = append(written, records...)
		mu.Unlock()
		return nil
	}}

	c := exportTestCollector(log, client, exp)
	require.NoError(t, c.ExportJobResults(t.Context(), jobIDsFile))

	require.Len(t, written, 3, "every completed job stands for one interval and must be exported")
	require.True(t, sort.SliceIsSorted(written, func(i, j int) bool {
		return written[i].Timestamp.Before(written[j].Timestamp)
	}), "records must reach the exporter oldest first, got %v", written)
	require.Equal(t, startTimes["job-18m"].Unix(), written[0].Timestamp.Unix())
	require.Equal(t, startTimes["job-6m"].Unix(), written[2].Timestamp.Unix())

	summary, _ := handler.only(t, "Operation completed: Wheresitup export_job_results")
	require.Equal(t, int64(3), attr(t, summary, "processed_count").Int64())
}

// GetLocations fails open with an empty slice when the ledger fetch fails. Polling on would
// label every record Unknown, the exporter would drop them without an error, and the jobs
// would be removed as completed, so the pass has to stop with the jobs still tracked.
func TestInternetLatency_Wheresitup_ExportJobResults_KeepsJobsWhenLocationMappingIsEmpty(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	writeJobState(t, jobIDsFile, []JobEntry{
		{JobID: "job-2m", CreatedAt: time.Now().Add(-2 * time.Minute)},
	}, []string{"lax → nyc", "lax → sin"})

	client := &MockWheresitupClient{
		GetNearestSourcesForLocationsFunc: func(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
			return nil, nil
		},
		GetJobResultsFunc: func(ctx context.Context, jobID string) (*JobResultResponse, error) {
			t.Errorf("job %s must not be polled without a location mapping", jobID)
			return nil, inProgressResults()
		},
	}

	c := exportTestCollector(log, client, &MockExporter{})
	require.Error(t, c.ExportJobResults(t.Context(), jobIDsFile), "an empty mapping must fail the cycle, not pass silently")

	state := NewState(jobIDsFile)
	require.NoError(t, state.Load())
	require.Equal(t, []string{"job-2m"}, state.GetJobIDs(), "the job must stay tracked for the next cycle")

	// The cycle still owes both circuits, and that has to be visible.
	missing, _ := handler.only(t, "Wheresitup - Tracked missing samples")
	require.Equal(t, int64(2), attr(t, missing, "missing_samples").Int64())
}

// A cancelled context must stop the pass rather than walk the remaining jobs issuing calls
// that can only fail, which is what a shutdown in the middle of a pass used to do.
func TestInternetLatency_Wheresitup_ExportJobResults_StopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")
	now := time.Now()
	writeJobState(t, jobIDsFile, []JobEntry{
		{JobID: "job-b", CreatedAt: now.Add(-3 * time.Minute)},
		{JobID: "job-a", CreatedAt: now.Add(-2 * time.Minute)},
	}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	var mu sync.Mutex
	var polled []string
	client := exportTestClient(func(ctx context.Context, jobID string) (*JobResultResponse, error) {
		mu.Lock()
		polled = append(polled, jobID)
		mu.Unlock()
		cancel()
		return nil, inProgressResults()
	})

	c := exportTestCollector(log, client, &MockExporter{})
	require.NoError(t, c.ExportJobResults(ctx, jobIDsFile))

	require.Equal(t, []string{"job-a"}, polled, "the pass must stop after the context is cancelled")
	handler.only(t, "Wheresitup - Stopping job export early")
}

func TestInternetLatency_Wheresitup_JobCreation_ExpireAfterIsConsistent(t *testing.T) {
	t.Parallel()

	handler := newTestLogHandler()
	log := slog.New(handler).With("test", t.Name())

	jobIDsFile := filepath.Join(t.TempDir(), "jobs.json")

	var mu sync.Mutex
	var requests []map[string]any
	c := &Collector{
		client: &MockWheresitupClient{
			GetNearestSourcesForLocationsFunc: exportTestClient(nil).GetNearestSourcesForLocationsFunc,
			CreateJobWithRequestFunc: func(ctx context.Context, request any, debug bool) (*JobResponse, error) {
				mu.Lock()
				requests = append(requests, request.(map[string]any))
				mu.Unlock()
				return &JobResponse{ID: "job-123", Status: "pending"}, nil
			},
		},
		log: log,
	}

	locations := []collector.LocationMatch{
		{LocationCode: "lax"},
		{LocationCode: "nyc"},
	}
	require.NoError(t, c.RunJobCreation(t.Context(), locations, false, jobIDsFile))

	// A log claiming an expiry the payload never asked for is how the original bug hid.
	require.Len(t, requests, 1)
	options := requests[0]["options"].(map[string]any)
	require.Equal(t, "1 hour", options["expire_after"])

	logged, _ := handler.only(t, "Wheresitup creating ping jobs between locations")
	require.Equal(t, "1 hour", attr(t, logged, "expire_after").String())
	require.Equal(t, time.Hour, JobExpireAfter, "jobExpireAfterParam must describe JobExpireAfter")

	// Stamped with the start of the pass, so a long pass does not understate the first job.
	state := NewState(jobIDsFile)
	require.NoError(t, state.Load())
	require.Len(t, state.Jobs, 1)
	require.WithinDuration(t, time.Now(), state.Jobs[0].CreatedAt, time.Minute)
}
