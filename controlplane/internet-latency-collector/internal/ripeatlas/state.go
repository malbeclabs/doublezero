package ripeatlas

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// UnresponsiveProbeExpiry is how long a probe stays blacklisted before being retried.
	UnresponsiveProbeExpiry = 24 * time.Hour

	// TargetLossWindow is how long loss against a target probe accumulates before the
	// ratio is judged. It matches the staleness timeout so both checks reason over the
	// same horizon.
	TargetLossWindow = time.Hour

	// MinTargetAttemptsForLossCheck is the number of pings that must land in a window
	// before its loss ratio means anything. Below this a quiet window is indistinguishable
	// from a lossy one.
	MinTargetAttemptsForLossCheck = 30

	// MaxTargetLossRatio is the share of pings a target may drop before it is treated as
	// unresponsive. A healthy anchor sits near zero; a probe behind NAT runs far above
	// this while still replying often enough to keep the staleness check satisfied.
	MaxTargetLossRatio = 0.5

	// MaxTargetLossWindowAge bounds how long a window that closed short may keep
	// accumulating. Without a bound the duration gate stays satisfied forever and the
	// verdict is eventually rendered over an arbitrarily long span, so an old outage
	// can mark a target that has since answered every ping: two hours of 12 attempts at
	// 1 success, then a fully recovered hour, pools to 36/14 and reads as 61% loss.
	//
	// Two windows rather than more, because the tallies have to be dropped before they
	// cross MinTargetAttemptsForLossCheck or the stale evidence is judged anyway. The
	// cost is that a measurement with too few sources to reach the minimum inside two
	// windows is never judged at all, which is the safe direction: its pooled ratio is
	// one or two circuits' reachability rather than the target's.
	MaxTargetLossWindowAge = 2 * TargetLossWindow
)

type MeasurementState struct {
	filename string
	tracker  *MetadataTracker
	mu       sync.Mutex
}

type MetadataTracker struct {
	Metadata map[int]MeasurementMeta `json:"metadata"`

	// UnresponsiveProbes holds probes that failed as a measurement source, meaning
	// they stopped running measurements at all. Such a probe is excluded from both
	// source and target selection.
	UnresponsiveProbes []UnresponsiveProbeEntry `json:"unresponsive_probes,omitempty"`

	// UnresponsiveTargets holds probes that failed as a measurement target: they do
	// not answer pings aimed at them, or answer too few. That says nothing about the
	// probe's ability to send pings, so these still source measurements normally and
	// are only ranked last when a target is chosen.
	UnresponsiveTargets []UnresponsiveProbeEntry `json:"unresponsive_targets,omitempty"`
}

type UnresponsiveProbeEntry struct {
	ProbeID  int   `json:"probe_id"`
	MarkedAt int64 `json:"marked_at"`
}

type MeasurementMeta struct {
	TargetLocation string            `json:"target_location"`
	TargetProbeID  int               `json:"target_probe_id"`
	Sources        []SourceProbeMeta `json:"sources"`
	CreatedAt      int64             `json:"created_at"`
	LastExportAt   int64             `json:"last_export_at,omitempty"`

	// Rolling ping tallies against the target probe, reset each time the window is
	// judged. Attempts counts every result a source uploaded; successes counts those
	// that carried a latency back.
	TargetWindowStart int64 `json:"target_window_start,omitempty"`
	TargetAttempts    int64 `json:"target_attempts,omitempty"`
	TargetSuccesses   int64 `json:"target_successes,omitempty"`

	// TargetLossCursor is the newest result timestamp already counted into the tallies.
	// It is separate from the export cursor, which advances only past results carrying
	// a latency: a timeout newer than the last success is refetched by every incremental
	// query until a later success arrives, and counting those repeats would inflate the
	// loss ratio for a target that is merely losing its most recent pings.
	TargetLossCursor int64 `json:"target_loss_cursor,omitempty"`
}

