package ripeatlas

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

const (
	cloudExportSampleProbe = 1003385
	cloudExportLossProbe   = 1003386
	cloudExportSampleTS    = 1757462400
	cloudExportLossTS      = 1757462460
)

func answeredResult(probeID int, unixSeconds int64, rttMillis float64) map[string]any {
	return map[string]any{
		"prb_id":    float64(probeID),
		"timestamp": float64(unixSeconds),
		"sent":      float64(3),
		"rcvd":      float64(3),
		"result": []any{
			map[string]any{"rtt": rttMillis},
			map[string]any{"rtt": rttMillis + 0.4},
			map[string]any{"rtt": rttMillis + 0.6},
		},
	}
}

// lostResult is what RIPE returns for a ping that got no reply: the counts disagree and no
// entry carries an rtt.
func lostResult(probeID int, unixSeconds int64) map[string]any {
	return map[string]any{
		"prb_id":    float64(probeID),
		"timestamp": float64(unixSeconds),
		"sent":      float64(3),
		"rcvd":      float64(0),
		"result": []any{
			map[string]any{"x": "*"},
			map[string]any{"x": "*"},
			map[string]any{"x": "*"},
		},
	}
}

func cloudExportCollector(t *testing.T, results []any) (*Collector, *recordingExporter, *MeasurementState) {
	t.Helper()

	e := &recordingExporter{}
	c := &Collector{
		client: &MockClient{
			GetMeasurementResultsIncrementalFunc: func(ctx context.Context, measurementID int, startTimestamp int64) ([]any, error) {
				return results, nil
			},
		},
		log:      logger.With("test", t.Name()),
		exporter: e,
	}

	measurementState := NewMeasurementState(filepath.Join(t.TempDir(), TimestampFileName))
	measurementState.SetMetadata(1, MeasurementMeta{
		TargetLocation: "us-east-1",
		Sources: []SourceProbeMeta{
			{LocationCode: "eu-west-1", ProbeID: cloudExportSampleProbe},
			{LocationCode: "eu-west-2", ProbeID: cloudExportLossProbe},
		},
		CreatedAt: time.Now().Unix(),
	})

	return c, e, measurementState
}

func TestInternetLatency_RIPEAtlas_CloudExport_TotalLossBecomesAZeroRTTRecord(t *testing.T) {
	t.Parallel()

	results := []any{
		answeredResult(cloudExportSampleProbe, cloudExportSampleTS, 70.5),
		lostResult(cloudExportLossProbe, cloudExportLossTS),
	}
	c, e, measurementState := cloudExportCollector(t, results)
	c.SetCloudExport(map[string]string{
		"us-east-1": "aws",
		"eu-west-1": "aws",
		"eu-west-2": "aws",
	})

	count, records, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, records, 2)
	require.Equal(t, records, e.written())

	sample := records[0]
	require.Equal(t, "us-east-1", sample.SourceExchangeCode)
	require.Equal(t, "eu-west-1", sample.TargetExchangeCode)
	require.Equal(t, 70500*time.Microsecond, sample.RTT)
	require.Equal(t, &exporter.CloudInfo{
		SourceCloud:     "aws",
		TargetCloud:     "aws",
		ProbeID:         cloudExportSampleProbe,
		PacketsSent:     3,
		PacketsReceived: 3,
	}, sample.Cloud)

	loss := records[1]
	require.Equal(t, "us-east-1", loss.SourceExchangeCode)
	require.Equal(t, "eu-west-2", loss.TargetExchangeCode)
	require.Equal(t, time.Duration(0), loss.RTT)
	require.True(t, loss.Timestamp.Equal(time.Unix(cloudExportLossTS, 0).UTC()))
	require.Equal(t, &exporter.CloudInfo{
		SourceCloud:     "aws",
		TargetCloud:     "aws",
		ProbeID:         cloudExportLossProbe,
		PacketsSent:     3,
		PacketsReceived: 0,
	}, loss.Cloud)

	cursor, ok := measurementState.GetLastTimestamp(1)
	require.True(t, ok)
	require.Equal(t, int64(cloudExportLossTS), cursor, "a loss row is a sample and advances the cursor")
}

