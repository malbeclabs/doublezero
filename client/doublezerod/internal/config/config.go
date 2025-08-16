package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gagliardetto/solana-go"
)

type Config struct {
	LedgerRPCURL            string           `json:"ledger_rpc_url"`
	ServiceabilityProgramID solana.PublicKey `json:"serviceability_program_id"`

	path      string
	mu        sync.RWMutex
	changedCh chan struct{}
}

func New(path string) *Config {
	return &Config{
		path:      path,
		changedCh: make(chan struct{}, 1),
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	cfg := New(path)
	if err := cfg.UpdateFromJSON(data); err != nil {
		return nil, fmt.Errorf("error decoding config: %v", err)
	}

	return cfg, nil
}

func (c *Config) UpdateFromJSON(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("error unmarshalling config: %v", err)
	}

	if err := c.saveLocked(); err != nil {
		return err
	}

	c.notifyChanged()

	return nil
}

func (c *Config) Update(ledgerRPCURL string, serviceabilityProgramID solana.PublicKey) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.LedgerRPCURL == ledgerRPCURL && c.ServiceabilityProgramID == serviceabilityProgramID {
		return false, nil
	}

	c.LedgerRPCURL = ledgerRPCURL
	c.ServiceabilityProgramID = serviceabilityProgramID

	if err := c.saveLocked(); err != nil {
		return false, err
	}

	c.notifyChanged()

	return true, nil
}

func (c *Config) notifyChanged() {
	select {
	case c.changedCh <- struct{}{}:
	default:
	}
}

func (c *Config) Changed() <-chan struct{} {
	return c.changedCh
}

func (c *Config) RPCURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LedgerRPCURL
}

func (c *Config) ProgramID() solana.PublicKey {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ServiceabilityProgramID
}

// saveLocked assumes c.mu is held (write or read+upgrade).
func (c *Config) saveLocked() error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("error marshalling config: %v", err)
	}

	dir := filepath.Dir(c.path)
	tmp, err := os.CreateTemp(dir, ".cfg-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}
