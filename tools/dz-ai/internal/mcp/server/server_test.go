package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"

	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/telemetry/latency"
	mcpgeoip "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/geoip"
)

func TestAI_MCP_Server_ReadyzHandler(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db := testDB(t)

	geoIPStore, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: log,
		DB:     db,
	})
	require.NoError(t, err)
	if err := geoIPStore.CreateTablesIfNotExists(); err != nil {
		t.Fatalf("failed to create geoip tables: %v", err)
	}

	mockGeoIPResolver := &mockGeoIPResolver{}

	svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
		Logger:            log,
		Clock:             clockwork.NewFakeClock(),
		ServiceabilityRPC: &mockServiceabilityRPC{},
		RefreshInterval:   time.Second,
		DB:                db,
		GeoIPStore:        geoIPStore,
		GeoIPResolver:     mockGeoIPResolver,
	})
	require.NoError(t, err)

	telemView, err := dztelemlatency.NewView(dztelemlatency.ViewConfig{
		Logger:                 log,
		Clock:                  clockwork.NewFakeClock(),
		TelemetryRPC:           &mockTelemetryRPC{},
		EpochRPC:               &mockEpochRPC{},
		MaxConcurrency:         32,
		InternetLatencyAgentPK: solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112"),
		InternetDataProviders:  []string{"test-provider"},
		DB:                     db,
		Serviceability:         svcView,
		RefreshInterval:        time.Second,
	})
	require.NoError(t, err)

	s := &Server{
		cfg: Config{
			Logger: log,
		},
		serviceabilityView: svcView,
		telemetryView:      telemView,
	}

	t.Run("both not ready", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()

		s.readyzHandler(rr, req)

		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
		require.Equal(t, "serviceability view not ready\n", rr.Body.String())
	})
}
