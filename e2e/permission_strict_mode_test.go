//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	e2esolana "github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// notAllowed is how the CLI prints DoubleZeroError::NotAllowed (custom program error 8), the
// error authorize() returns when no Permission account grants a requested flag.
const notAllowed = "not allowed to execute this action"

// requirePermissionAccountsBit is FeatureFlag::RequirePermissionAccounts (bit 1).
const requirePermissionAccountsBit = 1 << 1

// TestE2E_PermissionAccounts_StrictMode runs the permission accounts rollout end to end with
// `require-permission-accounts` on: provision Permission accounts from `permission audit`, enable
// strict mode, and check that every kind of signer keeps working through its Permission account
// alone while the GlobalState allowlist and authority keys stop granting anything.
//
// The signers are the ones that write to the ledger in production:
//   - the CLI / Rust SDK (the manager key, a foundation allowlist member);
//   - the Go SDK executor (as the device health oracle uses it);
//   - the sentinel, which builds its instructions with the instruction crate.
//
// Strict mode is toggled by a key outside the foundation allowlist whose only grant is
// globalstate-admin, so the toggle goes through authorize() and not the foundation recovery path.
//
// It also covers the two recovery paths the rollout depends on: a foundation allowlist member
// with no Permission account can still manage Permission accounts and turn strict mode off.
//
// Everything shares one devnet with no cEOS devices: devices and links only need to exist
// onchain for authorization to be exercised.
func TestE2E_PermissionAccounts_StrictMode(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	deployDir := t.TempDir()
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: deployDir,
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: filepath.Join(currentDir, "data", "serviceability-program-keypair.json"),
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()
	err = dn.Start(ctx, nil)
	t.Cleanup(func() { _ = dn.Destroy(context.Background(), false) })
	require.NoError(t, err)

	t.Cleanup(func() {
		if t.Failed() && dn.Sentinel != nil && dn.Sentinel.ContainerID != "" {
			dn.DumpContainerLogs(t, "sentinel", dn.Sentinel.ContainerID)
		}
	})

	// exec runs a shell command in the manager container, signing with keypair (a path inside the
	// container) or with the manager key when keypair is empty. Output includes stderr.
	exec := func(keypair, cmd string) (string, error) {
		script := "set -euo pipefail\n"
		if keypair != "" {
			script += "export DOUBLEZERO_KEYPAIR=" + keypair + "\n"
		}
		out, err := dn.Manager.Exec(ctx, []string{"bash", "-c", script + cmd + " 2>&1"}, docker.NoPrintOnError())
		return string(out), err
	}
	run := func(t *testing.T, keypair, cmd string) string {
		t.Helper()
		out, err := exec(keypair, cmd)
		require.NoError(t, err, "command failed: %s\noutput: %s", cmd, out)
		return out
	}
	deny := func(t *testing.T, keypair, cmd string) {
		t.Helper()
		out, err := exec(keypair, cmd)
		require.Error(t, err, "command should have been denied: %s\noutput: %s", cmd, out)
		require.Contains(t, out, notAllowed, "command failed for another reason: %s", cmd)
	}
	fund := func(t *testing.T, pubkey string) {
		t.Helper()
		run(t, "", "solana airdrop 100 "+pubkey+" >/dev/null")
	}

	// cliKey creates a funded keypair inside the manager container for CLI use.
	cliKey := func(t *testing.T, name string) (string, string) {
		t.Helper()
		path := "/tmp/" + name + ".json"
		run(t, "", "solana-keygen new --no-bip39-passphrase --force -o "+path+" >/dev/null")
		pubkey := strings.TrimSpace(run(t, "", "solana address -k "+path))
		fund(t, pubkey)
		return path, pubkey
	}

	// hostKey creates a funded keypair on the host, for signers that run outside the manager.
	hostKey := func(t *testing.T, name string) (string, solana.PrivateKey) {
		t.Helper()
		keypairJSON, err := e2esolana.GenerateKeypairJSON()
		require.NoError(t, err)
		path := filepath.Join(deployDir, name+".json")
		require.NoError(t, os.WriteFile(path, keypairJSON, 0o600))
		key, err := solana.PrivateKeyFromSolanaKeygenFile(path)
		require.NoError(t, err)
		fund(t, key.PublicKey().String())
		return path, key
	}

	strictMode := func(t *testing.T) bool {
		t.Helper()
		var flags struct {
			Raw uint64 `json:"raw"`
		}
		require.NoError(t, json.Unmarshal([]byte(run(t, "", "doublezero global-config feature-flags get --json")), &flags))
		return flags.Raw&requirePermissionAccountsBit != 0
	}
	setStrictMode := func(t *testing.T, keypair string, enable bool) {
		t.Helper()
		toggle := "--disable"
		if enable {
			toggle = "--enable"
		}
		run(t, keypair, "doublezero global-config feature-flags set "+toggle+" require-permission-accounts")
		require.Equal(t, enable, strictMode(t))
	}

	type gap struct {
		Key          string `json:"key"`
		Flag         string `json:"flag"`
		LegacySource string `json:"legacy_source"`
	}
	type auditReport struct {
		StrictModeEnabled bool  `json:"strict_mode_enabled"`
		Gaps              []gap `json:"gaps"`
	}
	// audit runs `permission audit`, which exits non-zero while there are gaps.
	audit := func(t *testing.T) auditReport {
		t.Helper()
		out, _ := dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero permission audit --json 2>/dev/null"}, docker.NoPrintOnError())
		var report auditReport
		require.NoError(t, json.Unmarshal(out, &report), "unparseable audit output: %s", out)
		return report
	}

	managerPubkey := dn.Manager.Pubkey
	programID := solana.MustPublicKeyFromBase58(dn.Manager.ServiceabilityProgramID)

	// Dedicated keys for the services, so each one authorizes only through its own grants.
	sentinelKeyPath, sentinelKey := hostKey(t, "sentinel")
	_, healthOracleKey := hostKey(t, "health-oracle")
	sentinelPubkey := sentinelKey.PublicKey().String()
	healthOraclePubkey := healthOracleKey.PublicKey().String()

	// CLI keys for the negative and recovery cases.
	noPDAPath, noPDAPubkey := cliKey(t, "no-pda")                // no Permission account
	infraOnlyPath, infraOnlyPubkey := cliKey(t, "infra-only")    // a Permission account with infra-admin only
	breakGlassPath, breakGlassPubkey := cliKey(t, "break-glass") // foundation allowlist member, no Permission account
	// Toggles strict mode. Not a foundation allowlist member, so it cannot use the recovery path.
	globalAdminPath, globalAdminPubkey := cliKey(t, "globalstate-admin")

	// Phase 1 of the rollout plan: with strict mode still off, give every legacy key a Permission
	// account covering what the audit says it would lose.
	if !t.Run("provision_from_audit", func(t *testing.T) {
		run(t, "", "doublezero global-config authority set --sentinel-authority "+sentinelPubkey+" --health-oracle "+healthOraclePubkey)

		report := audit(t)
		require.False(t, report.StrictModeEnabled)

		byKey := map[string][]string{}
		for _, g := range report.Gaps {
			byKey[g.Key] = append(byKey[g.Key], g.Flag)
		}
		require.Subset(t, byKey[sentinelPubkey], []string{"access-pass-admin", "multicast-admin", "tenant-admin"})
		require.Equal(t, []string{"health-oracle"}, byKey[healthOraclePubkey])
		require.Contains(t, byKey[managerPubkey], "network-admin", "the manager is a foundation allowlist member")

		for key, flags := range byKey {
			cmd := "doublezero permission set --user-payer " + key
			for _, flag := range grantableFlags(flags) {
				cmd += " --add " + flag
			}
			// The manager signs every grant, its own included: creating your own Permission
			// account is allowed, only modifying an existing one is not.
			run(t, "", cmd)
		}
		run(t, "", "doublezero permission set --user-payer "+infraOnlyPubkey+" --add infra-admin")
		run(t, "", "doublezero permission set --user-payer "+globalAdminPubkey+" --add globalstate-admin")

		report = audit(t)
		require.Empty(t, report.Gaps, "every legacy key must be covered before strict mode is enabled")
	}) {
		t.FailNow()
	}

	// Phase 3: enable strict mode. The plan checks the rollback path first. A foundation member
	// could disable strict mode without a Permission account, so a non-member signs every toggle.
	if !t.Run("enable_and_roll_back_through_permission_account", func(t *testing.T) {
		setStrictMode(t, globalAdminPath, true)
		setStrictMode(t, globalAdminPath, false)
		setStrictMode(t, globalAdminPath, true)
		require.True(t, audit(t).StrictModeEnabled)
	}) {
		t.FailNow()
	}

	var multicastGroupPK string

	// The manager now authorizes only through its Permission account. One write per domain that
	// goes through authorize().
	if !t.Run("cli_admin_flow", func(t *testing.T) {
		// globalstate-admin
		run(t, "", "doublezero global-config allowlist add --pubkey "+breakGlassPubkey)

		// infra-admin, contributor-admin
		run(t, "", `doublezero location create --code psloc --name "Strict Loc" --country US --lat 40.7 --lng -74.0`)
		run(t, "", `doublezero location update --pubkey psloc --name "Strict Loc 2"`)
		run(t, "", `doublezero exchange create --code psexc --name "Strict Exchange" --lat 40.7 --lng -74.0`)
		run(t, "", `doublezero exchange update --pubkey psexc --name "Strict Exchange 2"`)
		run(t, "", "doublezero contributor create --code psco --owner me")

		// network-admin: devices, interfaces, a link.
		_, err := dn.GetOrCreateDeviceOnchain(ctx, "dev01", "ams", "xams", "", "45.33.100.1",
			[]string{"45.33.100.8/29", "45.33.101.0/29"}, "mgmt")
		require.NoError(t, err)
		_, err = dn.GetOrCreateDeviceOnchain(ctx, "dev02", "lax", "xlax", "", "45.33.102.1",
			[]string{"45.33.102.8/29"}, "mgmt")
		require.NoError(t, err)
		// A second user tunnel endpoint, for the sentinel's multicast publishers.
		run(t, "", "doublezero device interface create dev01 Loopback100 --ip-net 45.33.101.1/32 --user-tunnel-endpoint true --bandwidth 10G")
		for _, device := range []string{"dev01", "dev02"} {
			run(t, "", "doublezero device interface create "+device+" Ethernet1 --bandwidth 10G --mtu 9000")
			run(t, "", "doublezero device interface update "+device+" Ethernet1 --status unlinked")
		}
		run(t, "", `doublezero link create wan --code pslink --contributor co01 --side-a dev01 --side-a-interface Ethernet1 --side-z dev02 --side-z-interface Ethernet1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 1 --jitter-ms 0.01`)

		// topology-admin
		run(t, "", "doublezero link topology create --name pstopo --constraint include-any")
		run(t, "", "doublezero link topology delete --name pstopo")

		// tenant-admin
		run(t, "", "doublezero tenant create --code pstenant")
		run(t, "", "doublezero tenant delete --pubkey pstenant")

		// multicast-admin
		run(t, "", "doublezero multicast group create --code psmc --max-bandwidth 1Gbps --owner me -w")
		run(t, "", "doublezero multicast group update --pubkey psmc --max-bandwidth 2Gbps")
		var group struct {
			Account string `json:"account"`
		}
		require.NoError(t, json.Unmarshal([]byte(run(t, "", "doublezero multicast group get --code psmc --json")), &group))
		require.NotEmpty(t, group.Account)
		multicastGroupPK = group.Account

		// access-pass-admin and user-admin: a full user lifecycle. Create and delete by the owner
		// skip the user-admin check; update always requires it.
		const ip = "45.33.100.12"
		userPK := "$(doublezero user list | grep '" + ip + " ' | awk '{print $1}')"
		run(t, "", "doublezero access-pass set --accesspass-type prepaid --client-ip "+ip+" --user-payer me")
		run(t, "", "doublezero user create --device dev01 --client-ip "+ip)
		run(t, "", "doublezero user update --pubkey "+userPK+" --validator-pubkey "+noPDAPubkey)
		run(t, "", "doublezero user delete --accesspass-type prepaid --pubkey "+userPK)
		require.NoError(t, poll.Until(ctx, func() (bool, error) {
			out, err := exec("", "doublezero user list")
			return err == nil && !strings.Contains(out, ip+" "), nil
		}, 60*time.Second, 2*time.Second), "user was not removed")
		run(t, "", "doublezero access-pass close --accesspass-type prepaid --pubkey $(doublezero access-pass list | grep '"+ip+" ' | awk '{print $1}')")

		// The users the sentinel turns into multicast publishers.
		for _, ip := range sentinelUserIPs {
			run(t, "", "doublezero access-pass set --client-ip "+ip+" --user-payer me")
			run(t, "", "doublezero user create --device dev01 --client-ip "+ip)
		}
	}) {
		t.FailNow()
	}

	// The Go SDK executor, signing as the health oracle does. Its key is still health_oracle_pk,
	// so legacy authorization would accept it; suspending its Permission account shows that only
	// the Permission account counts.
	t.Run("go_executor_health_oracle", func(t *testing.T) {
		rpcClient := &sendErrRecorder{Client: dn.Ledger.GetRPCClient()}
		executor := serviceability.NewExecutor(log, rpcClient, &healthOracleKey, programID)
		globalState, _, err := serviceability.GetGlobalStatePDA(programID)
		require.NoError(t, err)
		devicePK, err := dn.GetDevicePubkeyOnchain(ctx, "dev02")
		require.NoError(t, err)
		device := solana.MustPublicKeyFromBase58(devicePK)

		setHealth := func(health serviceability.DeviceHealth) error {
			_, err := executor.SetDeviceHealthBatch(ctx, []serviceability.DeviceHealthUpdate{
				{DevicePubkey: device, Health: health},
			}, globalState)
			return err
		}
		requireHealth := func(want serviceability.DeviceHealth) {
			client, err := dn.Ledger.GetServiceabilityClient()
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				data, err := client.GetProgramData(ctx)
				if err != nil {
					return false
				}
				i := slices.IndexFunc(data.Devices, func(d serviceability.Device) bool { return d.Code == "dev02" })
				return i >= 0 && data.Devices[i].DeviceHealth == want
			}, 30*time.Second, time.Second, "dev02 health should be %s", want)
		}

		// The executor's preflight simulates against finalized state, so wait for a status
		// change to finalize before signing, or the simulation still sees the old status.
		permissionPDA, _, err := serviceability.GetPermissionPDA(programID, healthOracleKey.PublicKey())
		require.NoError(t, err)
		waitFinalized := func(want serviceability.PermissionStatus) {
			require.Eventually(t, func() bool {
				info, err := rpcClient.GetAccountInfoWithOpts(ctx, permissionPDA, &solanarpc.GetAccountInfoOpts{
					Commitment: solanarpc.CommitmentFinalized,
				})
				if err != nil || info == nil || info.Value == nil {
					return false
				}
				var p serviceability.Permission
				serviceability.DeserializePermission(serviceability.NewByteReader(info.Value.Data.GetBinary()), &p)
				return p.Status == want
			}, 60*time.Second, time.Second, "permission status %d was not finalized", want)
		}

		waitFinalized(serviceability.PermissionStatusActivated)
		require.NoError(t, setHealth(serviceability.DeviceHealthReadyForLinks))
		requireHealth(serviceability.DeviceHealthReadyForLinks)

		run(t, "", "doublezero permission suspend --user-payer "+healthOraclePubkey)
		waitFinalized(serviceability.PermissionStatusSuspended)
		rpcClient.lastErr = nil
		require.ErrorIs(t, setHealth(serviceability.DeviceHealthReadyForUsers), serviceability.ErrAllUpdatesFailed,
			"a suspended Permission account must not authorize, even for health_oracle_pk")
		requireNotAllowed(t, rpcClient.lastErr)
		requireHealth(serviceability.DeviceHealthReadyForLinks)

		run(t, "", "doublezero permission resume --user-payer "+healthOraclePubkey)
		waitFinalized(serviceability.PermissionStatusActivated)
		require.NoError(t, setHealth(serviceability.DeviceHealthReadyForUsers))
		requireHealth(serviceability.DeviceHealthReadyForUsers)
	})

	// The sentinel issues an access pass, adds a publisher allowlist entry and creates a multicast
	// user for each IBRL validator, all signed with its own key.
	t.Run("sentinel", func(t *testing.T) {
		require.NotEmpty(t, multicastGroupPK)
		require.NoError(t, dn.StartValidatorMetadataServiceMock(ctx))
		require.NoError(t, dn.ValidatorMetadataServiceMock.SetValidators(ctx, []devnet.ValidatorMetadataItem{
			{IP: sentinelUserIPs[0], ActiveStake: 2000_000_000_000, VoteAccount: "vote1", SoftwareClient: "agave", SoftwareVersion: "1.0.0"},
			{IP: sentinelUserIPs[1], ActiveStake: 1000_000_000_000, VoteAccount: "vote2", SoftwareClient: "agave", SoftwareVersion: "1.0.0"},
		}))

		dn.Spec.Sentinel.KeypairPath = sentinelKeyPath
		dn.Spec.Sentinel.MulticastGroupPubkeys = multicastGroupPK
		dn.Spec.Sentinel.MulticastPublisherPollSecs = 5
		require.NoError(t, dn.StartSentinel(ctx))

		err := poll.Until(ctx, func() (bool, error) {
			out, err := exec("", "doublezero user list --user-type Multicast --all-tenants --json")
			if err != nil {
				return false, nil
			}
			var users []userListEntry
			if err := json.Unmarshal([]byte(out), &users); err != nil {
				return false, nil
			}
			activated := 0
			for _, u := range users {
				if u.Status == "Activated" && slices.Contains(u.publishersList(), multicastGroupPK) {
					activated++
				}
			}
			return activated >= len(sentinelUserIPs), nil
		}, 2*time.Minute, 5*time.Second)
		require.NoError(t, err, "the sentinel did not create its multicast publishers under strict mode")
	})

	t.Run("denials", func(t *testing.T) {
		// No Permission account at all.
		deny(t, noPDAPath, `doublezero location create --code psno --name "No PDA" --country US --lat 1 --lng 1`)

		// A Permission account authorizes only the flags it carries.
		run(t, infraOnlyPath, `doublezero location create --code psinf --name "Infra Only" --country US --lat 1 --lng 1`)
		deny(t, infraOnlyPath, "doublezero tenant create --code psinf")
		deny(t, infraOnlyPath, "doublezero permission set --user-payer "+noPDAPubkey+" --add infra-admin")
		deny(t, infraOnlyPath, "doublezero global-config feature-flags set --disable require-permission-accounts")

		// Being on the foundation allowlist no longer grants admin rights.
		deny(t, breakGlassPath, `doublezero location create --code psbg --name "Break Glass" --country US --lat 1 --lng 1`)
	})

	// The two recovery paths a foundation allowlist member keeps without a Permission account.
	t.Run("foundation_recovery", func(t *testing.T) {
		run(t, breakGlassPath, "doublezero permission set --user-payer "+noPDAPubkey+" --add infra-admin")
		run(t, noPDAPath, `doublezero location create --code psno --name "Granted" --country US --lat 1 --lng 1`)

		setStrictMode(t, breakGlassPath, false)
	})
}

