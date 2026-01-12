//go:build linux

package state

import (
	"encoding/json"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestTelemetry_StateCollector_BGP_DecodeTCPInfoBytes(t *testing.T) {
	t.Parallel()

	t.Run("valid TCPInfo", func(t *testing.T) {
		t.Parallel()

		ti := unix.TCPInfo{
			Rtt:      10000, // 10ms in microseconds
			Rttvar:   5000,  // 5ms in microseconds
			Snd_cwnd: 100,
		}

		attrData := make([]byte, unsafe.Sizeof(ti))
		dst := unsafe.Slice((*byte)(unsafe.Pointer(&ti)), len(attrData))
		copy(attrData, dst)

		dec := decodeTCPInfoBytes(attrData)
		require.True(t, dec.ok)
		require.Equal(t, ti.Rtt, dec.ti.Rtt)
		require.Equal(t, ti.Rttvar, dec.ti.Rttvar)
		require.Equal(t, ti.Snd_cwnd, dec.ti.Snd_cwnd)
	})

	t.Run("insufficient data", func(t *testing.T) {
		t.Parallel()

		// decodeTCPInfoBytes copies what it can, so it returns ok: true
		// but tiLen will be the size of the copied data
		dec := decodeTCPInfoBytes([]byte{1, 2, 3}) // Too small
		require.True(t, dec.ok)
		require.Equal(t, 3, dec.tiLen)
	})

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()

		dec := decodeTCPInfoBytes([]byte{})
		require.False(t, dec.ok)
	})
}

func TestTelemetry_StateCollector_BGP_RTTConversion(t *testing.T) {
	t.Parallel()

	t.Run("converts microseconds to milliseconds", func(t *testing.T) {
		t.Parallel()

		// 10ms = 10000 microseconds
		rttUs := uint32(10000)
		rttMs := float64(rttUs) / 1000.0
		require.Equal(t, 10.0, rttMs)

		// 5ms = 5000 microseconds
		rttvarUs := uint32(5000)
		rttvarMs := float64(rttvarUs) / 1000.0
		require.Equal(t, 5.0, rttvarMs)
	})
}

func TestTelemetry_StateCollector_BGP_ApplyTCPInfo_SetsRTTAndMSSAndBytes_WhenPresent(t *testing.T) {
	var ti unix.TCPInfo
	ti.Rtt = 1903
	ti.Rttvar = 3619
	ti.Snd_cwnd = 10
	ti.Snd_mss = 1436
	ti.Bytes_sent = 763776
	ti.Bytes_received = 650195
	ti.Rto = 204000
	ti.Ato = 40000
	ti.Pacing_rate = 15086014
	ti.Delivery_rate = 25192982

	row := BGPSocketState{}
	applyTCPInfo(&row, &ti, int(unsafe.Sizeof(ti)))

	if row.RTTms == nil || *row.RTTms != 1.903 {
		t.Fatalf("rtt_ms got %#v", row.RTTms)
	}
	if row.SndMSS == nil || *row.SndMSS != 1436 {
		t.Fatalf("snd_mss got %#v", row.SndMSS)
	}
	if row.BytesSent == nil || *row.BytesSent != 763776 {
		t.Fatalf("bytes_sent got %#v", row.BytesSent)
	}
	if row.RTOms == nil || *row.RTOms != 204 {
		t.Fatalf("rto_ms got %#v", row.RTOms)
	}
	if row.ATOms == nil || *row.ATOms != 40 {
		t.Fatalf("ato_ms got %#v", row.ATOms)
	}
	if row.PacingRateMbps == nil || *row.PacingRateMbps <= 0 {
		t.Fatalf("pacing_rate_Mbps got %#v", row.PacingRateMbps)
	}
	if row.SendRateMbps == nil || *row.SendRateMbps <= 0 {
		t.Fatalf("send_Mbps got %#v", row.SendRateMbps)
	}
}

func TestTelemetry_StateCollector_BGP_ApplyTCPInfo_TruncatedBlob_DoesNotSetLaterFields(t *testing.T) {
	var ti unix.TCPInfo
	ti.Rtt = 1903
	ti.Rttvar = 3619
	ti.Snd_cwnd = 10
	ti.Snd_mss = 1436
	ti.Bytes_sent = 123

	row := BGPSocketState{}

	// Allow RTT fields but truncate before Bytes_sent.
	// Using offsets makes this stable across x/sys updates.
	var tmp unix.TCPInfo
	cut := int(unsafe.Offsetof(tmp.Bytes_sent))
	applyTCPInfo(&row, &ti, cut)

	if row.RTTms == nil {
		t.Fatalf("expected RTTms to be set")
	}
	if row.BytesSent != nil {
		t.Fatalf("expected BytesSent to be nil on truncation, got %v", *row.BytesSent)
	}
}

func TestTelemetry_StateCollector_BGP_DecodeTCPInfoBytes_CopiesPrefixSafely(t *testing.T) {
	var ti unix.TCPInfo
	ti.Rtt = 999

	sz := int(unsafe.Sizeof(ti))
	buf := make([]byte, sz)
	src := unsafe.Slice((*byte)(unsafe.Pointer(&ti)), sz)
	copy(buf, src)

	dec := decodeTCPInfoBytes(buf[:16]) // tiny prefix
	if !dec.ok || dec.tiLen != 16 {
		t.Fatalf("dec ok=%v len=%d", dec.ok, dec.tiLen)
	}
}

