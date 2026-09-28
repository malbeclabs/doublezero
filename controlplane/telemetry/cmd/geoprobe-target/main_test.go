package main

import (
	"io"
	"log/slog"
	"net"
	"strings"
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
	// Stamped far in the future: if the slot floor were consulted before the
	// signature, a forgery could raise the impersonated key's floor and lock
	// the real holder out.
	spoofed.MeasurementSlot = 1 << 40

	floor := newSlotFloor(floorEntryTTL)
	for _, forged := range []*geoprobe.LocationOffset{unsigned, spoofed} {
		handleOffset(log, forged, addr, true, writer, caches, floor)

		if got := len(writer.PendingRows()); got != 0 {
			t.Errorf("expected forged offset to be dropped, got %d buffered clickhouse rows", got)
		}
		if _, ok := caches.Get(forged.SenderPubkey).Best(); ok {
			t.Error("expected forged offset to be dropped, but it entered the cache")
		}
	}

	genuine := signedOffsetAt(t, mustSigner(t, impersonator.PrivateKey, impersonator.PublicKey()), 1_000_000, 1_000_000)
	handleOffset(log, genuine, addr, true, writer, caches, floor)
	if got := len(writer.PendingRows()); got != 1 {
		t.Errorf("a forged offset moved the impersonated key's floor: got %d rows for the genuine offset", got)
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

func mustSigner(t *testing.T, key solana.PrivateKey, sender solana.PublicKey) *geoprobe.OffsetSigner {
	t.Helper()
	signer, err := geoprobe.NewOffsetSigner(key, sender)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	return signer
}

func newTestSigner(t *testing.T) *geoprobe.OffsetSigner {
	t.Helper()
	return mustSigner(t, solana.NewWallet().PrivateKey, solana.NewWallet().PublicKey())
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

func TestSlotFloor_IsPerKey(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	fast := [32]byte{1}
	slow := [32]byte{2}

	floor.accept(fast, 5_000_000)

	if ok, reason, _, _ := floor.accept(slow, 1_000); !ok {
		t.Fatalf("a second key was judged against the first key's floor: %s", reason)
	}
}

// A rejected offer still refreshes the entry: otherwise a sustained replay
// outlives its own floor, the sweep drops the entry, and the next replay
// reseeds from itself — handing the attacker a fresh acceptance window every
// TTL instead of one per process restart.
func TestSlotFloor_RejectionKeepsFloorAlive(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)
	now = now.Add(maxFloorStall + time.Minute)

	// Keep replaying the frozen slot for well past floorEntryTTL, sweeping as
	// the daemon does.
	for elapsed := time.Duration(0); elapsed < 2*floorEntryTTL; elapsed += 5 * time.Minute {
		if ok, _, _, _ := floor.accept(key, 1_000_000); ok {
			t.Fatalf("replay accepted again %s after the floor stalled", elapsed)
		}
		floor.sweep()
		now = now.Add(5 * time.Minute)
	}

	// Positive control: a key that goes genuinely silent is eventually
	// forgotten, so the map stays bounded.
	silent := [32]byte{2}
	floor.accept(silent, 1_000_000)
	now = now.Add(floorEntryTTL + time.Minute)
	floor.sweep()
	if _, ok := floor.entries[silent]; ok {
		t.Error("expected a silent key's floor to be swept")
	}
}

// SenderPubkey is signed but unauthenticated — anyone can mint a keypair and
// stamp a real geoprobe's SenderPubkey. Keying floors on it would let one
// datagram carrying a huge slot lock that geoprobe out of location_offsets.
func TestHandleOffset_ForgedSenderCannotPoisonAnotherFloor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	caches := newTestCaches()
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}
	floor := newSlotFloor(floorEntryTTL)

	victim := solana.NewWallet().PublicKey()
	victimSigner := mustSigner(t, solana.NewWallet().PrivateKey, victim)
	// An attacker's own keypair, claiming the victim geoprobe as sender.
	attackerSigner := mustSigner(t, solana.NewWallet().PrivateKey, victim)

	handleOffset(log, signedOffsetAt(t, attackerSigner, 1<<40, 1_000_000), addr, true, writer, caches, floor)

	genuine := signedOffsetAt(t, victimSigner, 1_000_000, 5_000_000)
	before := len(writer.PendingRows())
	handleOffset(log, genuine, addr, true, writer, caches, floor)
	if len(writer.PendingRows()) != before+1 {
		t.Error("a forged sender claim locked the real geoprobe out of location_offsets")
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

// Without an advance ceiling one anomalous slot becomes the floor, and every
// genuine offer after it fails slot_regressed.
func TestSlotFloor_RejectsImplausibleJumpAndKeepsSenderIngesting(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)
	now = now.Add(time.Minute)

	// One cycle against the wrong ledger RPC stamps a Solana-L1-height slot.
	const anomalous = 400_000_000
	ok, reason, floorSlot, _ := floor.accept(key, anomalous)
	if ok {
		t.Fatal("expected an implausible jump to be rejected, not absorbed into the floor")
	}
	if reason != rejectSlotJumped {
		t.Errorf("reason = %q, want %q", reason, rejectSlotJumped)
	}
	if floorSlot != 1_000_000 {
		t.Errorf("floor moved to %d; the bad slot must not become the floor", floorSlot)
	}

	now = now.Add(time.Minute)
	if ok, reason, _, _ := floor.accept(key, 1_000_150); !ok {
		t.Fatalf("sender wedged out after one anomalous slot: %s", reason)
	}
}

// A probe that rode out its own RPC outage on a frozen cached slot jumps
// forward by roughly the outage, which the ceiling must not reject.
func TestSlotFloor_AllowsCatchUpAfterSenderRPCOutage(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)

	outage := 25 * time.Minute
	now = now.Add(outage)
	caughtUp := uint64(1_000_000) + uint64(outage/dzSlotDuration)
	if ok, reason, _, _ := floor.accept(key, caughtUp); !ok {
		t.Fatalf("honest catch-up after a %s outage rejected: %s", outage, reason)
	}
}

