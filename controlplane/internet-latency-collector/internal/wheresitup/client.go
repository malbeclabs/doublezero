package wheresitup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

const CallDelay = 100 * time.Millisecond

type Client struct {
	BaseURL    string
	APIToken   string
	HTTPClient collector.HTTPClient
	log        *slog.Logger
}

// Wheresitup probe devices are called "sources"
type Source struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Title         string `json:"title"`
	Location      string `json:"location"`
	State         string `json:"state"`
	Latitude      string `json:"latitude"`
	Longitude     string `json:"longitude"`
	ContinentName string `json:"continent_name"`
}

func (s Source) GetCoordinates() (latitude, longitude float64) {
	lat, _ := strconv.ParseFloat(s.Latitude, 64)
	lng, _ := strconv.ParseFloat(s.Longitude, 64)
	return lat, lng
}

type SourcesResponse struct {
	Sources []Source `json:"sources"`
}

type LocationSourceMatch struct {
	collector.LocationMatch
	NearestSources []Source
	SourceCount    int
}

type JobResponse struct {
	ID      string `json:"jobID"`
	Status  string `json:"status"`
	Created string `json:"created"`
	Expires string `json:"expires"`
}

type JobResult struct {
	ID      string         `json:"id"`
	Status  string         `json:"status"`
	Created string         `json:"created"`
	Expires string         `json:"expires"`
	Results map[string]any `json:"results"`
}

type JobDetails struct {
	ID        string `json:"-"` // Not in JSON, set manually
	URL       string `json:"url"`
	IP        string `json:"ip"`
	StartTime int64  `json:"start_time"`
	EasyTime  string `json:"easy_time"`
	Expiry    struct {
		Sec  int64 `json:"sec"`
		Usec int   `json:"usec"`
	} `json:"expiry"`
	Services []struct {
		City   string   `json:"city"`
		Server string   `json:"server"`
		Checks []string `json:"checks"`
	} `json:"services"`
}

type PingSummary struct {
	Raw     string `json:"raw"`
	Bytes   string `json:"bytes"`
	IP      string `json:"ip"`
	ICMPReq string `json:"icmp_req"`
	TTL     string `json:"ttl"`
	MS      string `json:"ms"`
}

type PingStatistics struct {
	Transmitted string `json:"transmitted"`
	Received    string `json:"received"`
	PacketLoss  string `json:"packetloss"`
	Time        string `json:"time"`
	Min         string `json:"min"`
	Avg         string `json:"avg"`
	Max         string `json:"max"`
	Mdev        string `json:"mdev"`
	Requested   string `json:"requested"`
	TimedOut    bool   `json:"timedout"`
}

type PingResult struct {
	Raw     string `json:"raw"`
	Summary struct {
		Pings   []PingSummary  `json:"pings"`
		Summary PingStatistics `json:"summary"`
	} `json:"summary"`
}

type ServiceResult struct {
	Ping PingResult `json:"ping"`
}

type CreditResponse struct {
	Current int `json:"current"`
	Used    struct {
		Today     int `json:"today"`
		Yesterday int `json:"yesterday"`
		Week      int `json:"week"`
	} `json:"used"`
}

type JobResultResponse struct {
	Request struct {
		URL       string `json:"url"`
		IP        string `json:"ip"`
		StartTime int64  `json:"start_time"`
		EasyTime  string `json:"easy_time"`
		Expiry    struct {
			Sec  int64 `json:"sec"`
			Usec int   `json:"usec"`
		} `json:"expiry"`
	} `json:"request"`
	Response struct {
		Complete   map[string]ServiceResult `json:"complete"`
		InProgress []any                    `json:"in_progress"`
		Error      []any                    `json:"error"`
	} `json:"response"`
}

type ClientConfig struct {
	BaseURL    string
	APIToken   string
	HTTPClient collector.HTTPClient
}

