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
	// from a lossy one. It assumes the 10 minute sampling interval production runs, at
	// which a source contributes 6 attempts an hour; a much longer interval would leave
	// most measurements unjudged and a much shorter one would judge on minutes of data.
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
	//
	// The cap itself is this plus TargetLossWindowGrace, for the same drift reason the
	// grace exists at all. A measurement with 3 or 4 sources reaches the minimum in its
	// second window, so it is judged at an age of about two windows — right where a bare
	// two-window cap sits. Whether the cycle landed a few seconds either side of the
	// boundary would then decide between a verdict and dropping the evidence, which is
	// the coin flip the grace removes on the young side. Allowing the cap a grace too
	// puts the boundary clear of that, and a 2-source window still ends up dropped: it
	// holds 24 attempts at two windows, under the minimum, and is past the cap by three.
	MaxTargetLossWindowAge = 2 * TargetLossWindow

	// TargetLossWindowGrace is how early a window may be judged. TargetLossWindow
	// equals the default management interval, and the clock a cycle judges against is
	// its start plus that cycle's pre-work: location and probe fetches, each with its
	// own CallDelay sleeps, varying by seconds to tens of seconds. Judging only at the
	// full hour means any cycle faster than the one that last reset the window lands a
	// few seconds short and slips the verdict by another full hour, so detection
	// latency is one hour or two at random.
	TargetLossWindowGrace = 5 * time.Minute
)

type MeasurementState struct {
	filename string
	tracker  *MetadataTracker
	mu       sync.Mutex

	// migratedTargetMarks counts what the last Load reclassified, for the caller to log.
	migratedTargetMarks int
}

type MetadataTracker struct {
	Metadata map[int]MeasurementMeta `json:"metadata"`

	// UnresponsiveProbes holds probes that failed as a measurement source, meaning
	// they stopped running measurements at all. Such a probe is excluded from source
	// selection and ranked last in target selection — not excluded from it, since a
	// metro whose only candidate is marked still needs a target.
	UnresponsiveProbes []UnresponsiveProbeEntry `json:"unresponsive_probes,omitempty"`

	// UnresponsiveTargets holds probes that failed as a measurement target: they do
	// not answer pings aimed at them, or answer too few. That says nothing about the
	// probe's ability to send pings, so these still source measurements normally and
	// are only ranked last when a target is chosen.
	UnresponsiveTargets []UnresponsiveProbeEntry `json:"unresponsive_targets,omitempty"`

	// UnresponsiveMarksMigrated records that the one-time reclassification in Load has
	// run against this file, so a source failure marked later is not moved by it.
	UnresponsiveMarksMigrated bool `json:"unresponsive_marks_migrated,omitempty"`
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
		Metadata                  map[int]MeasurementMeta  `json:"metadata"`
		UnresponsiveProbes        json.RawMessage          `json:"unresponsive_probes,omitempty"`
		UnresponsiveTargets       []UnresponsiveProbeEntry `json:"unresponsive_targets,omitempty"`
		UnresponsiveMarksMigrated bool                     `json:"unresponsive_marks_migrated,omitempty"`
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&intermediate); err != nil {
		return fmt.Errorf("failed to decode timestamp file: %w", err)
	}

	var tracker MetadataTracker
	tracker.Metadata = intermediate.Metadata
	tracker.UnresponsiveTargets = intermediate.UnresponsiveTargets
	tracker.UnresponsiveMarksMigrated = intermediate.UnresponsiveMarksMigrated

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

	ms.migratedTargetMarks = 0
	if !tracker.UnresponsiveMarksMigrated {
		ms.migratedTargetMarks = migrateTargetMarks(&tracker)
		tracker.UnresponsiveMarksMigrated = true
	}

	ms.tracker = &tracker
	return nil
}

// migrateTargetMarks moves target failures out of the source list, reporting how many it
// moved. Until the two roles were split the staleness path marked a measurement's target
// probe with AddUnresponsiveProbe, so a file written by an earlier build holds target
// failures where they still bar the probe from sourcing. Left in place they would swap
// the metro's source probe back when they expire and tear down every measurement it
// feeds, which is the cascade the split exists to remove, paid one last time.
//
// A marked probe that is some measurement's target probe was marked by that path. The
// heuristic is not airtight — Step 4b could have marked the same probe as a source — so
// Load runs it once and records that in the file rather than re-deciding on every start.
//
// Moving to the target list only is safe even for a probe that is offline: its
// measurement exports nothing, and the next never_exported cycle marks it in both lists
// again, so the source bar is recovered rather than lost.
func migrateTargetMarks(tracker *MetadataTracker) int {
	targetProbes := make(map[int]bool, len(tracker.Metadata))
	for _, meta := range tracker.Metadata {
		if meta.TargetProbeID != 0 {
			targetProbes[meta.TargetProbeID] = true
		}
	}

	alreadyTarget := make(map[int]bool, len(tracker.UnresponsiveTargets))
	for _, entry := range tracker.UnresponsiveTargets {
		alreadyTarget[entry.ProbeID] = true
	}

	var keptSources []UnresponsiveProbeEntry
	moved := 0
	for _, entry := range tracker.UnresponsiveProbes {
		if !targetProbes[entry.ProbeID] {
			keptSources = append(keptSources, entry)
			continue
		}
		if !alreadyTarget[entry.ProbeID] {
			// MarkedAt carries over, so the mark expires when it always would have.
			tracker.UnresponsiveTargets = append(tracker.UnresponsiveTargets, entry)
			alreadyTarget[entry.ProbeID] = true
		}
		moved++
	}
	tracker.UnresponsiveProbes = keptSources
	return moved
}

