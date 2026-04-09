package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupClickHouseContainer(t *testing.T) (clickhouse.Conn, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := chmodule.Run(ctx,
		"clickhouse/clickhouse-server:24.3",
	)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "9000/tcp")
	require.NoError(t, err)

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
		},
	})
	require.NoError(t, err)
	require.NoError(t, conn.Ping(ctx))

	// Create the table
	err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS "default".controller_grpc_getconfig_success (
			timestamp DateTime64(3),
			device_pubkey LowCardinality(String)
		) ENGINE = MergeTree
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, device_pubkey)
	`)
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		_ = container.Terminate(ctx)
	}

	return conn, cleanup
}

func TestClickHouseClient_ControllerCallCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	conn, cleanup := setupClickHouseContainer(t)
	defer cleanup()

	ctx := context.Background()
	client := &ClickHouseClient{conn: conn, db: "default"}

	devicePubkey := "TestDevice123"
	now := time.Now().Truncate(time.Second)

	// Insert records: one per minute for 10 minutes
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(10-i) * time.Minute)
		err := conn.Exec(ctx, fmt.Sprintf(
			`INSERT INTO "default".controller_grpc_getconfig_success (timestamp, device_pubkey) VALUES (?, ?)`,
		), ts, devicePubkey)
		require.NoError(t, err)
	}

	t.Run("full coverage", func(t *testing.T) {
		start := now.Add(-11 * time.Minute)
		end := now
		minutes, err := client.ControllerCallCoverage(ctx, devicePubkey, start, end)
		require.NoError(t, err)
		assert.Equal(t, int64(10), minutes)
	})

	t.Run("partial window", func(t *testing.T) {
		start := now.Add(-5 * time.Minute)
		end := now
		minutes, err := client.ControllerCallCoverage(ctx, devicePubkey, start, end)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, minutes, int64(4))
		assert.LessOrEqual(t, minutes, int64(5))
	})

	t.Run("no data for different device", func(t *testing.T) {
		start := now.Add(-11 * time.Minute)
		end := now
		minutes, err := client.ControllerCallCoverage(ctx, "OtherDevice456", start, end)
		require.NoError(t, err)
		assert.Equal(t, int64(0), minutes)
	})

	t.Run("empty time range", func(t *testing.T) {
		start := now.Add(-1 * time.Hour)
		end := now.Add(-50 * time.Minute)
		minutes, err := client.ControllerCallCoverage(ctx, devicePubkey, start, end)
		require.NoError(t, err)
		assert.Equal(t, int64(0), minutes)
	})
}

func TestClickHouseClient_ControllerCallCoverage_WithGaps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	conn, cleanup := setupClickHouseContainer(t)
	defer cleanup()

	ctx := context.Background()
	client := &ClickHouseClient{conn: conn, db: "default"}

	devicePubkey := "GappyDevice789"
	now := time.Now().Truncate(time.Second)

	// Insert records with gaps: minutes 0,1,2 then skip 3,4, then 5,6,7
	gapMinutes := []int{10, 9, 8, 5, 4, 3}
	for _, m := range gapMinutes {
		ts := now.Add(-time.Duration(m) * time.Minute)
		err := conn.Exec(ctx, fmt.Sprintf(
			`INSERT INTO "default".controller_grpc_getconfig_success (timestamp, device_pubkey) VALUES (?, ?)`,
		), ts, devicePubkey)
		require.NoError(t, err)
	}

	start := now.Add(-11 * time.Minute)
	end := now
	minutes, err := client.ControllerCallCoverage(ctx, devicePubkey, start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(6), minutes, "should count 6 distinct minutes (with 2-minute gap)")
}