func NewClient(log *slog.Logger) *Client {
	// Expected format of API token: "CLIENT_ID TOKEN", where both CLIENT_ID and TOKEN are hex strings.
	apiToken := os.Getenv("WHERESITUP_API_TOKEN")

	apiTokenRegex := `^[a-fA-F0-9]+ [a-fA-F0-9]+$`

	if apiToken == "" || !regexp.MustCompile(apiTokenRegex).MatchString(apiToken) {
		if log != nil {
			log.Error("Invalid Wheresitup API token format. Expected regex: " + apiTokenRegex)
		}
		// Optionally, you can panic or return nil here
	}

	return NewClientWithConfig(log, ClientConfig{
		BaseURL:  "https://api.wheresitup.com/v4",
		APIToken: apiToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	})
}

func (c *Client) setCommonHeaders(req *http.Request) {
	if c.APIToken != "" {
		req.Header.Set("Auth", fmt.Sprintf("Bearer %s", c.APIToken))
	} else if c.log != nil {
		c.log.Warn("No API token configured for Wheresitup API")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DoubleZero-Collector/1.0")
}

func NewClientWithConfig(logger *slog.Logger, config ClientConfig) *Client {
	// Set defaults if not provided
	if config.BaseURL == "" {
		config.BaseURL = "https://api.wheresitup.com/v4"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &Client{
		BaseURL:    config.BaseURL,
		APIToken:   config.APIToken,
		HTTPClient: config.HTTPClient,
		log:        logger,
	}
}

func (c *Client) makeRequest(ctx context.Context, endpoint string) (*http.Response, error) {
	url := c.BaseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setCommonHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	return resp, nil
}

func (c *Client) GetAllSources(ctx context.Context) ([]Source, error) {
	resp, err := c.makeRequest(ctx, "/sources")
	if err != nil {
		return nil, fmt.Errorf("failed to get sources: %w", err)
	}
	defer resp.Body.Close()

	var response SourcesResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Sources, nil
}

func (c *Client) GetNearestSources(ctx context.Context, latitude, longitude float64, count int) ([]Source, error) {
	allSources, err := c.GetAllSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all sources: %w", err)
	}

	// Filter valid sources
	var validSources []Source
	for _, source := range allSources {
		// Validate coordinates
		if _, err := strconv.ParseFloat(source.Latitude, 64); err != nil {
			c.log.Warn("Invalid latitude for source",
				slog.String("source_name", source.Name),
				slog.String("latitude_value", source.Latitude))
			continue
		}
		if _, err := strconv.ParseFloat(source.Longitude, 64); err != nil {
			c.log.Warn("Invalid longitude for source",
				slog.String("source_name", source.Name),
				slog.String("longitude_value", source.Longitude))
			continue
		}
		validSources = append(validSources, source)
	}

	// Filter by distance and get nearest N sources using generic functions
	filteredSources := collector.FilterSourcesByDistance(validSources, latitude, longitude)
	nearestSources := collector.GetNearestSourcesSorted(filteredSources, latitude, longitude, count)

	return nearestSources, nil
}

func (c *Client) GetNearestSourcesForLocations(ctx context.Context, locations []collector.LocationMatch) ([]LocationSourceMatch, error) {
	var locationMatches []LocationSourceMatch

	for _, location := range locations {
		// Skip locations without valid coordinates
		if location.Latitude == 0 && location.Longitude == 0 {
			locationMatches = append(locationMatches, LocationSourceMatch{
				LocationMatch:  location,
				NearestSources: []Source{},
				SourceCount:    0,
			})
			continue
		}

		nearestSources, err := c.GetNearestSources(ctx, location.Latitude, location.Longitude, 5)
		if err != nil {
			c.log.Warn("Error fetching nearest sources for location",
				slog.String("location", location.LocationCode),
				slog.String("error", err.Error()))
			// Continue with empty sources rather than failing completely
			nearestSources = []Source{}
		}

		locationMatches = append(locationMatches, LocationSourceMatch{
			LocationMatch:  location,
			NearestSources: nearestSources,
			SourceCount:    len(nearestSources),
		})

		// Add a small delay to avoid rate limiting
		time.Sleep(CallDelay)
	}

	return locationMatches, nil
}

func (c *Client) CreateJob(ctx context.Context, url string) (string, error) {
	// Create a job request using GET endpoint with URL parameter
	endpoint := fmt.Sprintf("/jobs?url=%s", url)
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to create job: %w", err)
	}
	defer resp.Body.Close()

	var jobResponse JobResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&jobResponse); err != nil {
		return "", fmt.Errorf("failed to decode job response: %w", err)
	}

	return jobResponse.ID, nil
}

