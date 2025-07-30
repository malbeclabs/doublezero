package ripeatlas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

const CallDelay = 100 * time.Millisecond

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient collector.HTTPClient
	log        *slog.Logger
}

type Probe struct {
	ID        int     `json:"id"`
	Address   string  `json:"address_v4"`
	AddressV6 string  `json:"address_v6"`
	ASN       int     `json:"asn_v4"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Prefix    string  `json:"prefix_v4"`
	Status    struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Since string `json:"since"`
	} `json:"status"`
	Type     string `json:"type"`
	Geometry struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
}

// GetCoordinates implements the collector.CoordinatesGetter interface
func (p Probe) GetCoordinates() (latitude, longitude float64) {
	return p.Latitude, p.Longitude
}

type ProbesResponse struct {
	Count    int     `json:"count"`
	Next     string  `json:"next"`
	Previous string  `json:"previous"`
	Results  []Probe `json:"results"`
}

type MeasurementDefinition struct {
	Type           string `json:"type"`
	AF             int    `json:"af"`
	Interval       int    `json:"interval"`
	Packets        int    `json:"packets"`
	Size           int    `json:"size"`
	PacketInterval int    `json:"packet_interval"`
	Target         string `json:"target"`
	Description    string `json:"description"`
}

type MeasurementProbe struct {
	Value     int    `json:"value"`
	Type      string `json:"type"`
	Requested int    `json:"requested"`
}

type MeasurementRequest struct {
	Definitions []MeasurementDefinition `json:"definitions"`
	Probes      []MeasurementProbe      `json:"probes"`
}

type MeasurementResponse struct {
	Measurements []int `json:"measurements"`
}

type MeasurementListResponse struct {
	Count    int           `json:"count"`
	Next     string        `json:"next"`
	Previous string        `json:"previous"`
	Results  []Measurement `json:"results"`
}

type Measurement struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Status      struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	} `json:"status"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

type ClientConfig struct {
	BaseURL    string
	APIKey     string
	HTTPClient collector.HTTPClient
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		BaseURL: "https://atlas.ripe.net/api/v2",
		APIKey:  os.Getenv("RIPE_ATLAS_API_KEY"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: logger,
	}
}

func (c *Client) setCommonHeaders(req *http.Request, contentType string) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Key %s", c.APIKey))
	}
	req.Header.Set("User-Agent", "DoubleZero-Collector/1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
}

func (c *Client) fetchProbesWithErrorHandling(ctx context.Context, lat, lng float64, entityName string) ([]Probe, error) {
	probes, err := c.GetProbesInRadius(ctx, lat, lng, 15)
	if err != nil {
		c.log.Warn("Failed to get probes for location",
			slog.String("entity_name", entityName),
			slog.Float64("latitude", lat),
			slog.Float64("longitude", lng),
			slog.String("error", err.Error()))
		collector.APIErrors.WithLabelValues("ripeatlas", "get_probes").Inc()
		return []Probe{}, nil
	}

	return filterValidProbes(probes), nil
}

func (c *Client) makeRequest(ctx context.Context, endpoint string) (*http.Response, error) {
	url := c.BaseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setCommonHeaders(req, "")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		collector.APIErrors.WithLabelValues("ripeatlas", "http_request").Inc()
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		collector.APIErrors.WithLabelValues("ripeatlas", "http_status").Inc()
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	return resp, nil
}

func (c *Client) GetProbesInRadius(ctx context.Context, latitude, longitude float64, radiusKm int) ([]Probe, error) {
	radiusParam := fmt.Sprintf("%.6f,%.6f:%d", latitude, longitude, radiusKm)
	endpoint := "/probes/?radius=" + radiusParam + "&status_name=Connected&is_anchor=true"
	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var response ProbesResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	for i := range response.Results {
		if len(response.Results[i].Geometry.Coordinates) >= 2 {
			response.Results[i].Longitude = response.Results[i].Geometry.Coordinates[0]
			response.Results[i].Latitude = response.Results[i].Geometry.Coordinates[1]
		}
	}

	return response.Results, nil
}

