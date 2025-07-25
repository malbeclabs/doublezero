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
	Timestamps map[int]int64 `json:"timestamps"`
}

type MeasurementTimestamp struct {
	MeasurementID int   `json:"measurement_id"`
	LastTimestamp int64 `json:"last_timestamp"`
}

func NewMeasurementState(filename string) *MeasurementState {
	return &MeasurementState{
		filename: filename,
		tracker: &TimestampTracker{
			Timestamps: make(map[int]int64),
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

	if tracker.Timestamps == nil {
		tracker.Timestamps = make(map[int]int64)
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
	timestamp, exists := ms.tracker.Timestamps[measurementID]
	return timestamp, exists
}

func (ms *MeasurementState) UpdateTimestamp(measurementID int, timestamp int64) {
	ms.tracker.Timestamps[measurementID] = timestamp
}

func (ms *MeasurementState) GetAllTimestamps() map[int]int64 {
	result := make(map[int]int64)
	for k, v := range ms.tracker.Timestamps {
		result[k] = v
	}
	return result
}
