package serviceability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// slotTimeSeconds is the *approximate* time for a Solana slot (400ms).
// NOTE: This is a target. The actual slot time varies with network conditions.
// For more accurate calculations, one would need to use other RPC methods
// like `getBlockTime` for specific slots, which is beyond the scope of this tool.
const slotTimeSeconds = 0.4

// EpochInfoResponse defines the structure of the top-level JSON-RPC response.
type EpochInfoResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  EpochInfoResult `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

// EpochInfoResult maps to the "result" object in the RPC response.
type EpochInfoResult struct {
	AbsoluteSlot uint64 `json:"absoluteSlot"`
	Epoch        uint64 `json:"epoch"`
	SlotIndex    uint64 `json:"slotIndex"`
	SlotsInEpoch uint64 `json:"slotsInEpoch"`
}

// RPCError represents an error object returned by the JSON-RPC server.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error makes RPCError conform to the standard Go error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC Error %d: %s", e.Code, e.Message)
}

// GetEpochInfo fetches epoch information from a Solana-compatible RPC endpoint.
func GetEpochInfo(ctx context.Context, client *http.Client, rpcURL string) (*EpochInfoResult, error) {
	// Prepare the request body for the "getEpochInfo" method.
	requestBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getEpochInfo",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request to %s failed: %w", rpcURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %s", resp.Status)
	}

	var rpcResponse EpochInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	if rpcResponse.Error != nil {
		return nil, rpcResponse.Error
	}

	return &rpcResponse.Result, nil
}

// CalculateEpochTimes calculates the estimated start time of the previous and next epochs.
func CalculateEpochTimes(slotIndex, slotsInEpoch uint64) (currentEpochStartTime, nextEpochTime time.Time) {
	nowUTC := time.Now().UTC()

	// calculate epoch start
	secondsSinceEpochStart := float64(slotIndex) * slotTimeSeconds
	durationSinceEpochStart := time.Duration(secondsSinceEpochStart * float64(time.Second))
	currentEpochStartTime = nowUTC.Add(-durationSinceEpochStart)

	// calculate next epoch start
	slotsUntilNextEpoch := slotsInEpoch - slotIndex
	secondsUntilNextEpoch := float64(slotsUntilNextEpoch) * slotTimeSeconds
	durationUntilNextEpoch := time.Duration(secondsUntilNextEpoch * float64(time.Second))
	nextEpochTime = nowUTC.Add(durationUntilNextEpoch)

	return currentEpochStartTime, nextEpochTime
}

// GetEpochStatus retrieves the current epoch and calculates the start times of the previous and next epochs.
func GetEpochStatus(ctx context.Context, client *http.Client, rpcURL string) (currEpoch uint64, prevEpochStart, nextEpochStart time.Time, err error) {
	epochInfo, err := GetEpochInfo(ctx, client, rpcURL)
	if err != nil {
		return 0, time.Time{}, time.Time{}, fmt.Errorf("failed to get epoch info: %w", err)
	}

	currEpoch = epochInfo.Epoch
	prevEpochStart, nextEpochStart = CalculateEpochTimes(epochInfo.SlotIndex, epochInfo.SlotsInEpoch)
	return currEpoch, prevEpochStart, nextEpochStart, nil
}