func (c *Client) GetProbesForLocations(ctx context.Context, locations []LocationProbeMatch) ([]LocationProbeMatch, error) {
	var locationMatches []LocationProbeMatch

	for _, location := range locations {
		if location.Latitude == 0 && location.Longitude == 0 {
			locationMatches = append(locationMatches, LocationProbeMatch{
				LocationMatch: location.LocationMatch,
				NearbyProbes:  []Probe{},
				ProbeCount:    0,
			})
			continue
		}

		nearbyProbes, err := c.fetchProbesWithErrorHandling(ctx, location.Latitude, location.Longitude, location.LocationCode)
		if err != nil {
			return nil, err
		}

		locationMatches = append(locationMatches, LocationProbeMatch{
			LocationMatch: location.LocationMatch,
			NearbyProbes:  nearbyProbes,
			ProbeCount:    len(nearbyProbes),
		})

		time.Sleep(CallDelay)
	}

	return locationMatches, nil
}

func (c *Client) CreateMeasurement(ctx context.Context, request MeasurementRequest) (*MeasurementResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal measurement request: %w", err)
	}

	url := c.BaseURL + "/measurements/"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	c.setCommonHeaders(req, "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		collector.APIErrors.WithLabelValues("ripeatlas", "create_measurement").Inc()
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		collector.APIErrors.WithLabelValues("ripeatlas", "create_measurement_status").Inc()
		return nil, fmt.Errorf("measurement creation failed with status %d: %s", resp.StatusCode, string(responseBytes))
	}

	var measurementResponse MeasurementResponse
	if err := json.Unmarshal(responseBytes, &measurementResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal measurement response: %w", err)
	}

	return &measurementResponse, nil
}

func (c *Client) GetAllMeasurements(ctx context.Context) ([]Measurement, error) {
	var allMeasurements []Measurement
	endpoint := "/measurements/my/?status=Ongoing"

	for {
		resp, err := c.makeRequest(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get measurements: %w", err)
		}
		defer resp.Body.Close()

		var response MeasurementListResponse
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode measurements response: %w", err)
		}

		allMeasurements = append(allMeasurements, response.Results...)

		if response.Next == "" {
			break
		}

		endpoint = response.Next
		if len(endpoint) > len(c.BaseURL) {
			endpoint = endpoint[len(c.BaseURL):]
		}
	}

	return allMeasurements, nil
}

func (c *Client) GetMeasurementResultsIncremental(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
	endpoint := fmt.Sprintf("/measurements/%d/results/", measurementID)

	if startTimestamp > 0 {
		endpoint = fmt.Sprintf("%s?start=%d", endpoint, startTimestamp)
	}

	resp, err := c.makeRequest(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get measurement results: %w", err)
	}
	defer resp.Body.Close()

	var results []any
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode measurement results: %w", err)
	}

	return results, nil
}

func (c *Client) StopMeasurement(ctx context.Context, measurementID int) error {
	endpoint := fmt.Sprintf("/measurements/%d", measurementID)
	url := c.BaseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	c.setCommonHeaders(req, "")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		collector.APIErrors.WithLabelValues("ripeatlas", "delete_measurement").Inc()
		return fmt.Errorf("failed to delete measurement: %w", err)
	}
	defer resp.Body.Close()

	var responseBody []byte
	if resp.Body != nil {
		responseBody, _ = io.ReadAll(resp.Body)
	}

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		collector.APIErrors.WithLabelValues("ripeatlas", "delete_measurement_status").Inc()
		return fmt.Errorf("failed to stop measurement %d: status %d, response: %s", measurementID, resp.StatusCode, string(responseBody))
	}

	c.log.Debug("Successfully stopped measurement",
		slog.Int("measurement_id", measurementID),
		slog.Int("status_code", resp.StatusCode))
	return nil
}
