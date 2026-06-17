package migrations_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"

	"github.com/malbeclabs/doublezero/telemetry/migrations"
)

// TestIsisLatestViews_LastSeenPerNetworkInstance verifies that the isis_global_state_latest
// and isis_overload_bit_latest views group by (device_pubkey, network_instance) so a row
// remains visible after the device stops reporting that network_instance in a later scrape.
//
// Scenario:
//   - Scrape A (older): device reports two network_instances, "default" and "vrf1".
//   - Scrape B (newer): device reports only "default" (vrf1 removed from config).
//
// The view must surface both: the newest "default" row from scrape B AND the most recent
// "vrf1" row from scrape A. If the view ever regresses to GROUP BY device_pubkey, the
// "vrf1" row will be filtered out because its timestamp is not the device's max.
func TestIsisLatestViews_LastSeenPerNetworkInstance(t *testing.T) {
	t.Parallel()
	db := newClickHouseWithMigrations(t)

	const device = "DZdev11111111111111111111111111111111111111111"
	// Anchor the scrapes near now so the rows stay within the table's 30-day
	// TTL; a fixed past date would silently age out and the view would return
	// no rows.
	scrapeA := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	scrapeB := scrapeA.Add(time.Minute)

	// Insert one row per statement. The clickhouse-go database/sql driver only
	// reliably binds placeholders for single-row INSERTs; a multi-row VALUES
	// list with placeholders can silently drop rows, leaving the latest views
	// empty.
	const insertGlobalState = `
		INSERT INTO isis_global_state (timestamp, device_pubkey, network_instance, instance, net, level_capability)
		VALUES (?, ?, ?, ?, ?, ?)`
	mustExec(t, db, insertGlobalState, scrapeA, device, "default", "default", "49.0001.0000.0000.0001.00", "LEVEL_2")
	mustExec(t, db, insertGlobalState, scrapeA, device, "vrf1", "vrf1", "49.0002.0000.0000.0001.00", "LEVEL_2")
	mustExec(t, db, insertGlobalState, scrapeB, device, "default", "default", "49.0001.0000.0000.0001.00", "LEVEL_2")

	const insertOverloadBit = `
		INSERT INTO isis_overload_bit (timestamp, device_pubkey, network_instance, overload_bit)
		VALUES (?, ?, ?, ?)`
	mustExec(t, db, insertOverloadBit, scrapeA, device, "default", false)
	mustExec(t, db, insertOverloadBit, scrapeA, device, "vrf1", true)
	mustExec(t, db, insertOverloadBit, scrapeB, device, "default", false)

	t.Run("isis_global_state_latest", func(t *testing.T) {
		type row struct {
			Timestamp       time.Time
			NetworkInstance string
		}
		rows := selectAll(t, db, 2, `
			SELECT timestamp, network_instance
			FROM isis_global_state_latest
			WHERE device_pubkey = ?
			ORDER BY network_instance
		`, func(r *sql.Rows) (row, error) {
			var v row
			err := r.Scan(&v.Timestamp, &v.NetworkInstance)
			return v, err
		}, device)

		require.Len(t, rows, 2, "expected both network_instances in latest view; got %+v", rows)
		require.Equal(t, "default", rows[0].NetworkInstance)
		require.True(t, rows[0].Timestamp.Equal(scrapeB), "default row should be from newer scrape B, got %s", rows[0].Timestamp)
		require.Equal(t, "vrf1", rows[1].NetworkInstance)
		require.True(t, rows[1].Timestamp.Equal(scrapeA), "vrf1 row should be from scrape A (last sighting), got %s", rows[1].Timestamp)
	})

	t.Run("isis_overload_bit_latest", func(t *testing.T) {
		type row struct {
			Timestamp       time.Time
			NetworkInstance string
			OverloadBit     bool
		}
		rows := selectAll(t, db, 2, `
			SELECT timestamp, network_instance, overload_bit
			FROM isis_overload_bit_latest
			WHERE device_pubkey = ?
			ORDER BY network_instance
		`, func(r *sql.Rows) (row, error) {
			var v row
			err := r.Scan(&v.Timestamp, &v.NetworkInstance, &v.OverloadBit)
			return v, err
		}, device)

		require.Len(t, rows, 2, "expected both network_instances in latest view; got %+v", rows)
		require.Equal(t, "default", rows[0].NetworkInstance)
		require.True(t, rows[0].Timestamp.Equal(scrapeB))
		require.Equal(t, "vrf1", rows[1].NetworkInstance)
		require.True(t, rows[1].Timestamp.Equal(scrapeA))
		require.True(t, rows[1].OverloadBit, "vrf1 last-seen overload_bit should be true")
	})
}

func newClickHouseWithMigrations(t *testing.T) *sql.DB {
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

	host, err := container.ConnectionHost(ctx)
	require.NoError(t, err)

	err = migrations.RunMigrations(host, "default", "default", "", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	db := ch.OpenDB(&ch.Options{
		Addr: []string{host},
		Auth: ch.Auth{Database: "default", Username: "default", Password: ""},
	})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), query, args...)
	require.NoError(t, err, "exec failed: %s", query)
}

// selectAll runs query and returns the scanned rows, retrying until at least
// wantLen rows are visible (or a timeout elapses). ClickHouse inserts are not
// always immediately visible to a subsequent read on a different pooled
// connection, so a freshly-inserted row can be momentarily absent under load;
// polling makes the read deterministic without changing what is asserted.
func selectAll[T any](t *testing.T, db *sql.DB, wantLen int, query string, scan func(*sql.Rows) (T, error), args ...any) []T {
	t.Helper()

	query1 := func() ([]T, error) {
		rows, err := db.QueryContext(context.Background(), query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var out []T
		for rows.Next() {
			v, err := scan(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}

	var out []T
	require.Eventually(t, func() bool {
		res, err := query1()
		if err != nil {
			return false
		}
		out = res
		return len(out) >= wantLen
	}, 10*time.Second, 100*time.Millisecond, "query did not return %d row(s): %s", wantLen, query)

	return out
}
