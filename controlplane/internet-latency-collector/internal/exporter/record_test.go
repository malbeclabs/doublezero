package exporter_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Record_CloudInfoIsOptional(t *testing.T) {
	t.Parallel()

	record := exporter.Record{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: "LOC_A",
		TargetExchangeCode: "LOC_B",
		Timestamp:          time.Unix(1757462400, 0).UTC(),
		RTT:                26 * time.Millisecond,
	}

	require.Nil(t, record.Cloud)
	require.NoError(t, record.Validate())
	require.ErrorContains(t, record.Cloud.Validate(), "no cloud info")
}

func TestInternetLatency_Record_ValidateIgnoresCloudInfo(t *testing.T) {
	t.Parallel()

	record := exporter.Record{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: "LOC_A",
		TargetExchangeCode: "LOC_B",
		Timestamp:          time.Unix(1757462400, 0).UTC(),
		RTT:                26 * time.Millisecond,
		Cloud:              &exporter.CloudInfo{},
	}

	require.NoError(t, record.Validate())
	require.Error(t, record.Cloud.Validate())
}

func TestInternetLatency_Record_CloudInfoValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cloud             exporter.CloudInfo
		expectErrContains string
	}{
		{
			name: "complete cloud info",
			cloud: exporter.CloudInfo{
				SourceCloud:     "aws",
				TargetCloud:     "aws",
				ProbeID:         1003385,
				PacketsSent:     3,
				PacketsReceived: 3,
			},
			expectErrContains: "",
		},
		{
			name: "total loss validates",
			cloud: exporter.CloudInfo{
				SourceCloud:     "aws",
				TargetCloud:     "aws",
				ProbeID:         1003385,
				PacketsSent:     3,
				PacketsReceived: 0,
			},
			expectErrContains: "",
		},
		{
			name: "missing source cloud",
			cloud: exporter.CloudInfo{
				TargetCloud: "aws",
				ProbeID:     1003385,
			},
			expectErrContains: "no source cloud",
		},
		{
			name: "missing target cloud",
			cloud: exporter.CloudInfo{
				SourceCloud: "aws",
				ProbeID:     1003385,
			},
			expectErrContains: "no target cloud",
		},
		{
			name: "missing probe id",
			cloud: exporter.CloudInfo{
				SourceCloud: "aws",
				TargetCloud: "aws",
			},
			expectErrContains: "no probe id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cloud.Validate()
			if tt.expectErrContains != "" {
				require.ErrorContains(t, err, tt.expectErrContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInternetLatency_Record_CarriesCloudInfo(t *testing.T) {
	t.Parallel()

	record := exporter.Record{
		DataProvider:       exporter.DataProviderNameRIPEAtlas,
		SourceExchangeCode: "us-east-1",
		TargetExchangeCode: "eu-west-1",
		Timestamp:          time.Unix(1757462400, 0).UTC(),
		RTT:                70500 * time.Microsecond,
		Cloud: &exporter.CloudInfo{
			SourceCloud:     "aws",
			TargetCloud:     "aws",
			ProbeID:         1003385,
			PacketsSent:     3,
			PacketsReceived: 3,
		},
	}

	require.NoError(t, record.Validate())
	require.NoError(t, record.Cloud.Validate())
	require.Equal(t, uint32(1003385), record.Cloud.ProbeID)
	require.Equal(t, uint8(3), record.Cloud.PacketsSent)
	require.Equal(t, uint8(3), record.Cloud.PacketsReceived)
}
