package exporter_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type fakeClickHouseInserter struct {
	mu        sync.Mutex
	batches   [][]exporter.Record
	err       error
	delay     time.Duration
	active    int
	maxActive int
	closed    bool
}

func (f *fakeClickHouseInserter) Insert(ctx context.Context, records []exporter.Record) error {
	f.mu.Lock()
	if f.err != nil {
		err := f.err
		f.mu.Unlock()
		return err
	}
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	delay := f.delay
	f.mu.Unlock()

	time.Sleep(delay)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.active--
	f.batches = append(f.batches, append([]exporter.Record(nil), records...))
	return nil
}

func (f *fakeClickHouseInserter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeClickHouseInserter) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeClickHouseInserter) inserted() []exporter.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []exporter.Record
	for _, batch := range f.batches {
		out = append(out, batch...)
	}
	return out
}

func (f *fakeClickHouseInserter) batchSizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, 0, len(f.batches))
	for _, batch := range f.batches {
		out = append(out, len(batch))
	}
	return out
}

func (f *fakeClickHouseInserter) maxConcurrent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func (f *fakeClickHouseInserter) wasClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func cloudRecord(originRegion, targetRegion string, probeID uint32, ts time.Time, rtt time.Duration) exporter.Record {
	return exporter.Record{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: originRegion,
		TargetExchangeCode: targetRegion,
		Timestamp:          ts,
		RTT:                rtt,
		Cloud: &exporter.CloudInfo{
			SourceCloud:     "aws",
			TargetCloud:     "aws",
			ProbeID:         probeID,
			PacketsSent:     3,
			PacketsReceived: 3,
		},
	}
}

func TestInternetLatency_ClickHouseExporter_Validate(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tests := []struct {
		name              string
		cfg               exporter.ClickHouseExporterConfig
		expectErrContains string
	}{
		{
			name:              "no logger",
			cfg:               exporter.ClickHouseExporterConfig{Inserter: &fakeClickHouseInserter{}},
			expectErrContains: "logger is required",
		},
		{
			name:              "no address",
			cfg:               exporter.ClickHouseExporterConfig{Logger: log, Database: "telemetry"},
			expectErrContains: "address is required",
		},
		{
			name:              "no database",
			cfg:               exporter.ClickHouseExporterConfig{Logger: log, Addr: "localhost:9000"},
			expectErrContains: "database is required",
		},
		{
			name:              "an inserter replaces the connection settings",
			cfg:               exporter.ClickHouseExporterConfig{Logger: log, Inserter: &fakeClickHouseInserter{}},
			expectErrContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectErrContains != "" {
				require.ErrorContains(t, err, tt.expectErrContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInternetLatency_ClickHouseExporter_BatchedInsert(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	fake := &fakeClickHouseInserter{}

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		Table:              exporter.DefaultClickHouseTable,
		BatchSize:          2,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 10,
		Inserter:           fake,
	})
	require.NoError(t, err)

	ts := time.Unix(1757462400, 0).UTC()

	err = e.WriteRecords(t.Context(), []exporter.Record{
		cloudRecord("eu-west-1", "us-east-1", 1003385, ts, 70*time.Millisecond),
	})
	require.NoError(t, err)
	require.Equal(t, 1, e.Buffered())
	require.Empty(t, fake.inserted())

	err = e.WriteRecords(t.Context(), []exporter.Record{
		cloudRecord("eu-west-1", "us-east-1", 1003386, ts, 71*time.Millisecond),
	})
	require.NoError(t, err)
	require.Equal(t, 0, e.Buffered())

	inserted := fake.inserted()
	require.Len(t, inserted, 2)
	require.Equal(t, uint32(1003385), inserted[0].Cloud.ProbeID)
	require.Equal(t, uint32(1003386), inserted[1].Cloud.ProbeID)

	require.NoError(t, e.Close())
	require.True(t, fake.wasClosed())
}

func TestInternetLatency_ClickHouseExporter_SplitsOneWriteIntoBatches(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	fake := &fakeClickHouseInserter{}

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		BatchSize:          2,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 10,
		Inserter:           fake,
	})
	require.NoError(t, err)

	ts := time.Unix(1757462400, 0).UTC()
	records := make([]exporter.Record, 0, 5)
	for i := range 5 {
		records = append(records, cloudRecord("eu-west-1", "us-east-1", uint32(1000+i), ts, 70*time.Millisecond))
	}

	require.NoError(t, e.WriteRecords(t.Context(), records))
	require.Equal(t, []int{2, 2, 1}, fake.batchSizes())
	require.Len(t, fake.inserted(), 5)
	require.Equal(t, 0, e.Buffered())
	require.NoError(t, e.Close())
}

func TestInternetLatency_ClickHouseExporter_RejectsRecordWithoutCloudInfo(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	fake := &fakeClickHouseInserter{}

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		Table:              exporter.DefaultClickHouseTable,
		BatchSize:          1,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 10,
		Inserter:           fake,
	})
	require.NoError(t, err)

	err = e.WriteRecords(t.Context(), []exporter.Record{{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: "eu-west-1",
		TargetExchangeCode: "us-east-1",
		Timestamp:          time.Unix(1757462400, 0).UTC(),
		RTT:                70 * time.Millisecond,
	}})

	require.ErrorContains(t, err, "no cloud info")
	require.Equal(t, 0, e.Buffered())
	require.Empty(t, fake.inserted())
}

