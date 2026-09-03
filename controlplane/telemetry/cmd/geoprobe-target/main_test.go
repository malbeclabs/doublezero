package main

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
)

func newTestCaches() *geoprobe.MinCacheMap[[32]byte, geoprobe.LocationOffset] {
	return geoprobe.NewMinCacheMap[[32]byte, geoprobe.LocationOffset](time.Hour, func(o geoprobe.LocationOffset) uint64 {
		return o.RttNs
	})
}

func newTestOffset() *geoprobe.LocationOffset {
	return &geoprobe.LocationOffset{
		Version:         geoprobe.LocationOffsetVersion,
		MeasurementSlot: 12345,
		MeasuredRttNs:   1_000_000,
		Lat:             52.3676,
		Lng:             4.9041,
		RttNs:           1_000_000,
		TargetIP:        geoprobe.IPToTargetIP("198.51.100.1"),
		References:      []geoprobe.LocationOffset{},
	}
}

// Anyone can send a datagram to the geoprobe-target UDP port, so an offset that
// fails signature verification is an attacker-chosen location claim. It must
// reach neither the cache nor ClickHouse, which the lake explorer publishes.
func TestHandleOffset_DropsUnsignedOffset(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	caches := newTestCaches()
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}

	// An entirely unsigned datagram, and one claiming a real geoprobe's pubkey
	// (public onchain) with a junk signature.
	unsigned := newTestOffset()
	impersonator := solana.NewWallet()
	spoofed := newTestOffset()
	copy(spoofed.AuthorityPubkey[:], impersonator.PublicKey().Bytes())
	copy(spoofed.SenderPubkey[:], impersonator.PublicKey().Bytes())
	spoofed.Signature[0] = 0xff

	for _, forged := range []*geoprobe.LocationOffset{unsigned, spoofed} {
		handleOffset(log, forged, addr, true, writer, caches)

		if got := writer.BufferedRows(); got != 0 {
			t.Errorf("expected forged offset to be dropped, got %d buffered clickhouse rows", got)
		}
		if _, ok := caches.Get(forged.SenderPubkey).Best(); ok {
			t.Error("expected forged offset to be dropped, but it entered the cache")
		}
	}
}

func TestHandleOffset_AcceptsSignedOffset(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	caches := newTestCaches()
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}

	probe := solana.NewWallet()
	authority := solana.NewWallet()
	signer, err := geoprobe.NewOffsetSigner(authority.PrivateKey, probe.PublicKey())
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	offset := newTestOffset()
	if err := signer.SignOffset(offset); err != nil {
		t.Fatalf("failed to sign offset: %v", err)
	}

	handleOffset(log, offset, addr, true, writer, caches)

	if got := writer.BufferedRows(); got != 1 {
		t.Errorf("expected 1 buffered clickhouse row for a signed offset, got %d", got)
	}
	best, ok := caches.Get(offset.SenderPubkey).Best()
	if !ok {
		t.Fatal("expected signed offset to be cached")
	}
	if best.RttNs != offset.RttNs {
		t.Errorf("expected cached RttNs=%d, got %d", offset.RttNs, best.RttNs)
	}
}
