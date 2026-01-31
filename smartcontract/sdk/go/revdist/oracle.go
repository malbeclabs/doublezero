package revdist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OracleClient fetches SOL/2Z swap rates from the oracle API.
type OracleClient struct {
	baseURL string
	http    *http.Client
}

// SwapRate contains the current SOL to 2Z swap rate.
type SwapRate struct {
	Rate float64 `json:"swap_rate"`
}

// NewOracleClient creates a new oracle client with the given base URL.
func NewOracleClient(baseURL string) *OracleClient {
	return &OracleClient{
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

// FetchSwapRate fetches the current SOL/2Z swap rate from the oracle.
func (c *OracleClient) FetchSwapRate(ctx context.Context) (*SwapRate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/swap-rate", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching swap rate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oracle returned status %d", resp.StatusCode)
	}

	var rate SwapRate
	if err := json.NewDecoder(resp.Body).Decode(&rate); err != nil {
		return nil, fmt.Errorf("decoding swap rate: %w", err)
	}
	return &rate, nil
}
