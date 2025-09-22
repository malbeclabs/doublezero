package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	tokenMintPubkey = "J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd"
	rpcUrl          = "https://api.mainnet-beta.solana.com"
)

// SolanaClient is an HTTP client for the Solana JSON-RPC API.
type SolanaClient struct {
	client *http.Client
	rpcURL string
	pubkey string
}

// NewSolanaClient creates a new client for the Solana JSON-RPC API.
func NewSolanaClient() *SolanaClient {
	return &SolanaClient{
		client: &http.Client{},
		rpcURL: rpcUrl,
		pubkey: tokenMintPubkey,
	}
}

// jsonRPCRequest defines the structure for a JSON-RPC request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// jsonRPCResponse defines the structure for the getTokenSupply response.
type jsonRPCResponse struct {
	Result struct {
		Value struct {
			UIAmount float64 `json:"uiAmount"`
		} `json:"value"`
	} `json:"result"`
}

// GetTotalSupply fetches the total supply of a token from the Solana RPC.
func (c *SolanaClient) GetTotalSupply(ctx context.Context) (float64, error) {
	requestBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getTokenSupply",
		Params:  []any{c.pubkey},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.rpcURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return 0, fmt.Errorf("failed to decode response body: %w", err)
	}

	return rpcResp.Result.Value.UIAmount, nil
}
