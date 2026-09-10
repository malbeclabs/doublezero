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
		handleOffset(log, forged, addr, true, writer, caches, newSlotFloor(time.Hour))

		if got := len(writer.PendingRows()); got != 0 {
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

	handleOffset(log, offset, addr, true, writer, caches, newSlotFloor(time.Hour))

	if got := len(writer.PendingRows()); got != 1 {
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

// signedOffsetAt returns an offset stamped with slot and rttNs, signed by
// signer so it passes the chain check.
func signedOffsetAt(t *testing.T, signer *geoprobe.OffsetSigner, slot, rttNs uint64) *geoprobe.LocationOffset {
	t.Helper()
	offset := newTestOffset()
	offset.MeasurementSlot = slot
	offset.RttNs = rttNs
	if err := signer.SignOffset(offset); err != nil {
		t.Fatalf("failed to sign offset: %v", err)
	}
	return offset
}

func newTestSigner(t *testing.T) *geoprobe.OffsetSigner {
	t.Helper()
	signer, err := geoprobe.NewOffsetSigner(solana.NewWallet().PrivateKey, solana.NewWallet().PublicKey())
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	return signer
}

// A signature stays valid forever, so an offset captured off the wire replays
// cleanly through the signature gate. The slot floor is what stops it reaching
// location_offsets.
func TestHandleOffset_RejectsReplayedOffset(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	caches := newTestCaches()
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}
	floor := newSlotFloor(time.Hour)
	signer := newTestSigner(t)

	const currentSlot = 1_000_000
	live := signedOffsetAt(t, signer, currentSlot, 5_000_000)
	handleOffset(log, live, addr, true, writer, caches, floor)
	if got := len(writer.PendingRows()); got != 1 {
		t.Fatalf("expected the live offset to be recorded, got %d rows", got)
	}

	// A capture from well before the floor, with a lower RTT so it would win
	// the cache if it were accepted.
	replay := signedOffsetAt(t, signer, currentSlot-maxSlotRegression-1, 1_000_000)
	handleOffset(log, replay, addr, true, writer, caches, floor)

	if got := len(writer.PendingRows()); got != 1 {
		t.Errorf("expected the replay to be dropped, got %d buffered rows", got)
	}
	best, ok := caches.Get(replay.SenderPubkey).Best()
	if !ok {
		t.Fatal("expected the live offset to remain cached")
	}
	if best.RttNs != live.RttNs {
		t.Errorf("replay entered the cache: best RttNs=%d, want %d", best.RttNs, live.RttNs)
	}

	// Positive control: a fresh slot over the same path is still accepted.
	handleOffset(log, signedOffsetAt(t, signer, currentSlot+1, 4_000_000), addr, true, writer, caches, floor)
	if got := len(writer.PendingRows()); got != 2 {
		t.Errorf("expected a fresh-slot offset to be recorded, got %d rows", got)
	}
}

// Offsets are sent every 30s but stamped from a 5m slot cache, so most carry a
// slot the sender has already used. Rejecting repeats would drop nine of every
// ten legitimate offsets.
func TestSlotFloor_AcceptsRepeatedSlotWhileFresh(t *testing.T) {
	floor := newSlotFloor(time.Hour)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	sender := [32]byte{1}

	for i := 0; i < 10; i++ {
		if ok, reason, _, _ := floor.accept(sender, 1_000_000); !ok {
			t.Fatalf("repeat %d rejected while floor is fresh: %s", i, reason)
		}
		now = now.Add(30 * time.Second)
	}

	// A slot inside the regression allowance is also accepted.
	if ok, reason, _, _ := floor.accept(sender, 1_000_000-maxSlotRegression); !ok {
		t.Fatalf("slot inside the regression allowance rejected: %s", reason)
	}
}

// Once a sender stops advancing its slot, repeats no longer evidence a live
// sender: without this bound a capture taken while the probe was alive stays
// acceptable forever after it goes quiet.
func TestSlotFloor_RejectsStalledFloor(t *testing.T) {
	floor := newSlotFloor(time.Hour)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	sender := [32]byte{1}

	floor.accept(sender, 1_000_000)
	now = now.Add(maxFloorStall + time.Minute)

	ok, reason, floorSlot, floorAge := floor.accept(sender, 1_000_000)
	if ok {
		t.Fatal("expected a repeat at the frozen slot to be rejected once the floor stalled")
	}
	if reason != rejectFloorStalled {
		t.Errorf("reason = %q, want %q", reason, rejectFloorStalled)
	}
	if floorSlot != 1_000_000 || floorAge < maxFloorStall {
		t.Errorf("floor state = (slot %d, age %s), want slot 1000000 and age > %s", floorSlot, floorAge, maxFloorStall)
	}

	// Positive control: a strictly higher slot proves liveness and unfreezes it.
	if ok, reason, _, _ := floor.accept(sender, 1_000_001); !ok {
		t.Fatalf("expected a higher slot to be accepted after a stall, got %s", reason)
	}
}

func TestSlotFloor_IsPerSender(t *testing.T) {
	floor := newSlotFloor(time.Hour)
	fast := [32]byte{1}
	slow := [32]byte{2}

	floor.accept(fast, 5_000_000)

	if ok, reason, _, _ := floor.accept(slow, 1_000); !ok {
		t.Fatalf("a second sender was judged against the first sender's floor: %s", reason)
	}
}

// With verification disabled nothing is checked, so a row asserting
// signature_valid=true would be a mislabel in the table the public explorer
// reads.
func TestHandleOffset_UnverifiedRowIsNotLabelledValid(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	caches := newTestCaches()
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}

	handleOffset(log, newTestOffset(), addr, false, writer, caches, newSlotFloor(time.Hour))

	rows := writer.PendingRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 buffered row with verification disabled, got %d", len(rows))
	}
	if rows[0].SignatureValid {
		t.Error("row asserts signature_valid=true though no verification ran")
	}
	if rows[0].SignatureError != signatureUnverifiedMarker {
		t.Errorf("signature_error = %q, want %q", rows[0].SignatureError, signatureUnverifiedMarker)
	}

	// Positive control: a verified offset is still labelled valid.
	caches2 := newTestCaches()
	writer2 := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	handleOffset(log, signedOffsetAt(t, newTestSigner(t), 12345, 1_000_000), addr, true, writer2, caches2, newSlotFloor(time.Hour))
	rows2 := writer2.PendingRows()
	if len(rows2) != 1 || !rows2[0].SignatureValid || rows2[0].SignatureError != "" {
		t.Errorf("expected a verified offset to be labelled valid with no error, got %+v", rows2)
	}
}
