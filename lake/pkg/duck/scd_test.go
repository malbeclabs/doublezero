package duck

import (
	"context"
	"encoding/csv"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSCDTableViaCSV(t *testing.T) {
	t.Parallel()

	t.Run("creates tables and inserts first snapshot", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		data := []struct {
			id   string
			name string
			age  string
		}{
			{"1", "Alice", "30"},
			{"2", "Bob", "25"},
			{"3", "Charlie", "35"},
		}

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2",
			SnapshotTS:          snapshotTS,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR", "age:INTEGER"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data[i].id, data[i].name, data[i].age})
			},
		)
		require.NoError(t, err)

		// Verify current table
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_scd2_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify a specific row in current
		var name, age string
		var asOfTS time.Time
		var rowHash string
		err = conn.QueryRowContext(context.Background(),
			"SELECT name, age, as_of_ts, row_hash FROM test_scd2_current WHERE id = '1'").Scan(&name, &age, &asOfTS, &rowHash)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)
		require.Equal(t, "30", age)
		require.Equal(t, snapshotTS, asOfTS)
		require.NotEmpty(t, rowHash)

		// Verify history table
		var historyCount int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_scd2_history").Scan(&historyCount)
		require.NoError(t, err)
		require.Equal(t, 3, historyCount)

		// Verify history row
		var validFrom time.Time
		var validTo interface{}
		var op string
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_from, valid_to, op FROM test_scd2_history WHERE id = '1'").Scan(&validFrom, &validTo, &op)
		require.NoError(t, err)
		require.Equal(t, snapshotTS, validFrom)
		require.Nil(t, validTo)   // Should be NULL (open version)
		require.Equal(t, "I", op) // Insert operation
	})

	t.Run("updates existing rows in second snapshot", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_update",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR", "age:INTEGER"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		// First snapshot
		data1 := []struct {
			id   string
			name string
			age  string
		}{
			{"1", "Alice", "30"},
			{"2", "Bob", "25"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data1),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data1[i].id, data1[i].name, data1[i].age})
			},
		)
		require.NoError(t, err)

		// Get initial row hash
		var initialHash string
		err = conn.QueryRowContext(context.Background(),
			"SELECT row_hash FROM test_scd2_update_current WHERE id = '1'").Scan(&initialHash)
		require.NoError(t, err)

		// Second snapshot with updated data
		cfg.SnapshotTS = snapshotTS2
		data2 := []struct {
			id   string
			name string
			age  string
		}{
			{"1", "Alice Updated", "31"}, // Updated
			{"2", "Bob", "25"},           // Unchanged
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data2),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data2[i].id, data2[i].name, data2[i].age})
			},
		)
		require.NoError(t, err)

		// Verify current table has updated data
		var name string
		var asOfTS time.Time
		var newHash string
		err = conn.QueryRowContext(context.Background(),
			"SELECT name, as_of_ts, row_hash FROM test_scd2_update_current WHERE id = '1'").Scan(&name, &asOfTS, &newHash)
		require.NoError(t, err)
		require.Equal(t, "Alice Updated", name)
		require.Equal(t, snapshotTS2, asOfTS)
		require.NotEqual(t, initialHash, newHash) // Hash should have changed

		// Verify history: old version should be closed
		var validTo time.Time
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_update_history WHERE id = '1' AND valid_from = ?", snapshotTS1).Scan(&validTo)
		require.NoError(t, err)
		require.Equal(t, snapshotTS2, validTo)

		// Verify history: new version should be open
		var validTo2 interface{}
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_update_history WHERE id = '1' AND valid_from = ?", snapshotTS2).Scan(&validTo2)
		require.NoError(t, err)
		require.Nil(t, validTo2)

		// Verify history: Bob's row should still have only one version (unchanged)
		var bobHistoryCount int
		err = conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM test_scd2_update_history WHERE id = '2'").Scan(&bobHistoryCount)
		require.NoError(t, err)
		require.Equal(t, 1, bobHistoryCount) // Should only have insert, no update
	})

	t.Run("handles deletes when MissingMeansDeleted is true", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_delete",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: true, // Enable delete tracking
			TrackIngestRuns:     false,
		}

		// First snapshot
		data1 := []struct {
			id   string
			name string
		}{
			{"1", "Alice"},
			{"2", "Bob"},
			{"3", "Charlie"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data1),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data1[i].id, data1[i].name})
			},
		)
		require.NoError(t, err)

		// Second snapshot with one row removed
		cfg.SnapshotTS = snapshotTS2
		data2 := []struct {
			id   string
			name string
		}{
			{"1", "Alice"},
			{"2", "Bob"},
			// Charlie is missing (deleted)
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data2),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data2[i].id, data2[i].name})
			},
		)
		require.NoError(t, err)

		// Verify Charlie is removed from current
		var charlieCount int
		err = conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM test_scd2_delete_current WHERE id = '3'").Scan(&charlieCount)
		require.NoError(t, err)
		require.Equal(t, 0, charlieCount)

		// Verify the previous open version was closed (valid_to should be set to snapshotTS2)
		// First, check all history rows for id='3' to debug
		rows, err := conn.QueryContext(context.Background(),
			"SELECT valid_from, valid_to, op, name FROM test_scd2_delete_history WHERE id = '3' ORDER BY valid_from")
		require.NoError(t, err)
		defer rows.Close()

		var foundInsert bool
		for rows.Next() {
			var vf, vt interface{}
			var op, name string
			err := rows.Scan(&vf, &vt, &op, &name)
			require.NoError(t, err)
			if op == "I" {
				foundInsert = true
				require.NotNil(t, vt, "insert version should have valid_to set after delete (got NULL)")
				vtTime, ok := vt.(time.Time)
				require.True(t, ok, "valid_to should be a time.Time")
				require.Equal(t, snapshotTS2, vtTime, "insert version should be closed with valid_to = deletion snapshot timestamp")
			}
		}
		require.True(t, foundInsert, "should find the insert version for deleted row")

		// Verify delete tombstone in history
		var deleteCount int
		err = conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM test_scd2_delete_history WHERE id = '3' AND op = 'D'").Scan(&deleteCount)
		require.NoError(t, err)
		require.Equal(t, 1, deleteCount, "should have one delete tombstone for id '3'")

		var deleteOp string
		var deleteValidFrom time.Time
		var deleteValidTo interface{}
		var deleteName string
		err = conn.QueryRowContext(context.Background(),
			"SELECT op, valid_from, valid_to, name FROM test_scd2_delete_history WHERE id = '3' AND op = 'D'").Scan(&deleteOp, &deleteValidFrom, &deleteValidTo, &deleteName)
		require.NoError(t, err)
		require.Equal(t, "D", deleteOp)
		require.Equal(t, snapshotTS2, deleteValidFrom)
		require.Nil(t, deleteValidTo, "delete tombstone should have valid_to = NULL (open)")
		require.Equal(t, "Charlie", deleteName, "delete tombstone should have correct payload from closed version")
	})

	t.Run("sets valid_to correctly when deleting updated row", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
		snapshotTS3 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_delete_updated",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR", "status:VARCHAR"},
			MissingMeansDeleted: true,
			TrackIngestRuns:     false,
		}

		// First snapshot: insert
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice", "active"})
			},
		)
		require.NoError(t, err)

		// Second snapshot: update
		cfg.SnapshotTS = snapshotTS2
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice Updated", "inactive"})
			},
		)
		require.NoError(t, err)

		// Verify update closed the first version
		var firstValidTo interface{}
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_delete_updated_history WHERE id = '1' AND valid_from = ?", snapshotTS1).Scan(&firstValidTo)
		require.NoError(t, err)
		require.NotNil(t, firstValidTo, "first version should have valid_to set after update")
		require.Equal(t, snapshotTS2, firstValidTo)

		// Third snapshot: delete
		cfg.SnapshotTS = snapshotTS3
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify the updated version (second snapshot) was closed with valid_to = snapshotTS3
		var secondValidTo interface{}
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_delete_updated_history WHERE id = '1' AND valid_from = ?", snapshotTS2).Scan(&secondValidTo)
		require.NoError(t, err)
		require.NotNil(t, secondValidTo, "updated version should have valid_to set")
		require.Equal(t, snapshotTS3, secondValidTo, "updated version should be closed with valid_to = deletion snapshot timestamp")

		// Verify delete tombstone has correct payload from the closed updated version
		var deleteName, deleteStatus string
		var deleteValidFrom time.Time
		var deleteValidTo interface{}
		err = conn.QueryRowContext(context.Background(),
			"SELECT name, status, valid_from, valid_to FROM test_scd2_delete_updated_history WHERE id = '1' AND op = 'D'").Scan(&deleteName, &deleteStatus, &deleteValidFrom, &deleteValidTo)
		require.NoError(t, err)
		require.Equal(t, "Alice Updated", deleteName, "delete tombstone should have name from updated version")
		require.Equal(t, "inactive", deleteStatus, "delete tombstone should have status from updated version")
		require.Equal(t, snapshotTS3, deleteValidFrom)
		require.Nil(t, deleteValidTo, "delete tombstone should have valid_to = NULL")
	})

	t.Run("tracks ingest runs when enabled", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		runID := "test_run_123"

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_runs",
			SnapshotTS:          snapshotTS,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     true,
			RunID:               runID,
		}

		data := []struct {
			id   string
			name string
		}{
			{"1", "Alice"},
			{"2", "Bob"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data[i].id, data[i].name})
			},
		)
		require.NoError(t, err)

		// Verify ingest run was recorded
		var recordedRunID string
		var recordedSnapshotTS time.Time
		var rowsInSnapshot, inserts, updates, deletes int
		err = conn.QueryRowContext(context.Background(),
			"SELECT run_id, snapshot_ts, rows_in_snapshot, inserts, updates, deletes FROM test_scd2_runs_ingest_runs WHERE run_id = ?",
			runID).Scan(&recordedRunID, &recordedSnapshotTS, &rowsInSnapshot, &inserts, &updates, &deletes)
		require.NoError(t, err)
		require.Equal(t, runID, recordedRunID)
		require.Equal(t, snapshotTS, recordedSnapshotTS)
		require.Equal(t, 2, rowsInSnapshot)
		require.Equal(t, 2, inserts)
		require.Equal(t, 0, updates)
		require.Equal(t, 0, deletes)
	})

	t.Run("handles empty snapshot when MissingMeansDeleted is true", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_empty",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: true,
			TrackIngestRuns:     false,
		}

		// First snapshot with data
		data1 := []struct {
			id   string
			name string
		}{
			{"1", "Alice"},
			{"2", "Bob"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data1),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data1[i].id, data1[i].name})
			},
		)
		require.NoError(t, err)

		// Empty snapshot
		cfg.SnapshotTS = snapshotTS2
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify all rows deleted from current
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_scd2_empty_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)

		// Verify delete tombstones in history
		var deleteCount int
		err = conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM test_scd2_empty_history WHERE op = 'D' AND valid_from = ?", snapshotTS2).Scan(&deleteCount)
		require.NoError(t, err)
		require.Equal(t, 2, deleteCount)
	})

	t.Run("handles empty snapshot when MissingMeansDeleted is false", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_empty_no_delete",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false, // Don't delete on empty
			TrackIngestRuns:     false,
		}

		// First snapshot with data
		data1 := []struct {
			id   string
			name string
		}{
			{"1", "Alice"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data1),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data1[i].id, data1[i].name})
			},
		)
		require.NoError(t, err)

		// Empty snapshot (should be no-op)
		cfg.SnapshotTS = snapshotTS2
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			0,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.NoError(t, err)

		// Verify row still exists in current
		var count int
		err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM test_scd2_empty_no_delete_current").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})

	t.Run("computes row hash correctly", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_hash",
			SnapshotTS:          snapshotTS,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR", "age:INTEGER"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		data := []struct {
			id   string
			name string
			age  string
		}{
			{"1", "Alice", "30"},
			{"2", "Bob", "25"},
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			len(data),
			func(w *csv.Writer, i int) error {
				return w.Write([]string{data[i].id, data[i].name, data[i].age})
			},
		)
		require.NoError(t, err)

		// Get hashes for both rows
		var hash1, hash2 string
		err = conn.QueryRowContext(context.Background(),
			"SELECT row_hash FROM test_scd2_hash_current WHERE id = '1'").Scan(&hash1)
		require.NoError(t, err)
		err = conn.QueryRowContext(context.Background(),
			"SELECT row_hash FROM test_scd2_hash_current WHERE id = '2'").Scan(&hash2)
		require.NoError(t, err)

		// Hashes should be different (different payloads)
		require.NotEqual(t, hash1, hash2)
		require.NotEmpty(t, hash1)
		require.NotEmpty(t, hash2)

		// Verify hash is stored in history too
		var historyHash string
		err = conn.QueryRowContext(context.Background(),
			"SELECT row_hash FROM test_scd2_hash_history WHERE id = '1'").Scan(&historyHash)
		require.NoError(t, err)
		require.Equal(t, hash1, historyHash)
	})

	t.Run("handles multiple snapshots with SCD2 history", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_multiple",
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		// Snapshot 1: Insert
		cfg.SnapshotTS = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice"})
			},
		)
		require.NoError(t, err)

		// Snapshot 2: Update
		cfg.SnapshotTS = time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice Updated"})
			},
		)
		require.NoError(t, err)

		// Snapshot 3: Update again
		cfg.SnapshotTS = time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice Updated Again"})
			},
		)
		require.NoError(t, err)

		// Verify history has 3 versions
		var historyCount int
		err = conn.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM test_scd2_multiple_history WHERE id = '1'").Scan(&historyCount)
		require.NoError(t, err)
		require.Equal(t, 3, historyCount)

		// Verify validity windows
		var validFrom1, validTo1, validFrom2, validTo2, validFrom3, validTo3 interface{}
		rows, err := conn.QueryContext(context.Background(),
			"SELECT valid_from, valid_to FROM test_scd2_multiple_history WHERE id = '1' ORDER BY valid_from")
		require.NoError(t, err)
		defer rows.Close()

		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&validFrom1, &validTo1))
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&validFrom2, &validTo2))
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&validFrom3, &validTo3))
		require.False(t, rows.Next())

		// First version should be closed at second snapshot
		require.NotNil(t, validTo1)
		// Second version should be closed at third snapshot
		require.NotNil(t, validTo2)
		// Third version should be open
		require.Nil(t, validTo3)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_cancel",
			SnapshotTS:          time.Now(),
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err = SCDTableViaCSV(
			ctx,
			log,
			conn,
			cfg,
			5,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		// Error message can be "context cancelled" or "context canceled" depending on Go version
		require.True(t, strings.Contains(err.Error(), "context cancelled") ||
			strings.Contains(err.Error(), "context canceled") ||
			strings.Contains(err.Error(), "context canceled"))
	})

	t.Run("handles CSV write error", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_error",
			SnapshotTS:          time.Now(),
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			2,
			func(w *csv.Writer, i int) error {
				if i == 1 {
					return errors.New("write error")
				}
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to write CSV record")
	})

	t.Run("handles database transaction error", func(t *testing.T) {
		t.Parallel()

		conn := &failingDBConn{}
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_db_error",
			SnapshotTS:          time.Now(),
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err := SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		// Error could be from table creation or transaction begin
		require.True(t, strings.Contains(err.Error(), "failed to begin transaction") ||
			strings.Contains(err.Error(), "failed to create SCD2 tables"))
	})

	t.Run("validates primary key columns not empty", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_validate",
			SnapshotTS:          time.Now(),
			PrimaryKeyColumns:   []string{}, // Empty
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "test"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "primary key columns cannot be empty")
	})

	t.Run("validates payload columns not empty", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_validate2",
			SnapshotTS:          time.Now(),
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{}, // Empty
			MissingMeansDeleted: false,
			TrackIngestRuns:     false,
		}

		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1"})
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "payload columns cannot be empty")
	})

	t.Run("backfill valid_to on deletes fixes missing valid_to", func(t *testing.T) {
		t.Parallel()

		db, conn, err := testDBWithConn(t)
		require.NoError(t, err)
		defer db.Close()

		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		snapshotTS1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		snapshotTS2 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

		cfg := SCDTableConfig{
			TableBaseName:       "test_scd2_backfill",
			SnapshotTS:          snapshotTS1,
			PrimaryKeyColumns:   []string{"id:VARCHAR"},
			PayloadColumns:      []string{"name:VARCHAR"},
			MissingMeansDeleted: true,
			TrackIngestRuns:     false,
		}

		// First snapshot: insert a row
		err = SCDTableViaCSV(
			context.Background(),
			log,
			conn,
			cfg,
			1,
			func(w *csv.Writer, i int) error {
				return w.Write([]string{"1", "Alice"})
			},
		)
		require.NoError(t, err)

		// Manually insert a delete tombstone without closing the previous version
		// This simulates the bug where valid_to wasn't set
		_, err = conn.ExecContext(context.Background(), `
			INSERT INTO test_scd2_backfill_history (id, name, valid_from, valid_to, row_hash, op, run_id)
			SELECT id, name, ? AS valid_from, NULL AS valid_to, row_hash, 'D' AS op, 'manual_delete' AS run_id
			FROM test_scd2_backfill_history
			WHERE id = '1' AND op = 'I'
		`, snapshotTS2)
		require.NoError(t, err)

		// Verify the insert version still has valid_to = NULL (the bug)
		var validTo interface{}
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_backfill_history WHERE id = '1' AND op = 'I'").Scan(&validTo)
		require.NoError(t, err)
		require.Nil(t, validTo, "insert version should still have NULL valid_to before backfill")

		// Run backfill in dry-run mode first
		fixedCount, err := BackfillValidToOnDeletes(context.Background(), log, conn, cfg, true)
		require.NoError(t, err)
		require.Equal(t, 1, fixedCount, "should find 1 row to fix in dry run")

		// Run actual backfill
		fixedCount, err = BackfillValidToOnDeletes(context.Background(), log, conn, cfg, false)
		require.NoError(t, err)
		require.Equal(t, 1, fixedCount, "should fix 1 row")

		// Verify the insert version now has valid_to set to the delete timestamp
		err = conn.QueryRowContext(context.Background(),
			"SELECT valid_to FROM test_scd2_backfill_history WHERE id = '1' AND op = 'I'").Scan(&validTo)
		require.NoError(t, err)
		require.NotNil(t, validTo, "insert version should have valid_to set after backfill")
		require.Equal(t, snapshotTS2, validTo, "valid_to should be set to delete tombstone's valid_from")
	})
}
