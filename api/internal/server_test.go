package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRpcClient struct {
	totalSupply float64
	err         error
}

func (m *mockRpcClient) GetTotalSupply(ctx context.Context) (float64, error) {
	return m.totalSupply, m.err
}

func runEndpointTest(t *testing.T, serverOpts []Option, endpoint string, expectedStatusCode int, expectedBody string) {
	t.Helper()

	apiServer, err := NewApiServer(serverOpts...)
	require.NoError(t, err, "Failed to create API server")

	errCh := make(chan error, 1)
	go func() {
		errCh <- apiServer.Run()
	}()

	// give the server a moment to start.
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:8080" + endpoint)
	require.NoError(t, err, "Failed to send request to endpoint %s", endpoint)
	defer resp.Body.Close()

	assert.Equal(t, expectedStatusCode, resp.StatusCode)

	if expectedStatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		assert.Equal(t, expectedBody, string(bytes.TrimSpace(body)))
	} else {
		io.Copy(io.Discard, resp.Body) // nolint:errcheck
	}

	require.NoError(t, apiServer.Shutdown(), "Server shutdown failed")
	assert.NoError(t, <-errCh, "apiServer.Run() returned an unexpected error")
}

func TestApiServer_CirculatingSupplyEndpoint(t *testing.T) {
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
			serverOpts := []Option{
				WithRpcClient(mockClient),
				WithEstimatedSupply(tc.estimatedSupplyMap),
			}
			expectedBody := ""
			if tc.expectedStatusCode == http.StatusOK {
				expectedBody = strconv.FormatFloat(tc.expectedCirculatingSupply, 'f', 1, 64)
			}
			runEndpointTest(t, serverOpts, "/api/v1/2z/circulating-supply", tc.expectedStatusCode, expectedBody)
		})
	}
}

func TestApiServer_TotalSupplyEndpoint(t *testing.T) {
	testCases := []struct {
		name               string
		mockTotalSupply    float64
		expectedStatusCode int
	}{
		{
			name:               "valid total supply",
			mockTotalSupply:    12_345_678.9,
			expectedStatusCode: http.StatusOK,
		},
		{
			name:               "zero total supply",
			mockTotalSupply:    0.0,
			expectedStatusCode: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &mockRpcClient{
				totalSupply: tc.mockTotalSupply,
				err:         nil,
			}
			serverOpts := []Option{
				WithRpcClient(mockClient),
			}
			expectedBody := ""
			if tc.expectedStatusCode == http.StatusOK {
				expectedBody = strconv.FormatFloat(tc.mockTotalSupply, 'f', 1, 64)
			}
			runEndpointTest(t, serverOpts, "/api/v1/2z/total-supply", tc.expectedStatusCode, expectedBody)
		})
	}
}