func TestTelemetry_StateCollector_BGP_BGPSocketState_JSON(t *testing.T) {
	t.Parallel()

	t.Run("with all fields", func(t *testing.T) {
		t.Parallel()

		rtt := 10.5
		rttvar := 5.2
		cwnd := uint32(100)
		rto := uint32(204)
		ato := uint32(40)
		sndMSS := uint32(1424)
		bytesSent := uint64(20098709)
		bytesReceived := uint64(16935798)
		pacingRate := uint64(27500000)
		deliveryRate := uint64(34600000)
		pacingRateMbps := 220.0
		deliveryRateMbps := 276.8
		sendRateMbps := 113.92
		congestionControl := "cubic"
		tcpInfoPresent := true
		tcpInfoLen := 256

		state := BGPSocketState{
			Family:            "inet",
			State:             "ESTABLISHED",
			LocalIP:           "127.0.0.1",
			LocalPort:         179,
			RemoteIP:          "192.168.1.1",
			RemotePort:        54321,
			TCPInfoPresent:    tcpInfoPresent,
			TCPInfoLen:        tcpInfoLen,
			CongestionControl: &congestionControl,
			RTTms:             &rtt,
			RTTVarms:          &rttvar,
			Cwnd:              &cwnd,
			RTOms:             &rto,
			ATOms:             &ato,
			SndMSS:            &sndMSS,
			BytesSent:         &bytesSent,
			BytesReceived:     &bytesReceived,
			PacingRate:        &pacingRate,
			DeliveryRate:      &deliveryRate,
			PacingRateMbps:    &pacingRateMbps,
			DeliveryRateMbps:  &deliveryRateMbps,
			SendRateMbps:      &sendRateMbps,
		}

		data, err := json.Marshal(state)
		require.NoError(t, err)

		var unmarshaled BGPSocketState
		require.NoError(t, json.Unmarshal(data, &unmarshaled))

		require.Equal(t, state.Family, unmarshaled.Family)
		require.Equal(t, state.State, unmarshaled.State)
		require.Equal(t, state.LocalIP, unmarshaled.LocalIP)
		require.Equal(t, state.LocalPort, unmarshaled.LocalPort)
		require.Equal(t, state.RemoteIP, unmarshaled.RemoteIP)
		require.Equal(t, state.RemotePort, unmarshaled.RemotePort)
		require.Equal(t, state.TCPInfoPresent, unmarshaled.TCPInfoPresent)
		require.Equal(t, state.TCPInfoLen, unmarshaled.TCPInfoLen)
		require.NotNil(t, unmarshaled.RTTms)
		require.NotNil(t, unmarshaled.RTTVarms)
		require.NotNil(t, unmarshaled.Cwnd)
		require.NotNil(t, unmarshaled.RTOms)
		require.NotNil(t, unmarshaled.ATOms)
		require.NotNil(t, unmarshaled.SndMSS)
		require.NotNil(t, unmarshaled.BytesSent)
		require.NotNil(t, unmarshaled.BytesReceived)
		require.NotNil(t, unmarshaled.PacingRate)
		require.NotNil(t, unmarshaled.DeliveryRate)
		require.NotNil(t, unmarshaled.PacingRateMbps)
		require.NotNil(t, unmarshaled.DeliveryRateMbps)
		require.NotNil(t, unmarshaled.SendRateMbps)
		require.NotNil(t, unmarshaled.CongestionControl)

		require.Equal(t, rtt, *unmarshaled.RTTms)
		require.Equal(t, rttvar, *unmarshaled.RTTVarms)
		require.Equal(t, cwnd, *unmarshaled.Cwnd)
		require.Equal(t, rto, *unmarshaled.RTOms)
		require.Equal(t, ato, *unmarshaled.ATOms)
		require.Equal(t, sndMSS, *unmarshaled.SndMSS)
		require.Equal(t, bytesSent, *unmarshaled.BytesSent)
		require.Equal(t, bytesReceived, *unmarshaled.BytesReceived)
		require.Equal(t, pacingRate, *unmarshaled.PacingRate)
		require.Equal(t, deliveryRate, *unmarshaled.DeliveryRate)
		require.Equal(t, congestionControl, *unmarshaled.CongestionControl)
	})

	t.Run("without optional fields", func(t *testing.T) {
		t.Parallel()

		state := BGPSocketState{
			Family:     "inet6",
			State:      "ESTABLISHED",
			LocalIP:    "::1",
			LocalPort:  179,
			RemoteIP:   "2001:db8::1",
			RemotePort: 54321,
		}

		data, err := json.Marshal(state)
		require.NoError(t, err)

		// Verify optional fields are omitted
		require.NotContains(t, string(data), "rtt_ms")
		require.NotContains(t, string(data), "rttvar_ms")
		require.NotContains(t, string(data), "cwnd")
		require.NotContains(t, string(data), "bytes_sent")
		require.NotContains(t, string(data), "pacing_rate_Bps")

		var unmarshaled BGPSocketState
		require.NoError(t, json.Unmarshal(data, &unmarshaled))

		require.Equal(t, state.Family, unmarshaled.Family)
		require.Nil(t, unmarshaled.RTTms)
		require.Nil(t, unmarshaled.RTTVarms)
		require.Nil(t, unmarshaled.Cwnd)
		require.Nil(t, unmarshaled.BytesSent)
		require.Nil(t, unmarshaled.PacingRate)
	})
}
