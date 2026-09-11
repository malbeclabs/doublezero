package exporter_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/telemetry/migrations"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

func TestInternetLatency_ClickHouseExporter_WritesToClickHouse(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	addr, db := newClickHouseWithMigrations(t)

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		Addr:               addr,
		Database:           "default",
		Username:           "default",
		TLSDisabled:        true,
		Table:              exporter.DefaultClickHouseTable,
		BatchSize:          2,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 10,
	})
	require.NoError(t, err)

	eventTS := time.Now().UTC().Truncate(time.Second)
	before := time.Now().UTC().Truncate(time.Second)

	err = e.WriteRecords(t.Context(), []exporter.Record{
		cloudRecord("eu-west-1", "us-east-1", 1003385, eventTS, 70500*time.Microsecond),
		{
			DataProvider:       exporter.DataProviderNameRIPEAtlas,
			SourceExchangeCode: "sa-east-1",
			TargetExchangeCode: "eu-north-1",
			Timestamp:          eventTS,
			Cloud: &exporter.CloudInfo{
				SourceCloud:     "aws",
				TargetCloud:     "aws",
				ProbeID:         1003391,
				PacketsSent:     3,
				PacketsReceived: 0,
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, e.Close())

	type row struct {
		EventTS         time.Time
		IngestedAt      time.Time
		OriginCloud     string
		OriginRegion    string
		TargetCloud     string
		TargetRegion    string
		DataProvider    string
		ProbeID         uint32
		RttUs           uint32
		PacketsSent     uint8
		PacketsReceived uint8
	}

	rows, err := db.QueryContext(context.Background(), `
		SELECT event_ts, ingested_at, origin_cloud, origin_region, target_cloud, target_region,
		       data_provider, probe_id, rtt_us, packets_sent, packets_received
		FROM cloud_region_latency
		ORDER BY origin_region
	`)
	require.NoError(t, err)
	defer rows.Close()

	var got []row
	for rows.Next() {
		var v row
		require.NoError(t, rows.Scan(&v.EventTS, &v.IngestedAt, &v.OriginCloud, &v.OriginRegion,
			&v.TargetCloud, &v.TargetRegion, &v.DataProvider, &v.ProbeID, &v.RttUs,
			&v.PacketsSent, &v.PacketsReceived))
		got = append(got, v)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)

	require.Equal(t, row{
		EventTS:         eventTS,
		IngestedAt:      got[0].IngestedAt,
		OriginCloud:     "aws",
		OriginRegion:    "eu-west-1",
		TargetCloud:     "aws",
		TargetRegion:    "us-east-1",
		DataProvider:    "ripeatlas",
		ProbeID:         1003385,
		RttUs:           70500,
		PacketsSent:     3,
		PacketsReceived: 3,
	}, got[0])
	require.False(t, got[0].IngestedAt.Before(before), "ingested_at must be stamped at insert time, not left at zero")

	require.Equal(t, row{
		EventTS:         eventTS,
		IngestedAt:      got[1].IngestedAt,
		OriginCloud:     "aws",
		OriginRegion:    "sa-east-1",
		TargetCloud:     "aws",
		TargetRegion:    "eu-north-1",
		DataProvider:    "ripeatlas",
		ProbeID:         1003391,
		RttUs:           0,
		PacketsSent:     3,
		PacketsReceived: 0,
	}, got[1])
}

func TestInternetLatency_ClickHouseExporter_FailsToConstructWhenDatabaseIsMissing(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	addr, _ := newClickHouseWithMigrations(t)

	_, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:      log,
		Addr:        addr,
		Database:    "wrong_database",
		Username:    "default",
		TLSDisabled: true,
		Table:       exporter.DefaultClickHouseTable,
	})
	require.ErrorContains(t, err, "clickhouse ping")
}

func newClickHouseWithMigrations(t *testing.T) (string, *sql.DB) {
	t.Helper()
	ctx := context.Background()

	container, err := clickhouse.Run(ctx,
		"clickhouse/clickhouse-server:23.3.8.21-alpine",
		clickhouse.WithUsername("default"),
		clickhouse.WithPassword(""),
		clickhouse.WithDatabase("default"),
	)
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)

	addr, err := container.ConnectionHost(ctx)
	require.NoError(t, err)

	err = migrations.RunMigrations(addr, "default", "default", "", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	db := ch.OpenDB(&ch.Options{
		Addr: []string{addr},
		Auth: ch.Auth{Database: "default", Username: "default", Password: ""},
	})
	t.Cleanup(func() { _ = db.Close() })
	return addr, db
}
