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

	idx := testIndexer(t)
	logger := testLogger(t)
	s := &Server{
		log: logger,
		cfg: Config{
			Logger:  logger,
			Querier: testQuerier(t, idx),
			Indexer: idx,
		},
	}

	t.Run("both not ready", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()

		s.readyzHandler(rr, req)

		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		require.Equal(t, "indexer not ready\n", rr.Body.String())
	})
}