type SourceProbeMeta struct {
	LocationCode   string `json:"location_code"`
	ProbeID        int    `json:"probe_id"`
	LastResponseAt int64  `json:"last_response_at,omitempty"`
}

func NewMeasurementState(filename string) *MeasurementState {
	return &MeasurementState{
		filename: filename,
		tracker: &MetadataTracker{
			Metadata: make(map[int]MeasurementMeta),
		},
	}
}

func (ms *MeasurementState) Load() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	file, err := os.Open(ms.filename)
	if os.IsNotExist(err) {
		// File doesn't exist yet, keep empty tracker
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open timestamp file: %w", err)
	}
	defer file.Close()

	// Decode into intermediate struct with raw unresponsive_probes for backwards
	// compatibility. unresponsive_targets is new in this format and has no legacy
	// shape to fall back from, so it decodes directly.
	var intermediate struct {
		Metadata            map[int]MeasurementMeta  `json:"metadata"`
		UnresponsiveProbes  json.RawMessage          `json:"unresponsive_probes,omitempty"`
		UnresponsiveTargets []UnresponsiveProbeEntry `json:"unresponsive_targets,omitempty"`
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&intermediate); err != nil {
		return fmt.Errorf("failed to decode timestamp file: %w", err)
	}

	var tracker MetadataTracker
	tracker.Metadata = intermediate.Metadata
	tracker.UnresponsiveTargets = intermediate.UnresponsiveTargets

	// Try new format first: [{probe_id: N, marked_at: T}, ...]
	if len(intermediate.UnresponsiveProbes) > 0 {
		if err := json.Unmarshal(intermediate.UnresponsiveProbes, &tracker.UnresponsiveProbes); err != nil {
			// Fall back to legacy format: [N, N, ...]
			var legacyProbes []int
			if err := json.Unmarshal(intermediate.UnresponsiveProbes, &legacyProbes); err == nil {
				tracker.UnresponsiveProbes = make([]UnresponsiveProbeEntry, len(legacyProbes))
				for i, probeID := range legacyProbes {
					tracker.UnresponsiveProbes[i] = UnresponsiveProbeEntry{
						ProbeID:  probeID,
						MarkedAt: time.Now().Unix(), // Treat legacy entries as freshly marked
					}
				}
			}
		}
	}

	if tracker.Metadata == nil {
		tracker.Metadata = make(map[int]MeasurementMeta)
	}

	ms.tracker = &tracker
	return nil
}

func (ms *MeasurementState) Save() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	file, err := os.Create(ms.filename)
	if err != nil {
		return fmt.Errorf("failed to create timestamp file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(ms.tracker); err != nil {
		return fmt.Errorf("failed to encode timestamp file: %w", err)
	}

	return nil
}

func (ms *MeasurementState) GetLastTimestamp(measurementID int) (int64, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if meta, exists := ms.tracker.Metadata[measurementID]; exists {
		return meta.LastExportAt, meta.LastExportAt > 0
	}
	return 0, false
}

func (ms *MeasurementState) UpdateTimestamp(measurementID int, timestamp int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if meta, exists := ms.tracker.Metadata[measurementID]; exists {
		meta.LastExportAt = timestamp
		ms.tracker.Metadata[measurementID] = meta
	} else {
		// Create minimal metadata with just the timestamp
		ms.tracker.Metadata[measurementID] = MeasurementMeta{
			LastExportAt: timestamp,
		}
	}
}

func (ms *MeasurementState) SetMetadata(measurementID int, meta MeasurementMeta) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.tracker.Metadata[measurementID] = meta
}

func (ms *MeasurementState) GetMetadata(measurementID int) (MeasurementMeta, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	meta, exists := ms.tracker.Metadata[measurementID]
	return meta, exists
}

func (ms *MeasurementState) RemoveMetadata(measurementID int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delete(ms.tracker.Metadata, measurementID)
}

