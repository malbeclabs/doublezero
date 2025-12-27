package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"
)

func TestAI_MCP_Server_ReadyzHandler(t *testing.T) {
	t.Parallel()

	s := &Server{
		cfg: Config{
			Logger:  testLogger(t),
			Querier: testQuerier(t),
			Indexer: testIndexer(t),
		},
	}

	t.Run("both not ready", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()

		s.readyzHandler(rr, req)

		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		require.Equal(t, "server not ready\n", rr.Body.String())
	})
}
