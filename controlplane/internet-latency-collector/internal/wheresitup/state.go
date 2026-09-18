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

// MaxJobAge bounds how long a job stays in the state file and how long it is worth polling.
// One value covers both: past it the results are gone, so the entry can neither produce a
// sample nor justify its space. The steady-state poll set is ceil(MaxJobAge/interval)
// batches, about 11 at the 6-minute cadence against 20 under the previous 2-hour cutoff.
const MaxJobAge = JobExpireAfter + ExpiryGrace

type JobEntry struct {
	JobID     string    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	Jobs     []JobEntry `json:"jobs"`
	Circuits []string   `json:"circuits,omitempty"` // Circuits expected when jobs were created
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
		jt.Circuits = nil
		return nil
	}

	return nil
}

func (jt *State) Save() error {
	if err := jt.validateFilename(); err != nil {
		return err
	}

	// Deliberately does not prune: creation saves a batch about 30s before the export pass
	// runs, so a prune here would evict a job that crossed the cutoff between two passes
	// before export could count it, and expiry would go unreported. ExportJobResults owns it.
	if len(jt.Jobs) == 0 {
		jt.Circuits = nil
	}

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

// AddJobIDsWithCircuits records a creation pass. createdAt must be when the pass started:
// the vendor's expiry clock runs per job and a 435-pair pass takes minutes, so the end time
// understates the earliest jobs' age by more than ExpiryGrace and they outlive their
// results while still being polled.
func (jt *State) AddJobIDsWithCircuits(newJobIDs []string, circuits []string, createdAt time.Time) error {
	if err := jt.Load(); err != nil {
		return err
	}

	for _, jobID := range newJobIDs {
		jt.Jobs = append(jt.Jobs, JobEntry{
			JobID:     jobID,
			CreatedAt: createdAt,
		})
	}
	jt.Circuits = circuits
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

	// Clear circuits when all jobs are completed
	if len(jt.Jobs) == 0 {
		jt.Circuits = nil
	}

	return jt.Save()
}

// PruneExpired drops every job whose results WheresItUp has already discarded and returns
// the IDs it dropped. It is the only expiry predicate in the package, and ExportJobResults is
// its only caller, so every eviction is counted and logged once from one reading of the clock.
func (jt *State) PruneExpired(now time.Time) []string {
	cutoffTime := now.Add(-MaxJobAge)

	var activeJobs []JobEntry
	var expiredJobIDs []string
	for _, job := range jt.Jobs {
		if job.CreatedAt.After(cutoffTime) {
			activeJobs = append(activeJobs, job)
			continue
		}
		expiredJobIDs = append(expiredJobIDs, job.JobID)
	}
	jt.Jobs = activeJobs

	return expiredJobIDs
}

func (jt *State) GetJobIDs() []string {
	var jobIDs []string
	for _, job := range jt.Jobs {
		jobIDs = append(jobIDs, job.JobID)
	}
	return jobIDs
}
