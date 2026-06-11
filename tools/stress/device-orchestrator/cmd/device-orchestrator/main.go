// device-orchestrator runs the GRE Tunnel Capacity Study sweep against a
// live serviceability program: provisions N users on a target device in
// batches with a hold between each, then deprovisions in reverse-creation
// order. Per #3771 (part 2 of #3746) the SSH-driven agent runner is stubbed
// behind the agent.Runner interface; the no-op implementation is used here
// and the SSH implementation lands in part 3.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/abort"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/agent"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/exec"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/runlog"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/sweep"
)

// orchestratorConfig captures the resolved CLI inputs in the shape that gets
// dumped to orchestrator-config.json on start.
type orchestratorConfig struct {
	RunID                         string `json:"run_id"`
	TargetUserCount               int    `json:"target_user_count"`
	UsersPerBatch                 int    `json:"users_per_batch"`
	HoldSeconds                   int    `json:"hold_seconds"`
	AgentQuietSeconds             int    `json:"agent_quiet_seconds"`
	AgentQuiescenceTimeoutSeconds int    `json:"agent_quiescence_timeout_seconds"`
	ApplyCatchUpTimeoutSeconds    int    `json:"apply_catch_up_timeout_seconds"`
	ApplyPerBatchCatchUp          bool   `json:"apply_per_batch_catch_up"`
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		targetUserCount = flag.Int("target-user-count", 8, "Final user count to sweep up to.")
		usersPerBatch   = flag.Int("users-per-batch", 2, "Users provisioned per batch before the hold.")
		holdSeconds     = flag.Int("hold-seconds", 180, "Seconds to hold between batches.")
		dutPubkey       = flag.String("dut-pubkey", "", "Device-under-test pubkey (base58).")
		dutSSHHost      = flag.String("dut-ssh-host", "", "SSH host:port for the DUT (used by the part-3 agent runner).")
		dutSSHKey       = flag.String("dut-ssh-key", "", "SSH private-key path for the DUT.")
		rpcURL          = flag.String("rpc-url", "", "Serviceability RPC URL.")
		programID       = flag.String("program-id", "", "Serviceability program ID (base58).")
		keypairPath     = flag.String("keypair", "", "Path to the orchestrator's solana keypair JSON.")
		controllerAddr  = flag.String("controller", "", "Controller IP:PORT, forwarded to the DUT agent in part 3.")
		abortFile       = flag.String("abort-file", "", "Path to a sentinel file; when it appears the sweep finishes the current user and exits.")
		workingDir      = flag.String("working-dir", ".", "Output directory for orchestrator-config.json / orchestrator-runlog.jsonl.")
		clientIPBase    = flag.String("client-ip-base", "100.64.0.0", "Starting IPv4 address; per-user IP is base + idx.")
		tunnelEndpoint  = flag.String("tunnel-endpoint", "0.0.0.0", "Tunnel endpoint IP passed to UserCreateArgs; 0.0.0.0 lets the program fall back to the device's public IP.")
		tenantPubkey    = flag.String("tenant-pubkey", "", "Optional tenant pubkey for UserCreateArgs.")
		runID           = flag.String("run-id", "", "Run identifier written into every runlog row; auto-generated if empty.")
		logLevel        = flag.String("log-level", "info", "slog level: debug|info|warn|error.")
		dryRun          = flag.Bool("dry-run", false, "Validate flags and dump orchestrator-config.json without contacting the RPC.")
		dutSSHUser      = flag.String("dut-ssh-user", "admin", "SSH user for the DUT.")
		noAgent         = flag.Bool("no-agent", false, "Use the no-op AgentRunner even when SSH flags are set (offline testing).")
		// Default: 15 s — clears the 5 s controller poll plus an apply margin
		// observed up to ~10 s per cycle at 1024 users / batch=32.
		agentQuietSeconds = flag.Int("agent-quiet-seconds", 15, "Seconds of agent silence (no EventApplied) required after deprovision before the SSH session is cancelled. 0 disables the wait.")
		// Default: 300 s — accommodates the 22k-line / ~650 KB final config
		// commit at 1024 tunnels.
		agentQuiescenceTimeoutSeconds = flag.Int("agent-quiescence-timeout-seconds", 300, "Hard cap on the post-deprovision agent quiescence wait, in seconds.")
		// Default: 300 s — same cap as agent-quiescence-timeout-seconds.
		// Aligns the provision→deprovision boundary wait with the
		// post-deprovision one. The wait blocks until applied >= target
		// (minus a small grace), which prevents deprovision from
		// removing users before the agent has had time to add them.
		// Set to 0 to disable; useful when reproducing pre-3796
		// behavior or for --no-agent runs where applied never lands.
		applyCatchUpTimeoutSeconds = flag.Int("apply-catch-up-timeout-seconds", 300, "Hard cap on the provision→deprovision wait for the agent's applied count to catch up to the provisioned-user count. 0 disables the wait.")
		// Off by default. Real production traffic adds users at
		// human cadence, not in burst-fashion, so this flag is the
		// closer match for measuring per-user latency under steady-
		// state load. Off matches the default "stress the agent" shape
		// the harness was originally built for.
		applyPerBatchCatchUp = flag.Bool("apply-per-batch-catch-up", false, "Pause after each provision batch until the agent's applied count covers the cumulative target; honors --apply-catch-up-timeout-seconds per batch.")
		// The containerized harness ships a device-side wrapper (see
		// tools/stress/docker/device/agent-wrapper.sh) that injects -pubkey from
		// /etc/doublezero/agent/pubkey and re-execs through sudo, so the
		// orchestrator can leave these empty and rely on PATH + the wrapper.
		// Against a physical device with no wrapper, set --agent-binary to the
		// full path on the device, --agent-command-prefix to e.g.
		// "/sbin/ip netns exec ns-management", and --agent-pubkey to the device
		// pubkey so the SSH-exec'd command is fully self-contained.
		agentBinary        = flag.String("agent-binary", "doublezero-agent", "Path to doublezero-agent on the DUT; the orchestrator SSH-execs this directly.")
		agentCommandPrefix = flag.String("agent-command-prefix", "", "Optional prefix prepended to the agent command (e.g. \"/sbin/ip netns exec ns-management\" on a physical device).")
		agentPubkey        = flag.String("agent-pubkey", "", "When set, appended as `-pubkey <value>` to the agent command. Leave empty when the DUT has a wrapper that injects it locally.")
		agentMetricsAddr   = flag.String("agent-metrics-addr", "", "When set, appended as `-metrics-enable -metrics-addr <value>` so the observer can scrape the agent. Leave empty when the DUT has a wrapper that turns metrics on locally.")
	)
	flag.Parse()

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	if *runID == "" {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return fmt.Errorf("generate run id: %w", err)
		}
		*runID = "run-" + hex.EncodeToString(buf[:])
	}

	if err := os.MkdirAll(*workingDir, 0o755); err != nil {
		return fmt.Errorf("create working dir: %w", err)
	}

	baseIP, err := parseIPv4(*clientIPBase)
	if err != nil {
		return fmt.Errorf("parse --client-ip-base: %w", err)
	}
	tunnelIP, err := parseIPv4(*tunnelEndpoint)
	if err != nil {
		return fmt.Errorf("parse --tunnel-endpoint: %w", err)
	}

	resolved := orchestratorConfig{
		RunID:                         *runID,
		TargetUserCount:               *targetUserCount,
		UsersPerBatch:                 *usersPerBatch,
		HoldSeconds:                   *holdSeconds,
		AgentQuietSeconds:             *agentQuietSeconds,
		AgentQuiescenceTimeoutSeconds: *agentQuiescenceTimeoutSeconds,
		ApplyCatchUpTimeoutSeconds:    *applyCatchUpTimeoutSeconds,
		ApplyPerBatchCatchUp:          *applyPerBatchCatchUp,
		DUTPubkey:                     *dutPubkey,
		DUTSSHHost:                    *dutSSHHost,
		DUTSSHKey:                     *dutSSHKey,
		DUTSSHUser:                    *dutSSHUser,
		RPCURL:                        *rpcURL,
		ProgramID:                     *programID,
		KeypairPath:                   *keypairPath,
		ControllerAddr:                *controllerAddr,
		AbortFile:                     *abortFile,
		WorkingDir:                    *workingDir,
		ClientIPBase:                  *clientIPBase,
		TunnelEndpoint:                *tunnelEndpoint,
		TenantPubkey:                  *tenantPubkey,
		NoAgent:                       *noAgent,
		AgentBinary:                   *agentBinary,
		AgentCommandPrefix:            *agentCommandPrefix,
		AgentPubkey:                   *agentPubkey,
		AgentMetricsAddr:              *agentMetricsAddr,
	}
	// Validate required flags before writing anything, so a bad invocation
	// doesn't leave a config file behind. A dry-run is exempt: its whole job is
	// to dump the resolved config without needing the live-RPC flags.
	if !*dryRun {
		required := map[string]string{
			"--dut-pubkey": *dutPubkey,
			"--rpc-url":    *rpcURL,
			"--program-id": *programID,
			"--keypair":    *keypairPath,
		}
		if !*noAgent {
			// Agent telemetry is required unless explicitly disabled; don't
			// silently degrade to a no-op run that records no pre_commit_log /
			// applied rows.
			required["--dut-ssh-host"] = *dutSSHHost
			required["--dut-ssh-key"] = *dutSSHKey
		}
		if err := requireFlags(required); err != nil {
			return err
		}
	}

	configPath := filepath.Join(*workingDir, "orchestrator-config.json")
	if err := dumpJSON(configPath, resolved); err != nil {
		return fmt.Errorf("write orchestrator-config.json: %w", err)
	}
	logger.Info("orchestrator-config.json written", "path", configPath)

	if *dryRun {
		logger.Info("dry-run: skipping sweep")
		return nil
	}

	dutPK, err := solana.PublicKeyFromBase58(*dutPubkey)
	if err != nil {
		return fmt.Errorf("--dut-pubkey: %w", err)
	}
	programPK, err := solana.PublicKeyFromBase58(*programID)
	if err != nil {
		return fmt.Errorf("--program-id: %w", err)
	}
	signer, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		return fmt.Errorf("load --keypair: %w", err)
	}

	var tenantPK solana.PublicKey
	if *tenantPubkey != "" {
		tenantPK, err = solana.PublicKeyFromBase58(*tenantPubkey)
		if err != nil {
			return fmt.Errorf("--tenant-pubkey: %w", err)
		}
	}

	rpc := solanarpc.New(*rpcURL)
	client := serviceability.New(rpc, programPK)
	executor := serviceability.NewExecutor(logger, rpc, &signer, programPK)

	liveExec, err := exec.New(exec.Config{
		Client:         client,
		Executor:       executor,
		RPC:            rpc,
		DevicePubkey:   dutPK,
		TenantPubkey:   tenantPK,
		ClientIPBase:   baseIP,
		TunnelEndpoint: tunnelIP,
		UserType:       serviceability.UserTypeIBRL,
		CyoaType:       serviceability.CyoaTypeGREOverDIA,
		DzPrefixCount:  1,
	})
	if err != nil {
		return err
	}

	runlogPath := filepath.Join(*workingDir, "orchestrator-runlog.jsonl")
	rlw, err := runlog.Open(runlogPath)
	if err != nil {
		return err
	}
	defer rlw.Close()
	logger.Info("orchestrator-runlog.jsonl open", "path", runlogPath)

	// Compose ctx: signal cancellation + abort-file cancellation.
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()
	ctx, abortCancel := abort.Watch(rootCtx, *abortFile, abort.DefaultPollInterval, logger)
	defer abortCancel()

	agentRunner := selectAgentRunner(*noAgent, *dutSSHHost, *dutSSHKey, *dutSSHUser, *controllerAddr, *workingDir, *agentBinary, *agentCommandPrefix, *agentPubkey, *agentMetricsAddr, logger)

	// With --no-agent there is no agent to emit Applied events, so the
	// catch-up wait would pin to its full timeout on every run. Zero it
	// out automatically rather than asking the operator to remember the
	// matching flag.
	if *noAgent && *applyCatchUpTimeoutSeconds > 0 {
		logger.Info("sweep: --no-agent disables apply catch-up wait", "previous_timeout_seconds", *applyCatchUpTimeoutSeconds)
		*applyCatchUpTimeoutSeconds = 0
	}

	cfg := sweep.Config{
		RunID:                  *runID,
		Target:                 *targetUserCount,
		UsersPerBatch:          *usersPerBatch,
		Hold:                   time.Duration(*holdSeconds) * time.Second,
		AgentQuietWindow:       time.Duration(*agentQuietSeconds) * time.Second,
		AgentQuiescenceTimeout: time.Duration(*agentQuiescenceTimeoutSeconds) * time.Second,
		ApplyCatchUpTimeout:    time.Duration(*applyCatchUpTimeoutSeconds) * time.Second,
		ApplyPerBatchCatchUp:   *applyPerBatchCatchUp,
		NoAgent:                *noAgent,
		OwnerFilter:            signer.PublicKey(),
		Executor:               liveExec,
		Agent:                  agentRunner,
		Runlog:                 rlw,
		Clock:                  sweep.RealClock{},
		Logger:                 logger,
	}

	logger.Info("sweep starting", "target", cfg.Target, "batch", cfg.UsersPerBatch, "hold", cfg.Hold)
	if err := sweep.Run(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warn("sweep cancelled", "err", err)
			return err
		}
		return fmt.Errorf("sweep: %w", err)
	}
	logger.Info("sweep finished")
	return nil
}

