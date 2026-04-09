package telemetry

import (
	"bytes"
	"testing"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_State_TimestampIndex(t *testing.T) {
	t.Run("round-trip deserialize with entries", func(t *testing.T) {
		pk := solana.NewWallet().PublicKey()
		header := TimestampIndexHeader{
			AccountType:      AccountTypeTimestampIndex,
			SamplesAccountPK: pk,
			NextEntryIndex:   3,
		}

		var buf bytes.Buffer
		enc := bin.NewBorshEncoder(&buf)
		require.NoError(t, enc.Encode(header))
		for _, e := range []TimestampIndexEntry{
			{SampleIndex: 0, TimestampMicroseconds: 1_000_000},
			{SampleIndex: 10, TimestampMicroseconds: 2_000_000},
			{SampleIndex: 20, TimestampMicroseconds: 3_000_000},
		} {
			require.NoError(t, enc.Encode(e))
		}

		var d TimestampIndex
		require.NoError(t, d.Deserialize(buf.Bytes()))

		require.Equal(t, AccountTypeTimestampIndex, d.AccountType)
		require.Equal(t, pk, d.SamplesAccountPK)
		require.Equal(t, uint32(3), d.NextEntryIndex)
		require.Len(t, d.Entries, 3)
		require.Equal(t, uint32(0), d.Entries[0].SampleIndex)
		require.Equal(t, uint64(1_000_000), d.Entries[0].TimestampMicroseconds)
		require.Equal(t, uint32(10), d.Entries[1].SampleIndex)
		require.Equal(t, uint64(2_000_000), d.Entries[1].TimestampMicroseconds)
	})

	t.Run("round-trip with empty entries", func(t *testing.T) {
		pk := solana.NewWallet().PublicKey()
		header := TimestampIndexHeader{
			AccountType:      AccountTypeTimestampIndex,
			SamplesAccountPK: pk,
			NextEntryIndex:   0,
		}

		var buf bytes.Buffer
		enc := bin.NewBorshEncoder(&buf)
		require.NoError(t, enc.Encode(header))

		var d TimestampIndex
		require.NoError(t, d.Deserialize(buf.Bytes()))

		require.Equal(t, uint32(0), d.NextEntryIndex)
		require.Empty(t, d.Entries)
	})

	t.Run("rejects truncated entry data", func(t *testing.T) {
		header := TimestampIndexHeader{
			AccountType:    AccountTypeTimestampIndex,
			NextEntryIndex: 5,
		}

		var buf bytes.Buffer
		enc := bin.NewBorshEncoder(&buf)
		require.NoError(t, enc.Encode(header))
		// Write only 1 entry's worth of data instead of 5
		require.NoError(t, enc.Encode(TimestampIndexEntry{SampleIndex: 0, TimestampMicroseconds: 1000}))

		var d TimestampIndex
		err := d.Deserialize(buf.Bytes())
		require.Error(t, err)
	})
}

func TestSDK_Telemetry_ReconstructTimestamp(t *testing.T) {
	interval := uint64(5_000_000) // 5s in µs
	entries := []TimestampIndexEntry{
		{SampleIndex: 0, TimestampMicroseconds: 1_700_000_000_000_000},
		{SampleIndex: 12, TimestampMicroseconds: 1_700_000_000_120_000},
		{SampleIndex: 24, TimestampMicroseconds: 1_700_000_000_240_000},
	}

	require.Equal(t, uint64(1_700_000_000_000_000), ReconstructTimestamp(entries, 0, 0, interval))
	require.Equal(t, uint64(1_700_000_000_000_000+5*5_000_000), ReconstructTimestamp(entries, 5, 0, interval))
	require.Equal(t, uint64(1_700_000_000_120_000), ReconstructTimestamp(entries, 12, 0, interval))
	require.Equal(t, uint64(1_700_000_000_120_000+3*5_000_000), ReconstructTimestamp(entries, 15, 0, interval))
	require.Equal(t, uint64(1_700_000_000_240_000+6*5_000_000), ReconstructTimestamp(entries, 30, 0, interval))
}

func TestSDK_Telemetry_ReconstructTimestamp_Fallback(t *testing.T) {
	ts := ReconstructTimestamp(nil, 10, 1_700_000_000_000_000, 5_000_000)
	require.Equal(t, uint64(1_700_000_000_000_000+10*5_000_000), ts)
}

func TestSDK_Telemetry_ReconstructTimestamp_LateStart(t *testing.T) {
	startTS := uint64(1_700_000_000_000_000)
	interval := uint64(5_000_000)
	entries := []TimestampIndexEntry{
		{SampleIndex: 120, TimestampMicroseconds: 1_700_000_000_800_000},
		{SampleIndex: 240, TimestampMicroseconds: 1_700_000_001_600_000},
	}

	require.Equal(t, startTS, ReconstructTimestamp(entries, 0, startTS, interval))
	require.Equal(t, startTS+50*interval, ReconstructTimestamp(entries, 50, startTS, interval))
	require.Equal(t, startTS+119*interval, ReconstructTimestamp(entries, 119, startTS, interval))
	require.Equal(t, uint64(1_700_000_000_800_000), ReconstructTimestamp(entries, 120, startTS, interval))
	require.Equal(t, uint64(1_700_000_000_800_000+5*interval), ReconstructTimestamp(entries, 125, startTS, interval))
	require.Equal(t, uint64(1_700_000_001_600_000), ReconstructTimestamp(entries, 240, startTS, interval))
}

func TestSDK_Telemetry_ReconstructTimestamps(t *testing.T) {
	entries := []TimestampIndexEntry{
		{SampleIndex: 0, TimestampMicroseconds: 1000},
		{SampleIndex: 3, TimestampMicroseconds: 5000},
	}
	ts := ReconstructTimestamps(5, entries, 0, 100)
	require.Equal(t, []uint64{1000, 1100, 1200, 5000, 5100}, ts)
}

func TestSDK_Telemetry_ReconstructTimestamps_LateStart(t *testing.T) {
	startTS := uint64(1000)
	interval := uint64(100)
	entries := []TimestampIndexEntry{
		{SampleIndex: 3, TimestampMicroseconds: 5000},
	}
	ts := ReconstructTimestamps(5, entries, startTS, interval)
	require.Equal(t, []uint64{1000, 1100, 1200, 5000, 5100}, ts)
}