func TestInternetLatency_ClickHouseExporter_FlushesOneBatchAtATime(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	fake := &fakeClickHouseInserter{delay: 2 * time.Millisecond}

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		BatchSize:          1,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 100,
		Inserter:           fake,
	})
	require.NoError(t, err)

	ts := time.Unix(1757462400, 0).UTC()
	ctx := t.Context()
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 10 {
				if err := e.WriteRecords(ctx, []exporter.Record{
					cloudRecord("eu-west-1", "us-east-1", uint32(1000+w*10+i), ts, 70*time.Millisecond),
				}); err != nil {
					t.Errorf("write failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 1, fake.maxConcurrent(), "concurrent inserts can reorder the buffer on retry")
	require.Len(t, fake.inserted(), 40)
	require.NoError(t, e.Close())
}

func TestInternetLatency_ClickHouseExporter_DropsOldestWhenBufferFull(t *testing.T) {
	// Not parallel: this test reads package-level Prometheus counters as deltas.
	log := logger.With("test", t.Name())

	fake := &fakeClickHouseInserter{}
	fake.setErr(errors.New("clickhouse is down"))

	e, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:             log,
		Table:              exporter.DefaultClickHouseTable,
		BatchSize:          1,
		FlushInterval:      time.Hour,
		MaxBufferedRecords: 3,
		Inserter:           fake,
	})
	require.NoError(t, err)

	droppedBefore := testutil.ToFloat64(metrics.ExporterClickHouseRecordsDroppedTotal)
	errorsBefore := testutil.ToFloat64(metrics.ExporterClickHouseInsertErrorsTotal)
	writtenBefore := testutil.ToFloat64(metrics.ExporterClickHouseRecordsWrittenTotal)

	ts := time.Unix(1757462400, 0).UTC()
	for i := range 5 {
		err := e.WriteRecords(t.Context(), []exporter.Record{
			cloudRecord("eu-west-1", "us-east-1", uint32(1000+i), ts.Add(time.Duration(i)*time.Minute), 70*time.Millisecond),
		})
		if i < 2 {
			require.NoError(t, err, "a failed insert the buffer absorbs must not fail the write")
		} else {
			require.ErrorContains(t, err, "buffer is full at 3 records")
		}
	}

	require.Equal(t, 3, e.Buffered(), "the buffer must stay at its cap")
	require.Equal(t, float64(3), testutil.ToFloat64(metrics.ExporterClickHouseBufferedRecords))
	require.Equal(t, float64(2), testutil.ToFloat64(metrics.ExporterClickHouseRecordsDroppedTotal)-droppedBefore,
		"the two oldest records must be counted as dropped")
	require.Equal(t, float64(5), testutil.ToFloat64(metrics.ExporterClickHouseInsertErrorsTotal)-errorsBefore)

	fake.setErr(nil)
	require.NoError(t, e.Flush(t.Context()))

	inserted := fake.inserted()
	require.Len(t, inserted, 3)
	require.Equal(t, uint32(1002), inserted[0].Cloud.ProbeID)
	require.Equal(t, uint32(1003), inserted[1].Cloud.ProbeID)
	require.Equal(t, uint32(1004), inserted[2].Cloud.ProbeID)
	require.Equal(t, float64(3), testutil.ToFloat64(metrics.ExporterClickHouseRecordsWrittenTotal)-writtenBefore)

	require.NoError(t, e.Close())
}

func TestInternetLatency_ClickHouseExporter_FailsToConstructWhenClickHouseIsUnreachable(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	_, err := exporter.NewClickHouseExporter(exporter.ClickHouseExporterConfig{
		Logger:      log,
		Addr:        "127.0.0.1:1",
		Database:    "telemetry",
		Username:    "default",
		TLSDisabled: true,
		Table:       exporter.DefaultClickHouseTable,
	})
	require.ErrorContains(t, err, "clickhouse ping")
}