// sentinelUserIPs are the IBRL users the sentinel turns into multicast publishers.
var sentinelUserIPs = []string{"45.33.100.9", "45.33.100.10"}

// grantableFlags turns the flags `permission audit` reports for one key into `permission set
// --add` arguments. An any-of entry such as "foundation|feed-authority" is satisfied by one of its
// flags; the narrowest one is granted, never foundation.
func grantableFlags(flags []string) []string {
	var out []string
	for _, flag := range flags {
		options := strings.Split(flag, "|")
		choice := options[0]
		for _, option := range options {
			if option != "foundation" {
				choice = option
				break
			}
		}
		if !slices.Contains(out, choice) {
			out = append(out, choice)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		panic(fmt.Sprintf("no grantable flag in %v", flags))
	}
	return out
}

// sendErrRecorder keeps the last error from sending a transaction. The executor logs a batch
// failure but returns only ErrAllUpdatesFailed, which says nothing about the reason.
type sendErrRecorder struct {
	*solanarpc.Client
	lastErr error
}

func (r *sendErrRecorder) SendTransactionWithOpts(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
	sig, err := r.Client.SendTransactionWithOpts(ctx, tx, opts)
	if err != nil {
		r.lastErr = err
	}
	return sig, err
}

// requireNotAllowed checks that a preflight rejection is NotAllowed (custom error 8) from the
// only instruction in the transaction.
func requireNotAllowed(t *testing.T, err error) {
	t.Helper()
	var rpcErr *jsonrpc.RPCError
	require.True(t, errors.As(err, &rpcErr), "expected a preflight rejection, got %v", err)
	data, _ := json.Marshal(rpcErr.Data)
	var sim struct {
		Err struct {
			InstructionError []json.RawMessage `json:"InstructionError"`
		} `json:"err"`
		Logs []string `json:"logs"`
	}
	require.NoError(t, json.Unmarshal(data, &sim), "unexpected RPC error data: %s", data)
	require.Len(t, sim.Err.InstructionError, 2, "not an instruction error: %s", data)
	require.JSONEq(t, `0`, string(sim.Err.InstructionError[0]))
	require.JSONEq(t, `{"Custom": 8}`, string(sim.Err.InstructionError[1]))
	require.True(t, slices.ContainsFunc(sim.Logs, func(l string) bool {
		return strings.HasSuffix(l, "failed: custom program error: 0x8")
	}), "missing NotAllowed failure log: %v", sim.Logs)
}
