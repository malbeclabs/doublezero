package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// OrchestratorConfig mirrors the subset of orchestrator-config.json that the
// reporter actually reads. Extra fields the orchestrator dumps are tolerated
// (json.Unmarshal ignores unknown keys); add typed fields here only when
// something downstream needs to read them.
type OrchestratorConfig struct {
	RunID              string `json:"run_id"`
	TargetUserCount    int    `json:"target_user_count"`
	UsersPerBatch      int    `json:"users_per_batch"`
	HoldSeconds        int    `json:"hold_seconds"`
	AgentCommandPrefix string `json:"agent_command_prefix,omitempty"`
	AgentPubkey        string `json:"agent_pubkey,omitempty"`
	// DUTSSHHost is the ssh target the orchestrator drove (e.g.
	// "chi-dn-dzd9:22" or "10.0.0.15:22"). The reporter strips the port
	// to render a device name in the summary header.
	DUTSSHHost string `json:"dut_ssh_host,omitempty"`
}

// DUTName returns the DUT identifier without the SSH port suffix, for
// display in the summary header. Falls back to the raw value when no
// colon is present.
func (c *OrchestratorConfig) DUTName() string {
	host, _, _ := strings.Cut(c.DUTSSHHost, ":")
	return host
}

// IsPhysical heuristics off the presence of AgentCommandPrefix or
// AgentPubkey, which the containerized harness leaves empty (the
// device-side wrapper handles them) and the physical harness sets.
func (c *OrchestratorConfig) IsPhysical() bool {
	return c.AgentCommandPrefix != "" || c.AgentPubkey != ""
}

func loadOrchestratorConfig(path string) (*OrchestratorConfig, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c OrchestratorConfig
	if err := json.Unmarshal(buf, &c); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &c, nil
}
