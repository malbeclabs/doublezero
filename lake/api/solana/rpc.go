package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DefaultRPCURL is the default Solana RPC endpoint
const DefaultRPCURL = "https://api.mainnet-beta.solana.com"

// GetRPCURL returns the configured Solana RPC URL
func GetRPCURL() string {
	url := os.Getenv("SOLANA_RPC_URL")
	if url == "" {
		return DefaultRPCURL
	}
	return url
}

// rpcRequest represents a JSON-RPC 2.0 request
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// rpcResponse represents a JSON-RPC 2.0 response
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError represents a JSON-RPC 2.0 error
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// balanceResult represents the result of a getBalance call
type balanceResult struct {
	Context struct {
		Slot uint64 `json:"slot"`
	} `json:"context"`
	Value uint64 `json:"value"`
}

// GetBalance fetches the SOL balance for an address in lamports
func GetBalance(ctx context.Context, address string) (int64, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "getBalance",
		Params:  []interface{}{address},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", GetRPCURL(), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return 0, fmt.Errorf("RPC error: %s (code %d)", rpcResp.Error.Message, rpcResp.Error.Code)
	}

	var result balanceResult
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		return 0, fmt.Errorf("failed to unmarshal balance result: %w", err)
	}

	return int64(result.Value), nil
}

// LamportsToSOL converts lamports to SOL
func LamportsToSOL(lamports int64) float64 {
	return float64(lamports) / 1_000_000_000
}

// SOLToLamports converts SOL to lamports
func SOLToLamports(sol float64) int64 {
	return int64(sol * 1_000_000_000)
}