func (ms *MeasurementState) GetAllTimestamps() map[int]int64 {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	result := make(map[int]int64)
	for id, meta := range ms.tracker.Metadata {
		if meta.LastExportAt > 0 {
			result[id] = meta.LastExportAt
		}
	}
	return result
}

func (ms *MeasurementState) GetAllMetadata() map[int]MeasurementMeta {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	result := make(map[int]MeasurementMeta, len(ms.tracker.Metadata))
	for id, meta := range ms.tracker.Metadata {
		result[id] = meta
	}
	return result
}

func (ms *MeasurementState) MetadataCount() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	return len(ms.tracker.Metadata)
}

func (ms *MeasurementState) UpdateSourceProbeResponse(measurementID int, probeID int, timestamp int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	meta, exists := ms.tracker.Metadata[measurementID]
	if !exists {
		return
	}

	for i, source := range meta.Sources {
		if source.ProbeID == probeID {
			if timestamp > source.LastResponseAt {
				meta.Sources[i].LastResponseAt = timestamp
			}
			break
		}
	}
	ms.tracker.Metadata[measurementID] = meta
}

// RecordTargetResults adds a batch of ping outcomes against a measurement's target
// probe to the current window, starting one if none is open, and advances the loss
// cursor to newestResult so the same outcomes are not counted twice.
//
// The caller is responsible for counting only results newer than TargetLossCursor.
func (ms *MeasurementState) RecordTargetResults(measurementID int, attempts, successes, newestResult, now int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	meta, exists := ms.tracker.Metadata[measurementID]
	if !exists || attempts <= 0 {
		return
	}

	if meta.TargetWindowStart == 0 {
		meta.TargetWindowStart = now
	}
	meta.TargetAttempts += attempts
	meta.TargetSuccesses += successes
	if newestResult > meta.TargetLossCursor {
		meta.TargetLossCursor = newestResult
	}
	ms.tracker.Metadata[measurementID] = meta
}

// EvaluateTargetLoss judges a measurement's open loss window and resets it. It reports
// whether the target dropped more than MaxTargetLossRatio of the pings aimed at it,
// along with the tallies behind that call.
//
// A window is only judged once it has run for TargetLossWindow and carries at least
// MinTargetAttemptsForLossCheck attempts; until then the window stays open and this
// reports false. Resetting on every judged window means a probe that recovers starts
// from a clean slate rather than carrying old loss forward.
func (ms *MeasurementState) EvaluateTargetLoss(measurementID int, now int64) (lossy bool, attempts, successes int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	meta, exists := ms.tracker.Metadata[measurementID]
	if !exists || meta.TargetWindowStart == 0 {
		return false, 0, 0
	}

	if now-meta.TargetWindowStart < int64(TargetLossWindow.Seconds()) {
		return false, meta.TargetAttempts, meta.TargetSuccesses
	}

	attempts, successes = meta.TargetAttempts, meta.TargetSuccesses

	// A window that closes short stays open and keeps accumulating rather than being
	// reset. Resetting here would discard the evidence every hour, and a measurement
	// with few enough sources never reaches the minimum within one window: at the
	// 10 minute sampling interval a source contributes 6 attempts an hour, so fewer
	// than 5 sources could never be judged at all.
	//
	// It does not accumulate indefinitely, though. Past MaxTargetLossWindowAge the
	// tallies are dropped unjudged, so a recovered target is not marked on evidence
	// from an outage it has already come back from.
	if attempts < MinTargetAttemptsForLossCheck {
		if now-meta.TargetWindowStart >= int64(MaxTargetLossWindowAge.Seconds()) {
			meta.TargetWindowStart = now
			meta.TargetAttempts = 0
			meta.TargetSuccesses = 0
			ms.tracker.Metadata[measurementID] = meta
		}
		return false, attempts, successes
	}

	meta.TargetWindowStart = now
	meta.TargetAttempts = 0
	meta.TargetSuccesses = 0
	ms.tracker.Metadata[measurementID] = meta

	lossRatio := 1 - float64(successes)/float64(attempts)
	return lossRatio > MaxTargetLossRatio, attempts, successes
}

