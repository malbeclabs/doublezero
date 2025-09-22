package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRpcClient is a mock implementation of the RpcClient interface for testing.
type mockRpcClient struct {
	totalSupply float64
	err         error
}

func (m *mockRpcClient) GetTotalSupply(ctx context.Context) (float64, error) {
	return m.totalSupply, m.err
}

func TestApiServer_SupplyEndpoint(t *testing.T) {
	today := time.Now().Format("2006-01-02")

	testCases := []struct {
		name                      string
		mockTotalSupply           float64
		estimatedSupplyMap        map[string]float64
		expectedCirculatingSupply float64
		expectedStatusCode        int
	}{
		{
			name:            "valid supply",
			mockTotalSupply: 10_000_000_000.0,
			estimatedSupplyMap: map[string]float64{
				today: 3_000_000_000.0,
			},
			expectedCirculatingSupply: 3_000_000_000.0,
			expectedStatusCode:        http.StatusOK,
		},
		{
			name:            "burn",
			mockTotalSupply: 9_500_000_000.0,
			estimatedSupplyMap: map[string]float64{
				today: 3_000_000_000.0,
			},
			expectedCirculatingSupply: 2_500_000_000.0,
			expectedStatusCode:        http.StatusOK,
		},
		{
			name:            "inflation",
			mockTotalSupply: 10_500_000_000.0,
			estimatedSupplyMap: map[string]float64{
				today: 3_000_000_000.0,
			},
			expectedCirculatingSupply: 3_500_000_000.0,
			expectedStatusCode:        http.StatusOK,
		},
		{
			name:            "missing estimated supply for date",
			mockTotalSupply: 10_000_000_000.0,
			estimatedSupplyMap: map[string]float64{
				"2000-01-01": 1.0,
			},
			expectedStatusCode: http.StatusInternalServerError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &mockRpcClient{
				totalSupply: tc.mockTotalSupply,
				err:         nil,
			}

			apiServer, err := NewApiServer(
				WithRpcClient(mockClient),
				WithEstimatedSupply(tc.estimatedSupplyMap),
			)
			require.NoError(t, err, "Failed to create API server")

			errCh := make(chan error, 1)
			go func() {
				errCh <- apiServer.Run()
			}()

			// give the server a moment to start.
			time.Sleep(100 * time.Millisecond)

			resp, err := http.Get("http://localhost:8080/api/v1/2z/supply")
			require.NoError(t, err, "Failed to send request to /supply endpoint")
			defer resp.Body.Close()

			assert.Equal(t, tc.expectedStatusCode, resp.StatusCode)

			if tc.expectedStatusCode == http.StatusOK {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err, "Failed to read response body")

				supply, err := strconv.ParseFloat(string(body), 64)
				require.NoError(t, err, "Failed to parse supply from response body")
				assert.Equal(t, tc.expectedCirculatingSupply, supply)
			} else {
				io.Copy(io.Discard, resp.Body) // nolint:errcheck
			}

			err = apiServer.Shutdown()
			require.NoError(t, err, "Server shutdown failed")

			runErr := <-errCh
			assert.NoError(t, runErr, "apiServer.Run() returned an unexpected error")
		})
	}
}
