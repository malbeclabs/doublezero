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
	RunID           string `json:"run_id"`
	TargetUserCount int    `json:"target_user_count"`
	UsersPerBatch   int    `json:"users_per_batch"`
	HoldSeconds     int    `json:"hold_seconds"`
	DUTPubkey       string `json:"dut_pubkey"`
	DUTSSHHost      string `json:"dut_ssh_host"`
	DUTSSHKey       string `json:"dut_ssh_key"`
	DUTSSHUser      string `json:"dut_ssh_user"`
	RPCURL          string `json:"rpc_url"`
	ProgramID       string `json:"program_id"`
	KeypairPath     string `json:"keypair"`
	ControllerAddr  string `json:"controller"`
	AbortFile       string `json:"abort_file"`
	WorkingDir      string `json:"working_dir"`
	ClientIPBase    string `json:"client_ip_base"`
	TunnelEndpoint  string `json:"tunnel_endpoint"`
	TenantPubkey    string `json:"tenant_pubkey,omitempty"`
	NoAgent         bool   `json:"no_agent"`
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
		RunID:           *runID,
		TargetUserCount: *targetUserCount,
		UsersPerBatch:   *usersPerBatch,
		HoldSeconds:     *holdSeconds,
		DUTPubkey:       *dutPubkey,
		DUTSSHHost:      *dutSSHHost,
		DUTSSHKey:       *dutSSHKey,
		DUTSSHUser:      *dutSSHUser,
		RPCURL:          *rpcURL,
		ProgramID:       *programID,
		KeypairPath:     *keypairPath,
		ControllerAddr:  *controllerAddr,
		AbortFile:       *abortFile,
		WorkingDir:      *workingDir,
		ClientIPBase:    *clientIPBase,
		TunnelEndpoint:  *tunnelEndpoint,
		TenantPubkey:    *tenantPubkey,
		NoAgent:         *noAgent,
	}
	// Validate required flags before writing anything, so a bad invocation
	// doesn't leave a config file behind. A dry-run is exempt: its whole job is
	// to dump the resolved config without needing the live-RPC flags.
	if !*dryRun {
		if err := requireFlags(map[string]string{
			"--dut-pubkey": *dutPubkey,
			"--rpc-url":    *rpcURL,
			"--program-id": *programID,
			"--keypair":    *keypairPath,
		}); err != nil {
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

	agentRunner := selectAgentRunner(*noAgent, *dutSSHHost, *dutSSHKey, *dutSSHUser, *controllerAddr, *workingDir, logger)

	cfg := sweep.Config{
		RunID:         *runID,
		Target:        *targetUserCount,
		UsersPerBatch: *usersPerBatch,
		Hold:          time.Duration(*holdSeconds) * time.Second,
		OwnerFilter:   signer.PublicKey(),
		Executor:      liveExec,
		Agent:         agentRunner,
		Runlog:        rlw,
		Clock:         sweep.RealClock{},
		Logger:        logger,
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
//   - --no-agent → noop (operator opted out)
//   - --dut-ssh-host + --dut-ssh-key set → SSH runner (default for live runs)
//   - otherwise → noop with a warning (operator forgot the flags)
//
// The SSH runner tees remote stdout/stderr into <working-dir>/orchestrator.agent.log.
// The exec'd command appends --controller iff the operator passed --controller.
func selectAgentRunner(noAgent bool, sshHost, sshKey, sshUser, controllerAddr, workingDir string, logger *slog.Logger) agent.Runner {
	if noAgent {
		logger.Info("agent: --no-agent set; using no-op runner")
		return agent.NewNoop(logger)
	}
	if sshHost == "" || sshKey == "" {
		logger.Warn("agent: --dut-ssh-host and --dut-ssh-key not both set; falling back to no-op runner (pre_commit_log / applied events will not be recorded)")
		return agent.NewNoop(logger)
	}

	cmd := "doublezero-agent -verbose"
	if controllerAddr != "" {
		cmd = fmt.Sprintf("doublezero-agent -verbose -controller %s", controllerAddr)
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