// A cluster sustaining the fastest assumed block time must not trip the ceiling.
func TestSlotFloor_ToleratesFastBlockTimes(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)

	elapsed := 30 * time.Minute
	now = now.Add(elapsed)
	fast := uint64(1_000_000) + uint64(elapsed/minSlotDuration)
	if ok, reason, _, _ := floor.accept(key, fast); !ok {
		t.Fatalf("a cluster sustaining %s slots tripped the ceiling: %s", minSlotDuration, reason)
	}
}

// A cache-refresh step arrives with almost no wall time behind it at the target.
func TestSlotFloor_AllowsCacheRefreshStepWithNoElapsedTime(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)

	step := uint64(geoprobe.SlotCacheTTL / dzSlotDuration)
	if ok, reason, _, _ := floor.accept(key, 1_000_000+step); !ok {
		t.Fatalf("a slot-cache refresh step rejected with no elapsed time: %s", reason)
	}
}

// slotsPer is how far a genuine sender's slot moves over d.
func slotsPer(d time.Duration) uint64 { return uint64(d / dzSlotDuration) }

// One bad slot-cache refresh stamps the same anomalous slot on every offer for
// SlotCacheTTL, and two in a row for twice that. Neither may take the floor.
func TestSlotFloor_DoesNotAdoptBriefAnomaly(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)
	for elapsed := time.Duration(0); elapsed < 2*geoprobe.SlotCacheTTL; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		if ok, _, _, _ := floor.accept(key, 400_000_000); ok {
			t.Fatalf("anomalous slot adopted %s into a bad refresh", elapsed)
		}
	}

	now = now.Add(30 * time.Second)
	if ok, reason, _, _ := floor.accept(key, 1_000_000+slotsPer(10*time.Minute)); !ok {
		t.Fatalf("sender wedged out after a brief anomaly: %s", reason)
	}
}

// The regression test for the stale-seed lockout: a replayed capture seeds the
// floor low, the replayer keeps it advancing by feeding captures at their
// original cadence, and the advance ceiling then rejects every genuine slot as
// slot_jumped. Remove the rival takeover and the genuine sender stays out for
// the week the capture is old.
func TestSlotFloor_GenuineSenderDisplacesStaleSeed(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	genuine := uint64(100_000_000)
	replay := genuine - slotsPer(7*24*time.Hour)
	if ok, reason, _, _ := floor.accept(key, replay); !ok {
		t.Fatalf("seed rejected: %s", reason)
	}

	var adoptedAfter time.Duration
	for elapsed := time.Duration(0); elapsed < 2*rivalConfirm; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		if elapsed%geoprobe.SlotCacheTTL == 0 {
			genuine += slotsPer(geoprobe.SlotCacheTTL)
		}
		replay += slotsPer(30 * time.Second)
		floor.accept(key, replay)
		if ok, _, _, _ := floor.accept(key, genuine); ok && adoptedAfter == 0 {
			adoptedAfter = elapsed
		}
	}
	if adoptedAfter == 0 || adoptedAfter > rivalConfirm {
		t.Fatalf("genuine sender adopted after %s, want within %s", adoptedAfter, rivalConfirm)
	}

	now = now.Add(30 * time.Second)
	if ok, _, _, _ := floor.accept(key, replay+slotsPer(30*time.Second)); ok {
		t.Error("replay still accepted after the genuine sender took the floor")
	}
}

// A replayer interleaving old captures must not keep resetting the genuine
// sender's rival run, or it would hold the stale floor indefinitely.
func TestSlotFloor_LowerOffersCannotResetRival(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	floor.accept(key, 1_000_000)
	genuine := uint64(50_000_000)
	for elapsed := time.Duration(0); elapsed <= rivalConfirm; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		genuine += slotsPer(30 * time.Second)
		floor.accept(key, 10_000_000) // out of range, below the genuine run
		if ok, _, _, _ := floor.accept(key, genuine); ok {
			return
		}
	}
	t.Fatal("interleaved lower offers kept the genuine sender out past rivalConfirm")
}