// AddUnresponsiveTarget records that a probe failed as a measurement target. Unlike
// AddUnresponsiveProbe this does not bar the probe from sourcing measurements, since
// failing to answer pings says nothing about sending them.
func (ms *MeasurementState) AddUnresponsiveTarget(probeID int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, entry := range ms.tracker.UnresponsiveTargets {
		if entry.ProbeID == probeID {
			return
		}
	}
	ms.tracker.UnresponsiveTargets = append(ms.tracker.UnresponsiveTargets, UnresponsiveProbeEntry{
		ProbeID:  probeID,
		MarkedAt: time.Now().Unix(),
	})
}

func (ms *MeasurementState) IsTargetUnresponsive(probeID int) bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	// A probe that cannot source cannot target either, so both lists mark a target.
	return hasLiveEntry(ms.tracker.UnresponsiveTargets, probeID, expiry) ||
		hasLiveEntry(ms.tracker.UnresponsiveProbes, probeID, expiry)
}

// hasLiveEntry reports whether entries hold an unexpired mark for probeID. Callers hold
// ms.mu; the mutex is not reentrant, so this stays a free function rather than a method.
func hasLiveEntry(entries []UnresponsiveProbeEntry, probeID int, expiry int64) bool {
	for _, entry := range entries {
		if entry.ProbeID == probeID && entry.MarkedAt > expiry {
			return true
		}
	}
	return false
}

func (ms *MeasurementState) GetUnresponsiveTargets() []int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	probes := []int{}
	for _, entry := range ms.tracker.UnresponsiveTargets {
		if entry.MarkedAt > expiry {
			probes = append(probes, entry.ProbeID)
		}
	}
	return probes
}

// PruneExpiredUnresponsiveTargets removes target entries older than UnresponsiveProbeExpiry.
func (ms *MeasurementState) PruneExpiredUnresponsiveTargets() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	var kept []UnresponsiveProbeEntry
	pruned := 0
	for _, entry := range ms.tracker.UnresponsiveTargets {
		if entry.MarkedAt > expiry {
			kept = append(kept, entry)
		} else {
			pruned++
		}
	}
	ms.tracker.UnresponsiveTargets = kept
	return pruned
}

func (ms *MeasurementState) AddUnresponsiveProbe(probeID int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Check if probe is already in the list
	for _, entry := range ms.tracker.UnresponsiveProbes {
		if entry.ProbeID == probeID {
			return
		}
	}
	ms.tracker.UnresponsiveProbes = append(ms.tracker.UnresponsiveProbes, UnresponsiveProbeEntry{
		ProbeID:  probeID,
		MarkedAt: time.Now().Unix(),
	})
}

func (ms *MeasurementState) IsProbeUnresponsive(probeID int) bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	return hasLiveEntry(ms.tracker.UnresponsiveProbes, probeID, expiry)
}

func (ms *MeasurementState) GetUnresponsiveProbes() []int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.tracker.UnresponsiveProbes == nil {
		return []int{}
	}
	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	var result []int
	for _, entry := range ms.tracker.UnresponsiveProbes {
		if entry.MarkedAt > expiry {
			result = append(result, entry.ProbeID)
		}
	}
	if result == nil {
		return []int{}
	}
	return result
}

// PruneExpiredUnresponsiveProbes removes entries older than UnresponsiveProbeExpiry.
func (ms *MeasurementState) PruneExpiredUnresponsiveProbes() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	expiry := time.Now().Add(-UnresponsiveProbeExpiry).Unix()
	var kept []UnresponsiveProbeEntry
	pruned := 0
	for _, entry := range ms.tracker.UnresponsiveProbes {
		if entry.MarkedAt > expiry {
			kept = append(kept, entry)
		} else {
			pruned++
		}
	}
	ms.tracker.UnresponsiveProbes = kept
	return pruned
}
