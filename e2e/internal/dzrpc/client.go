// Package dzrpc provides a lightweight client for querying DoubleZero
// serviceability program accounts via Solana JSON-RPC.
package dzrpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client queries on-chain state via Solana JSON-RPC.
type Client struct {
	rpcURL    string
	programID string
}

// NewClient creates a new DZ RPC client.
func NewClient(rpcURL, programID string) *Client {
	return &Client{rpcURL: rpcURL, programID: programID}
}

// Account represents a Solana account from getProgramAccounts.
type Account struct {
	Pubkey string
	Data   []byte
}

// GetProgramAccounts fetches all accounts owned by the program with an optional
// memcmp filter at offset 0.
func (c *Client) GetProgramAccounts(ctx context.Context, filterByte *byte) ([]Account, error) {
	filters := []any{}
	if filterByte != nil {
		filters = append(filters, map[string]any{
			"memcmp": map[string]any{
				"offset":   0,
				"bytes":    base64.StdEncoding.EncodeToString([]byte{*filterByte}),
				"encoding": "base64",
			},
		})
	}

	params := []any{
		c.programID,
		map[string]any{
			"encoding": "base64",
			"filters":  filters,
		},
	}

	result, err := c.rpcCall(ctx, "getProgramAccounts", params)
	if err != nil {
		return nil, err
	}

	var rawAccounts []struct {
		Pubkey  string `json:"pubkey"`
		Account struct {
			Data []any `json:"data"`
		} `json:"account"`
	}

	if err := json.Unmarshal(result, &rawAccounts); err != nil {
		return nil, fmt.Errorf("failed to parse accounts: %w", err)
	}

	var accounts []Account
	for _, raw := range rawAccounts {
		if len(raw.Account.Data) < 1 {
			continue
		}
		dataStr, ok := raw.Account.Data[0].(string)
		if !ok {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			continue
		}
		accounts = append(accounts, Account{
			Pubkey: raw.Pubkey,
			Data:   data,
		})
	}

	return accounts, nil
}

func (c *Client) rpcCall(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
