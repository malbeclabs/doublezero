package serviceability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetEpochStatus(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := EpochInfoResponse{
				JSONRPC: "2.0",
				ID:      1,
				Result: EpochInfoResult{
					Epoch:        500,
					SlotIndex:    1000,
					SlotsInEpoch: 432000,
				},
			}
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
		}))
		defer server.Close()

		epoch, prev, next, err := GetEpochStatus(context.Background(), server.Client(), server.URL)
		require.NoError(t, err)
		require.Equal(t, uint64(500), epoch)
		require.False(t, prev.IsZero(), "previous epoch start time should not be zero")
		require.False(t, next.IsZero(), "next epoch start time should not be zero")
		require.True(t, next.After(prev), "next epoch time should be after previous epoch start time")
	})

	t.Run("http request fails", func(t *testing.T) {
		t.Parallel()

		// Create a server that immediately closes to simulate a connection error.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

		server.Close() // Close the server to cause a connection error.

		_, _, _, err := GetEpochStatus(context.Background(), server.Client(), server.URL)
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to get epoch info")
	})

	t.Run("rpc error in response", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := EpochInfoResponse{
				JSONRPC: "2.0",
				ID:      1,
				Error:   &RPCError{Code: -32000, Message: "test error"},
			}
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
		}))
		defer server.Close()

		_, _, _, err := GetEpochStatus(context.Background(), server.Client(), server.URL)
		require.Error(t, err)
		require.ErrorContains(t, err, "RPC Error -32000: test error")
	})
}
