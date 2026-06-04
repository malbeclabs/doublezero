package parser

import (
	"encoding/json"
	"fmt"
	"os"
)

// OrchestratorConfig mirrors the JSON shape the device-orchestrator dumps
// at start. Fields beyond what the reporter uses are kept as-is so a
// future analyzer can read them without re-parsing.
type OrchestratorConfig struct {
	RunID                         string `json:"run_id"`
	TargetUserCount               int    `json:"target_user_count"`
	UsersPerBatch                 int    `json:"users_per_batch"`
	HoldSeconds                   int    `json:"hold_seconds"`
	AgentQuietSeconds             int    `json:"agent_quiet_seconds"`
	AgentQuiescenceTimeoutSeconds int    `json:"agent_quiescence_timeout_seconds"`
	DUTPubkey                     string `json:"dut_pubkey"`
	DUTSSHHost                    string `json:"dut_ssh_host"`
	DUTSSHKey                     string `json:"dut_ssh_key"`
	DUTSSHUser                    string `json:"dut_ssh_user"`
	RPCURL                        string `json:"rpc_url"`
	ProgramID                     string `json:"program_id"`
	KeypairPath                   string `json:"keypair"`
	ControllerAddr                string `json:"controller"`
	AbortFile                     string `json:"abort_file"`
	WorkingDir                    string `json:"working_dir"`
	ClientIPBase                  string `json:"client_ip_base"`
	TunnelEndpoint                string `json:"tunnel_endpoint"`
	TenantPubkey                  string `json:"tenant_pubkey,omitempty"`
	NoAgent                       bool   `json:"no_agent"`
	AgentBinary                   string `json:"agent_binary,omitempty"`
	AgentCommandPrefix            string `json:"agent_command_prefix,omitempty"`
	AgentPubkey                   string `json:"agent_pubkey,omitempty"`
	AgentMetricsAddr              string `json:"agent_metrics_addr,omitempty"`
}

// IsPhysical heuristics off the presence of AgentCommandPrefix or
// AgentPubkey, which the containerized harness leaves empty (the
// device-side wrapper handles them) and the physical harness sets.
func (c *OrchestratorConfig) IsPhysical() bool {
	return c.AgentCommandPrefix != "" || c.AgentPubkey != ""
}

// ObserverConfig mirrors the JSON shape device-observer dumps at start.
// Only the fields the reporter currently uses are typed; the rest are
// preserved via the trailing Extra map.
type ObserverConfig struct {
	StartedAt       string `json:"started_at"`
	PID             int    `json:"pid"`
	DUTHost         string `json:"dut_host"`
	EAPIUser        string `json:"eapi_user"`
	AgentMetricsURL string `json:"agent_metrics_url"`
	SampleInterval  string `json:"sample_interval"`
	AbortFile       string `json:"abort_file"`
	WorkingDir      string `json:"working_dir"`
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

func loadObserverConfig(path string) (*ObserverConfig, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c ObserverConfig
	if err := json.Unmarshal(buf, &c); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &c, nil
}