// A replayer holding old captures of a live sender must never displace its
// floor downward, however long it keeps sending.
func TestSlotFloor_ReplayCannotDisplaceLiveSender(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	genuine := uint64(100_000_000)
	replay := genuine - slotsPer(24*time.Hour)
	floor.accept(key, genuine)
	for elapsed := time.Duration(0); elapsed < 6*time.Hour; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		replay += slotsPer(30 * time.Second)
		if ok, _, _, _ := floor.accept(key, replay); ok {
			t.Fatalf("replay accepted %s in against a live sender", elapsed)
		}
		if elapsed%(5*time.Minute) == 0 {
			genuine += slotsPer(5 * time.Minute)
			if ok, reason, _, _ := floor.accept(key, genuine); !ok {
				t.Fatalf("live sender rejected: %s", reason)
			}
		}
	}
}

// The regression test for the seeding wedge: a probe pointed at the wrong
// ledger RPC seeds the floor at the wrong cluster's height, and once repointed
// every genuine offer fails slot_regressed. That floor stops advancing, so the
// genuine run takes over after maxFloorStall. Remove the rival takeover and
// the remediated sender never comes back.
func TestSlotFloor_RecoversFromWrongClusterSeed(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	// Misconfigured for an hour, then repointed.
	wrong := uint64(400_000_000)
	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += 5 * time.Minute {
		if ok, reason, _, _ := floor.accept(key, wrong); !ok {
			t.Fatalf("misconfigured sender rejected: %s", reason)
		}
		now = now.Add(5 * time.Minute)
		wrong += slotsPer(5 * time.Minute)
	}

	genuine := uint64(1_000_000)
	var recoveredAfter time.Duration
	for elapsed := time.Duration(0); elapsed < 2*time.Hour; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		if elapsed%geoprobe.SlotCacheTTL == 0 {
			genuine += slotsPer(geoprobe.SlotCacheTTL)
		}
		if ok, _, _, _ := floor.accept(key, genuine); ok && recoveredAfter == 0 {
			recoveredAfter = elapsed
		}
	}
	if recoveredAfter == 0 || recoveredAfter > maxFloorStall+rivalConfirm {
		t.Fatalf("remediated sender recovered after %s, want within %s", recoveredAfter, maxFloorStall+rivalConfirm)
	}
}

// Any minted keypair verifies, so no key's floor may bear on another's: a key
// parked at a low slot must not stop a genuine sender from seeding.
func TestSlotFloor_ParkedKeyCannotBlockSeed(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	floor.accept([32]byte{1}, 1_000)

	if ok, reason, _, _ := floor.accept([32]byte{2}, 100_000_000); !ok {
		t.Fatalf("a parked key blocked a genuine seed: %s", reason)
	}
}

// With verification off the slot is attacker-chosen, so it must not move the
// floor a later verified offer is judged against.
func TestHandleOffset_UnverifiedOffsetDoesNotMoveFloor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := geoprobe.NewClickhouseWriter(geoprobe.ClickhouseConfig{Addr: "unused"}, log)
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}
	floor := newSlotFloor(floorEntryTTL)

	offset := newTestOffset()
	offset.MeasurementSlot = 1 << 40
	handleOffset(log, offset, addr, false, writer, newTestCaches(), floor)

	if len(floor.entries) != 0 {
		t.Errorf("unverified offset created %d floor entries", len(floor.entries))
	}
}

// Text output must not report a check that never ran as a failed one.
func TestFormatTextOutput_UnverifiedIsNotInvalid(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 41234}
	text := formatTextOutput(formatLocationOffset(newTestOffset(), addr, false, signatureUnverifiedMarker))
	if strings.Contains(text, "INVALID") || !strings.Contains(text, "UNVERIFIED") {
		t.Errorf("unverified offset rendered as:\n%s", text)
	}
}

// The regression test for the repeat takeover: one datagram the ceiling
// rejected sits above the floor forever, so replaying that one capture every
// 5 minutes used to pass for a rival stream and seize the floor. A repeat never
// advances, so it must never qualify.
func TestSlotFloor_RepeatedJumpedCaptureCannotSeizeFloor(t *testing.T) {
	floor := newSlotFloor(floorEntryTTL)
	now := time.Now()
	floor.nowFunc = func() time.Time { return now }
	key := [32]byte{1}

	genuine := uint64(1_000_000)
	floor.accept(key, genuine)
	const captured = 400_000_000
	for elapsed := 30 * time.Second; elapsed < 46*time.Hour; elapsed += 30 * time.Second {
		now = now.Add(30 * time.Second)
		// The sender stamps from its slot cache, so its slot moves once per refresh.
		if elapsed%geoprobe.SlotCacheTTL == 0 {
			genuine += slotsPer(geoprobe.SlotCacheTTL)
			if ok, _, _, _ := floor.accept(key, captured); ok {
				t.Fatalf("repeated capture accepted %s in", elapsed)
			}
		}
		if ok, reason, _, _ := floor.accept(key, genuine); !ok {
			t.Fatalf("genuine sender rejected %s in: %s", elapsed, reason)
		}
	}
}