// MigratedTargetMarks reports how many marks the last Load reclassified from the source
// list to the target list.
func (ms *MeasurementState) MigratedTargetMarks() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	return ms.migratedTargetMarks
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
// A window is only judged once it has run for TargetLossWindow, less
// TargetLossWindowGrace, and carries at least MinTargetAttemptsForLossCheck attempts;
// until then the window stays open and this reports false. Resetting on every judged
// window means a probe that recovers starts from a clean slate rather than carrying old
// loss forward.
func (ms *MeasurementState) EvaluateTargetLoss(measurementID int, now int64) (lossy bool, attempts, successes int64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	meta, exists := ms.tracker.Metadata[measurementID]
	if !exists || meta.TargetWindowStart == 0 {
		return false, 0, 0
	}

	windowAge := now - meta.TargetWindowStart
	if windowAge < int64((TargetLossWindow - TargetLossWindowGrace).Seconds()) {
		return false, meta.TargetAttempts, meta.TargetSuccesses
	}

	attempts, successes = meta.TargetAttempts, meta.TargetSuccesses

	// An aged-out window is dropped unjudged whatever it holds, so no verdict is ever
	// rendered over much more than MaxTargetLossWindowAge of evidence. Checked ahead of
	// the attempt count deliberately: a cycle that drifts past the cap still accumulates,
	// so by the next cycle a thin-fan-in window can hold enough attempts to be judged
	// and would convict a target on an outage it has already recovered from.
	//
	// The bound carries TargetLossWindowGrace, like the young gate above and for the
	// same reason: a 3- or 4-source window is ready to judge at an age of about two
	// windows, so a bare cap would let cycle drift decide between a verdict and
	// discarding the evidence.
	if windowAge >= int64((MaxTargetLossWindowAge + TargetLossWindowGrace).Seconds()) {
		meta.TargetWindowStart = now
		meta.TargetAttempts = 0
		meta.TargetSuccesses = 0
		ms.tracker.Metadata[measurementID] = meta
		return false, attempts, successes
	}

	// A window that closes short stays open and keeps accumulating rather than being
	// reset. Resetting here would discard the evidence every hour, and a measurement
	// with few enough sources never reaches the minimum within one window: at the
	// 10 minute sampling interval a source contributes 6 attempts an hour, so fewer
	// than 5 sources could never be judged at all.
	if attempts < MinTargetAttemptsForLossCheck {
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
// AddUnresponsiveProbe this does not bar the probe from sourcing measurements: for a
// probe that has answered before and stopped, failing to answer pings says nothing
// about sending them. That does not hold for a probe whose measurement never produced
// a single result, which is offline rather than unreachable inbound; the caller marks
// that one in both lists.
func (ms *MeasurementState) AddUnresponsiveTarget(probeID int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.tracker.UnresponsiveTargets = refreshOrAppendMark(ms.tracker.UnresponsiveTargets, probeID)
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

	ms.tracker.UnresponsiveProbes = refreshOrAppendMark(ms.tracker.UnresponsiveProbes, probeID)
}

// refreshOrAppendMark stamps probeID's entry with the current time, adding one if it is
// absent. A repeat mark extends the existing entry rather than being discarded: the
// collector re-judges a bad probe every cycle, and keeping the first timestamp meant the
// mark expired 24h after that first judgement no matter how recent the evidence. The
// probe was then ranked first again on distance, its metro torn down to retarget at it,
// and a full window of bad data had to accumulate before it could be re-marked — a
// second teardown to move back off it, every 24h, indefinitely.
//
// Refreshing also removes the dependency on PruneExpiredUnresponsiveProbes having run
// first: an expired entry is restamped rather than left to read as expired.
// Callers hold ms.mu.
func refreshOrAppendMark(entries []UnresponsiveProbeEntry, probeID int) []UnresponsiveProbeEntry {
	now := time.Now().Unix()
	for i := range entries {
		if entries[i].ProbeID == probeID {
			entries[i].MarkedAt = now
			return entries
		}
	}
	return append(entries, UnresponsiveProbeEntry{ProbeID: probeID, MarkedAt: now})
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