func newLogger(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

func dumpJSON(path string, v any) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// Capture the Close error so a flush failure (e.g. a full filesystem) on the
	// buffered JSON isn't swallowed.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func requireFlags(required map[string]string) error {
	var missing []string
	for name, val := range required {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// Sort so the error is deterministic regardless of map iteration order.
		sort.Strings(missing)
		return fmt.Errorf("missing required flag(s): %v", missing)
	}
	return nil
}

// selectAgentRunner picks between the SSH-backed runner and the no-op, based
// on the CLI flags:
//
//   - --no-agent → noop (operator explicitly opted out, e.g. offline testing)
//   - otherwise → SSH runner
//
// The SSH flags are validated as required upstream when --no-agent is unset, so
// this never silently falls back to the no-op runner.
//
// The SSH runner tees remote stdout/stderr into <working-dir>/orchestrator.agent.log.
// The exec'd command is constructed as:
//
//	[<prefix> ]<binary> -verbose[ -pubkey <pk>][ -controller <addr>]
//
// Containerized DUTs leave prefix and pubkey empty and rely on a device-side
// wrapper that injects -pubkey from /etc/doublezero/agent/pubkey and re-execs
// through sudo. Physical DUTs without a wrapper set prefix to e.g.
// "/sbin/ip netns exec ns-management" and pubkey to the device's onchain pubkey
// so the command is self-contained.
func selectAgentRunner(noAgent bool, sshHost, sshKey, sshUser, controllerAddr, workingDir, agentBinary, agentCmdPrefix, agentPubkey, agentMetricsAddr string, logger *slog.Logger) agent.Runner {
	if noAgent {
		logger.Info("agent: --no-agent set; using no-op runner")
		return agent.NewNoop(logger)
	}

	cmd := agentBinary + " -verbose"
	if agentPubkey != "" {
		cmd += " -pubkey " + agentPubkey
	}
	if controllerAddr != "" {
		cmd += " -controller " + controllerAddr
	}
	if agentMetricsAddr != "" {
		cmd += " -metrics-enable -metrics-addr " + agentMetricsAddr
	}
	if agentCmdPrefix != "" {
		cmd = agentCmdPrefix + " " + cmd
	}
	return agent.NewSSH(agent.SSHConfig{
		Host:    sshHost,
		User:    sshUser,
		KeyPath: sshKey,
		Command: cmd,
		LogPath: filepath.Join(workingDir, "orchestrator.agent.log"),
		Logger:  logger,
	})
}

func parseIPv4(s string) ([4]byte, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return [4]byte{}, fmt.Errorf("invalid IPv4 %q", s)
	}
	v4 := ip.To4()
	if v4 == nil {
		return [4]byte{}, fmt.Errorf("not IPv4: %q", s)
	}
	var out [4]byte
	copy(out[:], v4)
	return out, nil
}
