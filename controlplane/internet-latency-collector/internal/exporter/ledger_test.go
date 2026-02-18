package exporter_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

func TestBufferedLedgerExporter_WriteRecords(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	locA := serviceability.Exchange{Code: "LOC_A", PubKey: solana.NewWallet().PublicKey()}
	locB := serviceability.Exchange{Code: "LOC_B", PubKey: solana.NewWallet().PublicKey()}

	testCases := []struct {
		name               string
		records            []exporter.Record
		mockGetProgramData func(ctx context.Context) (*serviceability.ProgramData, error)
		expectErrContains  string
		expectEmptyBuffer  bool
	}{
		{
			name:    "no records",
			records: nil,
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Exchanges: []serviceability.Exchange{locA, locB}}, nil
			},
			expectErrContains: "",
			expectEmptyBuffer: true,
		},
		{
			name: "record with missing DataProvider",
			records: []exporter.Record{{
				Timestamp:          now,
				RTT:                10,
				DataProvider:       "",
				SourceExchangeCode: "LOC_A",
				TargetExchangeCode: "LOC_B",
			}},
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Exchanges: []serviceability.Exchange{locA, locB}}, nil
			},
			expectErrContains: "no data provider",
		},
		{
			name: "record with missing SourceExchangeCode",
			records: []exporter.Record{{
				Timestamp:          now,
				RTT:                10,
				DataProvider:       "DP",
				SourceExchangeCode: "",
				TargetExchangeCode: "LOC_B",
			}},
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Exchanges: []serviceability.Exchange{locA, locB}}, nil
			},
			expectErrContains: "no source exchange code",
		},
		{
			name: "record with unknown exchange code",
			records: []exporter.Record{{
				Timestamp:          now,
				RTT:                10,
				DataProvider:       "DP",
				SourceExchangeCode: "UNKNOWN",
				TargetExchangeCode: "LOC_B",
			}},
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Exchanges: []serviceability.Exchange{locA, locB}}, nil
			},
			expectErrContains: "",
			expectEmptyBuffer: true,
		},
		{
			name: "valid record gets buffered",
			records: []exporter.Record{{
				Timestamp:          now,
				RTT:                42,
				DataProvider:       "DP",
				SourceExchangeCode: "LOC_A",
				TargetExchangeCode: "LOC_B",
			}},
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{Exchanges: []serviceability.Exchange{locA, locB}}, nil
			},
			expectErrContains: "",
			expectEmptyBuffer: false,
		},
		{
			name: "serviceability error",
			records: []exporter.Record{{
				Timestamp:          now,
				RTT:                42,
				DataProvider:       "DP",
				SourceExchangeCode: "LOC_A",
				TargetExchangeCode: "LOC_B",
			}},
			mockGetProgramData: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return nil, errors.New("boom")
			},
			expectErrContains: "failed to get exchanges",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			serviceabilityMock := &mockServiceabilityProgramClient{
				GetProgramDataFunc: tc.mockGetProgramData,
			}
			telemetryMock := &mockTelemetryProgramClient{}
			exporterInstance, err := exporter.NewBufferedLedgerExporter(exporter.BufferedLedgerExporterConfig{
				Logger:         slog.Default(),
				Serviceability: serviceabilityMock,
				Telemetry:      telemetryMock,
				OracleAgentPK:  solana.NewWallet().PublicKey(),
				DataProviderSamplingIntervals: map[exporter.DataProviderName]time.Duration{
					exporter.DataProviderNameWheresitup: time.Second,
					exporter.DataProviderNameRIPEAtlas:  time.Second,
				},
				SubmissionInterval: time.Minute,
				MaxAttempts:        1,
				BackoffFunc:        func(int) time.Duration { return time.Millisecond },
				EpochFinder: &mockEpochFinder{
					ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
						return 0, nil
					},
				},
			})
			require.NoError(t, err)

			err = exporterInstance.WriteRecords(ctx, tc.records)

			if tc.expectErrContains != "" {
				require.ErrorContains(t, err, tc.expectErrContains)
			} else {
				require.NoError(t, err)
			}

			if tc.expectEmptyBuffer {
				require.Empty(t, exporterInstance.Buffer().FlushWithoutReset())
			} else if err == nil {
				require.NotEmpty(t, exporterInstance.Buffer().FlushWithoutReset())
			}
		})
	}
}
