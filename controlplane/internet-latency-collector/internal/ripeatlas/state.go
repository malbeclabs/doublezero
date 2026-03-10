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
)

type MeasurementState struct {
	filename string
	tracker  *MetadataTracker
	mu       sync.Mutex
}

type MetadataTracker struct {
	Metadata           map[int]MeasurementMeta  `json:"metadata"`
	UnresponsiveProbes []UnresponsiveProbeEntry `json:"unresponsive_probes,omitempty"`
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

	// Decode into intermediate struct with raw unresponsive_probes for backwards compatibility
	var intermediate struct {
		Metadata           map[int]MeasurementMeta `json:"metadata"`
		UnresponsiveProbes json.RawMessage         `json:"unresponsive_probes,omitempty"`
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&intermediate); err != nil {
		return fmt.Errorf("failed to decode timestamp file: %w", err)
	}

	var tracker MetadataTracker
	tracker.Metadata = intermediate.Metadata

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
	for _, entry := range ms.tracker.UnresponsiveProbes {
		if entry.ProbeID == probeID && entry.MarkedAt > expiry {
			return true
		}
	}
	return false
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
