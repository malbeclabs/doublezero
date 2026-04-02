package geoprobe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOffsetRowFromLocationOffset(t *testing.T) {
	dzdPubkey := [32]byte{1, 2, 3}
	dzdSender := [32]byte{4, 5, 6}
	probePubkey := [32]byte{10, 11, 12}
	probeSender := [32]byte{13, 14, 15}

	offset := &LocationOffset{
		Signature:       [64]byte{0xff},
		Version:         LocationOffsetVersion,
		AuthorityPubkey: probePubkey,
		SenderPubkey:    probeSender,
		MeasurementSlot: 42,
		MeasuredRttNs:   500_000,
		Lat:             52.3676,
		Lng:             4.9041,
		RttNs:           1_500_000,
		TargetIP:        [4]byte{10, 0, 0, 1},
		NumReferences:   1,
		References: []LocationOffset{
			{
				Signature:       [64]byte{0xaa},
				Version:         LocationOffsetVersion,
				AuthorityPubkey: dzdPubkey,
				SenderPubkey:    dzdSender,
				MeasurementSlot: 41,
				MeasuredRttNs:   300_000,
				Lat:             52.3676,
				Lng:             4.9041,
				RttNs:           1_000_000,
				TargetIP:        [4]byte{10, 0, 0, 2},
				NumReferences:   0,
			},
		},
	}

	rawBytes, err := offset.Marshal()
	require.NoError(t, err)

	row := OffsetRowFromLocationOffset(offset, "192.168.1.1:8923", true, "", rawBytes)

	require.Equal(t, "192.168.1.1:8923", row.SourceAddr)
	require.True(t, row.SignatureValid)
	require.Empty(t, row.SignatureError)
	require.Equal(t, uint8(1), row.NumReferences)
	require.Len(t, row.RefAuthorityPubkeys, 1)
	require.Len(t, row.RefSenderPubkeys, 1)
	require.Len(t, row.RefMeasuredRttNs, 1)
	require.Len(t, row.RefRttNs, 1)
	require.Equal(t, uint64(300_000), row.RefMeasuredRttNs[0])
	require.Equal(t, uint64(1_000_000), row.RefRttNs[0])
	require.NotEmpty(t, row.RawOffset)
	require.WithinDuration(t, time.Now(), row.ReceivedAt, 2*time.Second)
}

func TestClickhouseWriterRecordBuffers(t *testing.T) {
	w := &ClickhouseWriter{
		buf: make([]OffsetRow, 0),
	}
	w.Record(OffsetRow{SourceAddr: "a"})
	w.Record(OffsetRow{SourceAddr: "b"})

	w.mu.Lock()
	require.Len(t, w.buf, 2)
	w.mu.Unlock()
}
