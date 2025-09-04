package ripeatlas

import (
	"encoding/json"
	"fmt"
	"os"
)

type MeasurementState struct {
	filename string
	tracker  *TimestampTracker
}

type TimestampTracker struct {
	Metadata map[int]MeasurementMeta `json:"metadata"`
}

type MeasurementMeta struct {
	TargetLocation string            `json:"target_location"`
	TargetProbeID  int               `json:"target_probe_id"`
	Sources        []SourceProbeMeta `json:"sources"`
	CreatedAt      int64             `json:"created_at"`
	LastExportAt   int64             `json:"last_export_at,omitempty"`
}

type SourceProbeMeta struct {
	LocationCode string `json:"location_code"`
	ProbeID      int    `json:"probe_id"`
}

func NewMeasurementState(filename string) *MeasurementState {
	return &MeasurementState{
		filename: filename,
		tracker: &TimestampTracker{
			Metadata: make(map[int]MeasurementMeta),
		},
	}
}

func (ms *MeasurementState) Load() error {
	file, err := os.Open(ms.filename)
	if os.IsNotExist(err) {
		// File doesn't exist yet, keep empty tracker
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open timestamp file: %w", err)
	}
	defer file.Close()

	var tracker TimestampTracker
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&tracker); err != nil {
		return fmt.Errorf("failed to decode timestamp file: %w", err)
	}

	if tracker.Metadata == nil {
		tracker.Metadata = make(map[int]MeasurementMeta)
	}

	ms.tracker = &tracker
	return nil
}

func (ms *MeasurementState) Save() error {
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
	if meta, exists := ms.tracker.Metadata[measurementID]; exists {
		return meta.LastExportAt, meta.LastExportAt > 0
	}
	return 0, false
}

func (ms *MeasurementState) UpdateTimestamp(measurementID int, timestamp int64) {
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
	ms.tracker.Metadata[measurementID] = meta
}

func (ms *MeasurementState) GetMetadata(measurementID int) (MeasurementMeta, bool) {
	meta, exists := ms.tracker.Metadata[measurementID]
	return meta, exists
}

func (ms *MeasurementState) RemoveMetadata(measurementID int) {
	delete(ms.tracker.Metadata, measurementID)
}

func (ms *MeasurementState) GetAllTimestamps() map[int]int64 {
	result := make(map[int]int64)
	for id, meta := range ms.tracker.Metadata {
		if meta.LastExportAt > 0 {
			result[id] = meta.LastExportAt
		}
	}
	return result
}
