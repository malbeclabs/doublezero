package netlink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	ErrMissingStateDir = errors.New("missing state directory")
)

type Db struct {
	State *ProvisionRequest
	Path  string
	mu    sync.Mutex
}

func stateDir() string {
	path := os.Getenv("XDG_STATE_HOME")
	if path == "" {
		path = "/var/lib/"
	}
	return filepath.Join(path, "doublezerod")
}

// NewDb returns a Db struct to handle tracking the latest provisioned state of
// a client.
func NewDb() (*Db, error) {
	dir := stateDir()
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		// We ideally want systemd to create this prior to startup so failing
		// until we have a reason to change.
		return nil, ErrMissingStateDir
	}
	path := filepath.Join(dir, "doublezerod.json")

	info, err := os.Stat(path)
	if err == nil && info.Size() != 0 {
		// if file exists, we assume we either crashed or were restarted and need to recover
		file, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("error reading db file: %v", err)
		}
		p := &ProvisionRequest{}
		if err := json.Unmarshal(file, p); err != nil {
			return nil, fmt.Errorf("error unmarshaling db file: %v", err)
		}
		return &Db{Path: path, State: p, mu: sync.Mutex{}}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		// if no state file exists, create an empty one so we can check permissions on init
		if err = WriteFile(path, []byte{}, 0666); err != nil {
			return nil, fmt.Errorf("error creating db file: %v", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error checking for state file: %v", err)
	}
	return &Db{Path: path, State: nil, mu: sync.Mutex{}}, nil
}

// GetState returns the latest provisioned state
func (d *Db) GetState() *ProvisionRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.State
}

// TODO: this needs to be implemented once the remove endpoint is added
// Delete removes the latest provisioned state from disk
func (d *Db) DeleteState() error {
	if _, err := os.Stat(d.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// nothing to delete
			return nil
		}
		return fmt.Errorf("error checking for state file: %v", err)
	}
	if err := os.Remove(d.Path); err != nil {
		return fmt.Errorf("error delete state file: %v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.State = nil
	return nil
}

// Save writes the latest provisioned state to disk
func (d *Db) SaveState(p *ProvisionRequest) error {
	if p == nil {
		return fmt.Errorf("provision request is nil")
	}
	buf, err := json.MarshalIndent(p, "", "    ")
	if err != nil {
		return fmt.Errorf("error saving state: %v", err)
	}
	d.mu.Lock()
	d.State = p
	d.mu.Unlock()
	buf = append(buf, []byte("\n")...)
	if err = WriteFile(d.Path, buf, 0666); err != nil {
		return fmt.Errorf("error writing state file: %v", err)
	}
	return nil
}

// This is from a tailscale library but marked as an unstable API:
// https://github.com/tailscale/tailscale/blob/main/atomicfile/atomicfile.go
func WriteFile(filename string, data []byte, perm os.FileMode) (err error) {
	fi, err := os.Stat(filename)
	if err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s already exists and is not a regular file", filename)
	}
	f, err := os.CreateTemp(filepath.Dir(filename), filepath.Base(filename)+".tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(perm); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filename)
}
