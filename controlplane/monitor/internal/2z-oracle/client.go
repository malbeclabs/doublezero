package twozoracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type SwapRateResponse struct {
	SwapRate     int64  `json:"swapRate"`
	Timestamp    int64  `json:"timestamp"`
	Signature    string `json:"signature"`
	SOLPriceUSD  string `json:"solPriceUsd"`
	TwoZPriceUSD string `json:"twozPriceUsd"`
	CacheHit     bool   `json:"cacheHit"`
}

type HealthResponse struct {
	Healthy        bool           `json:"healthy"`
	HealthChecks   []HealthCheck  `json:"healthChecks"`
	CircuitBreaker CircuitBreaker `json:"circuitBreaker"`
	Timestamp      string         `json:"timestamp"`
}

type HealthCheck struct {
	ServiceType     string `json:"serviceType"`
	Status          string `json:"status"`
	HermesConnected bool   `json:"hermes_connected"`
	CacheConnected  bool   `json:"cache_connected"`
	LastPriceUpdate string `json:"last_price_update"`
}

type CircuitBreaker struct {
	State             string `json:"state"`
	LastFailureReason string `json:"lastFailureReason"`
}

type TwoZOracleClient interface {
	SwapRate(ctx context.Context) (SwapRateResponse, int, error)
	Health(ctx context.Context) (HealthResponse, int, error)
}

type twoZOracleClient struct {
	http    *http.Client
	baseURL string
}

func NewTwoZOracleClient(httpClient *http.Client, baseURL string) TwoZOracleClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &twoZOracleClient{http: httpClient, baseURL: baseURL}
}

func (c *twoZOracleClient) SwapRate(ctx context.Context) (SwapRateResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/swap-rate", nil)
	if err != nil {
		return SwapRateResponse{}, 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return SwapRateResponse{}, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SwapRateResponse{}, resp.StatusCode, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var out SwapRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return SwapRateResponse{}, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

func (c *twoZOracleClient) Health(ctx context.Context) (HealthResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return HealthResponse{}, 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return HealthResponse{}, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, resp.StatusCode, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var out HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HealthResponse{}, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}