func TestInternetLatency_RIPEAtlas_CloudExport_ExchangePathDropsTotalLoss(t *testing.T) {
	t.Parallel()

	results := []any{
		answeredResult(cloudExportSampleProbe, cloudExportSampleTS, 70.5),
		lostResult(cloudExportLossProbe, cloudExportLossTS),
	}
	c, e, measurementState := cloudExportCollector(t, results)

	count, records, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, records, 1)
	require.Equal(t, "eu-west-1", records[0].TargetExchangeCode)
	require.Nil(t, records[0].Cloud)
	require.Equal(t, records, e.written())

	cursor, ok := measurementState.GetLastTimestamp(1)
	require.True(t, ok)
	require.Equal(t, int64(cloudExportSampleTS), cursor, "the lost result must not advance the cursor")
}

func TestInternetLatency_RIPEAtlas_CloudExport_SkipsSampleFromLocationWithoutCloud(t *testing.T) {
	t.Parallel()

	results := []any{
		answeredResult(cloudExportSampleProbe, cloudExportSampleTS, 70.5),
		answeredResult(cloudExportLossProbe, cloudExportLossTS, 71.5),
	}
	c, e, measurementState := cloudExportCollector(t, results)
	// eu-west-2 is deliberately absent.
	c.SetCloudExport(map[string]string{
		"us-east-1": "aws",
		"eu-west-1": "aws",
	})

	count, records, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, records, 1)
	require.Equal(t, "eu-west-1", records[0].TargetExchangeCode)
	require.NotNil(t, records[0].Cloud)
	require.Equal(t, records, e.written())

	cursor, ok := measurementState.GetLastTimestamp(1)
	require.True(t, ok)
	require.Equal(t, int64(cloudExportLossTS), cursor, "a skipped sample still advances the cursor")
}

func TestInternetLatency_RIPEAtlas_CloudExport_ResultsThatAreNotLoss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result any
	}{
		{
			name:   "a probe the measurement does not list",
			result: lostResult(999999, cloudExportLossTS),
		},
		{
			name: "a result carrying no packets",
			result: map[string]any{
				"prb_id":    float64(cloudExportLossProbe),
				"timestamp": float64(cloudExportLossTS),
			},
		},
		{
			name: "a result carrying no timestamp",
			result: map[string]any{
				"prb_id": float64(cloudExportLossProbe),
				"sent":   float64(3),
				"rcvd":   float64(0),
				"result": []any{map[string]any{"x": "*"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, e, measurementState := cloudExportCollector(t, []any{tt.result})
			c.SetCloudExport(map[string]string{
				"us-east-1": "aws",
				"eu-west-1": "aws",
				"eu-west-2": "aws",
			})

			count, records, err := c.exportSingleMeasurementResults(t.Context(), Measurement{ID: 1}, measurementState)
			require.NoError(t, err)
			require.Equal(t, 0, count)
			require.Empty(t, records)
			require.Empty(t, e.written())

			_, ok := measurementState.GetLastTimestamp(1)
			require.False(t, ok)
		})
	}
}

func TestInternetLatency_RIPEAtlas_CloudExport_ParsePacketCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		result       any
		wantSent     int
		wantReceived int
	}{
		{
			name:         "reported counts are used",
			result:       map[string]any{"sent": float64(3), "rcvd": float64(1)},
			wantSent:     3,
			wantReceived: 1,
		},
		{
			name: "absent counts are taken from the ping entries",
			result: map[string]any{"result": []any{
				map[string]any{"rtt": float64(12.0)},
				map[string]any{"x": "*"},
			}},
			wantSent:     2,
			wantReceived: 1,
		},
		{
			name:         "a result that is not an object has no packets",
			result:       "nonsense",
			wantSent:     0,
			wantReceived: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sent, received := parsePacketCountsFromResult(tt.result)
			require.Equal(t, tt.wantSent, sent)
			require.Equal(t, tt.wantReceived, received)
		})
	}
}

func TestInternetLatency_RIPEAtlas_CloudExport_ClampPacketCount(t *testing.T) {
	t.Parallel()

	require.Equal(t, uint8(0), clampPacketCount(-1))
	require.Equal(t, uint8(3), clampPacketCount(3))
	require.Equal(t, uint8(255), clampPacketCount(255))
	require.Equal(t, uint8(255), clampPacketCount(256))
}
