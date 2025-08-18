package wheresitup

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

const MaxJobAge = 2 * time.Hour

type JobEntry struct {
	JobID     string    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	Jobs     []JobEntry `json:"jobs"`
	filename string
	log      *slog.Logger
}

func NewState(filename string) *State {
	return &State{
		filename: filename,
		Jobs:     []JobEntry{},
		log:      slog.Default(),
	}
}

func NewStateWithLogger(filename string, logger *slog.Logger) *State {
	return &State{
		filename: filename,
		Jobs:     []JobEntry{},
		log:      logger,
	}
}

func (jt *State) validateFilename() error {
	if !strings.HasSuffix(jt.filename, ".json") {
		return collector.NewValidationError("filename_validation", "unsupported filename suffix, expected .json", nil).
			WithContext("filename", jt.filename)
	}
	return nil
}

func (jt *State) Load() error {
	if err := jt.validateFilename(); err != nil {
		return err
	}

	file, err := os.Open(jt.filename)
	if os.IsNotExist(err) {
		// File doesn't exist yet, keep empty list
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(jt); err != nil {
		// If decoding fails (old format or corruption), start fresh
		jt.log.Warn("Failed to decode state file, starting fresh",
			slog.String("filename", jt.filename),
			slog.String("error", err.Error()))
		jt.Jobs = []JobEntry{}
		return nil
	}

	return nil
}

func (jt *State) Save() error {
	if err := jt.validateFilename(); err != nil {
		return err
	}

	// Filter out jobs older than MaxJobAge
	cutoffTime := time.Now().Add(-MaxJobAge)

	var activeJobs []JobEntry
	for _, job := range jt.Jobs {
		if job.CreatedAt.After(cutoffTime) {
			activeJobs = append(activeJobs, job)
		}
	}
	jt.Jobs = activeJobs

	file, err := os.Create(jt.filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(jt); err != nil {
		return fmt.Errorf("failed to encode file: %w", err)
	}

	return nil
}

func (jt *State) AddJobIDs(newJobIDs []string) error {
	if err := jt.Load(); err != nil {
		return err
	}

	now := time.Now()
	for _, jobID := range newJobIDs {
		jt.Jobs = append(jt.Jobs, JobEntry{
			JobID:     jobID,
			CreatedAt: now,
		})
	}
	return jt.Save()
}

func (jt *State) RemoveJobIDs(jobIDsToRemove []string) error {
	if err := jt.Load(); err != nil {
		return err
	}

	removeSet := make(map[string]bool)
	for _, id := range jobIDsToRemove {
		removeSet[id] = true
	}

	var updatedJobs []JobEntry
	for _, job := range jt.Jobs {
		if !removeSet[job.JobID] {
			updatedJobs = append(updatedJobs, job)
		}
	}

	jt.Jobs = updatedJobs
	return jt.Save()
}

func (jt *State) GetJobIDs() []string {
	var jobIDs []string
	for _, job := range jt.Jobs {
		jobIDs = append(jobIDs, job.JobID)
	}
	return jobIDs
}