func (c *Client) CreateJobWithRequest(ctx context.Context, request any, debug bool) (*JobResponse, error) {
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job request: %w", err)
	}

	// Debug: Log the request body
	if debug {
		c.log.Debug("Sending job request",
			slog.String("request_body", string(requestBody)))
	}

	url := c.BaseURL + "/jobs"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setCommonHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	// Debug: Log request headers for debugging
	if debug {
		c.log.Debug("Request headers",
			slog.String("content_type", req.Header.Get("Content-Type")),
			slog.String("accept", req.Header.Get("Accept")),
			slog.String("user_agent", req.Header.Get("User-Agent")),
			slog.String("url", req.URL.String()))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error details
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response body: %w", readErr)
	}

	// Always log the response details for debugging
	if debug {
		c.log.Debug("API response details",
			slog.Int("status_code", resp.StatusCode),
			slog.String("response_body", string(responseBody)),
			slog.Int("response_length", len(responseBody)))
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if debug {
			c.log.Debug("API error response",
				slog.String("response_body", string(responseBody)))
		}
		return nil, fmt.Errorf("job creation failed with status: %d, response: %s", resp.StatusCode, string(responseBody))
	}

	var jobResponse JobResponse
	if err := json.Unmarshal(responseBody, &jobResponse); err != nil {
		// Log the unmarshal error with response body for debugging
		c.log.Warn("Failed to unmarshal job response",
			slog.String("error", err.Error()),
			slog.String("response_body", string(responseBody)),
			slog.Int("response_length", len(responseBody)))
		return nil, fmt.Errorf("failed to decode job response: %w", err)
	}

	// Log the parsed job response
	if debug {
		c.log.Debug("Parsed job response",
			slog.String("job_id", jobResponse.ID),
			slog.String("status", jobResponse.Status),
			slog.String("created", jobResponse.Created),
			slog.String("expires", jobResponse.Expires))
	}

	return &jobResponse, nil
}

func (c *Client) GetJob(ctx context.Context, jobID string) (*JobResult, error) {
	endpoint := fmt.Sprintf("/jobs/%s", jobID)
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}
	defer resp.Body.Close()

	var jobResult JobResult
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&jobResult); err != nil {
		return nil, fmt.Errorf("failed to decode job result: %w", err)
	}

	return &jobResult, nil
}

func (c *Client) GetAllJobs(ctx context.Context) ([]JobDetails, error) {
	endpoint := "/jobs"
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs: %w", err)
	}
	defer resp.Body.Close()

	// The API returns a flat object with job IDs as keys
	var response map[string]JobDetails
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode jobs response: %w", err)
	}

	// Convert map to slice and include job IDs
	var jobs []JobDetails
	for jobID, jobDetails := range response {
		// Set the job ID
		jobDetails.ID = jobID
		jobs = append(jobs, jobDetails)
	}

	return jobs, nil
}

func (c *Client) GetJobResults(ctx context.Context, jobID string) (*JobResultResponse, error) {
	endpoint := fmt.Sprintf("/jobs/%s", jobID)
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get job results: %w", err)
	}
	defer resp.Body.Close()

	var jobResult JobResultResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&jobResult); err != nil {
		return nil, fmt.Errorf("failed to decode job results: %w", err)
	}

	return &jobResult, nil
}

func (c *Client) GetCredit(ctx context.Context) (int, error) {
	endpoint := "/credits"
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return 0, fmt.Errorf("failed to get credit: %w", err)
	}
	defer resp.Body.Close()

	var creditResp CreditResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&creditResp); err != nil {
		return 0, fmt.Errorf("failed to decode credit response: %w", err)
	}

	return creditResp.Current, nil
}
