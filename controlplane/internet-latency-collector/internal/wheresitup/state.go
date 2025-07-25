package wheresitup

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

type State struct {
	JobIDs   []string `json:"job_ids"`
	filename string
}

func NewState(filename string) *State {
	return &State{
		filename: filename,
		JobIDs:   []string{},
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
		return collector.ErrJobIDRetrieval.WithContext("filename", jt.filename).
			WithContext("cause", err.Error())
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(jt); err != nil {
		return collector.ErrJobIDRetrieval.WithContext("filename", jt.filename).
			WithContext("operation", "decode").
			WithContext("cause", err.Error())
	}

	return nil
}

func (jt *State) Save() error {
	if err := jt.validateFilename(); err != nil {
		return err
	}

	file, err := os.Create(jt.filename)
	if err != nil {
		return collector.ErrJobIDStorage.WithContext("filename", jt.filename).
			WithContext("cause", err.Error())
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(jt); err != nil {
		return collector.ErrJobIDStorage.WithContext("filename", jt.filename).
			WithContext("operation", "encode").
			WithContext("cause", err.Error())
	}

	return nil
}

func (jt *State) AddJobIDs(newJobIDs []string) error {
	if err := jt.Load(); err != nil {
		return err
	}

	jt.JobIDs = append(jt.JobIDs, newJobIDs...)
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

	var updatedIDs []string
	for _, id := range jt.JobIDs {
		if !removeSet[id] {
			updatedIDs = append(updatedIDs, id)
		}
	}

	jt.JobIDs = updatedIDs
	return jt.Save()
}

func (jt *State) GetJobIDs() []string {
	return jt.JobIDs
}
