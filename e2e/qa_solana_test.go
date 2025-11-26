//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type validtorOnDZ struct {
	ValidatorPK solana.PublicKey
	User        *qa.User
	Device      *qa.Device
	IP          net.IP
	TPUQUICPort uint16
}

func TestQA_SolanaConnectivity(t *testing.T) {
	solanaEnvFlag := flag.String("solana-env", config.SolanaEnvMainnetBeta, "environment to run in (devnet, testnet, mainnet-beta)")
	solanaEnv := *solanaEnvFlag
	if solanaEnv == "" {
		t.Fatalf("The -solana-env flag is required. Must be one of: devnet, testnet, mainnet-beta")
	}
	solanaNetworkConfig, err := config.SolanaNetworkConfigForEnv(solanaEnv)
	require.NoError(t, err, "failed to get solana network config")

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	// Disconnect all clients on cleanup.
	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, client := range clients {
			wg.Add(1)
			go func(client *qa.Client) {
				defer wg.Done()
				err := client.DisconnectUser(context.Background(), true, true)
				assert.NoError(t, err, "failed to disconnect user")
			}(client)
		}
		wg.Wait()
	})

	// Filter devices to only include those with sufficient capacity and skip test devices, and
	// shuffle them to avoid always testing connectivity via the same devices.
	validDevices := test.ShuffledValidDevices(2)
	if len(validDevices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
	}

	// Select devices for the number of clients, on different exchanges.
	usedExchanges := make(map[string]struct{})
	devices := make([]*qa.Device, 0, len(clients))
	for _, device := range validDevices {
		if _, ok := usedExchanges[device.ExchangeCode]; ok {
			continue
		}
		usedExchanges[device.ExchangeCode] = struct{}{}
		devices = append(devices, device)
		if len(devices) == len(clients) {
			break
		}
	}
	if len(devices) < len(clients) {
		t.Fatalf("Expected at least %d devices, got %d", len(clients), len(devices))
	}

	// Connect each client to its assigned device.
	for i, client := range clients {
		err := client.ConnectUserUnicast_NoWait(ctx, devices[i].Code)
		require.NoError(t, err, "failed to connect user %s to device %s", client.Host, devices[i].Code)
	}

	// Wait for status of all users to be up.
	for _, client := range clients {
		err := client.WaitForStatusUp(ctx)
		require.NoError(t, err, "failed to wait for status")
	}

	// Get IPs and TPU ports of all Solana validators that are in the leader schedule.
	solanaRPC := solanarpc.New(solanaNetworkConfig.RPCURL)
	leaderScheduleRes, err := solanaRPC.GetLeaderSchedule(ctx)
	require.NoError(t, err, "failed to get leader schedule")
	validatorPKs := map[solana.PublicKey]struct{}{}
	for validatorPK := range leaderScheduleRes {
		validatorPKs[validatorPK] = struct{}{}
	}
	gossipNodesRes, err := solanaRPC.GetClusterNodes(ctx)
	require.NoError(t, err, "failed to get cluster nodes")
	validators := map[solana.PublicKey]*solanarpc.GetClusterNodesResult{}
	for _, node := range gossipNodesRes {
		if _, ok := validatorPKs[node.Pubkey]; ok && node.TPUQUIC != nil {
			validators[node.Pubkey] = node
		}
	}

	// Find validators on DZ.
	usersByIP := make(map[string]*qa.User)
	for _, user := range test.Users() {
		if user.DZIP.To4() == nil || user.UserType != serviceability.UserTypeIBRL {
			continue
		}
		usersByIP[user.DZIP.To4().String()] = user
	}
	validatorsOnDZByIP := make(map[string]validtorOnDZ)
	for _, node := range validators {
		if node.TPUQUIC == nil {
			continue
		}
		ip, port, err := net.SplitHostPort(*node.TPUQUIC)
		require.NoError(t, err, "failed to split host port")
		tpuQUICPort, err := strconv.ParseUint(port, 10, 16)
		require.NoError(t, err, "failed to parse port")
		if user, ok := usersByIP[ip]; ok {
			validatorsOnDZByIP[ip] = validtorOnDZ{
				ValidatorPK: node.Pubkey,
				User:        user,
				Device:      user.Device,
				IP:          net.ParseIP(ip),
				TPUQUICPort: uint16(tpuQUICPort),
			}
		}
	}
	log.Info("Found validators on DZ", "count", len(validatorsOnDZByIP))

	waitForRoutesCfg := &qa.WaitConfig{
		// With this timeout, we are going to sometimes skip tests if BGP is damping the client
		// and slowing down route installation, but we accept that for faster test runtime.
		Timeout:  65 * time.Second,
		Interval: 3 * time.Second,
	}
	publicInternetPingCount := 1
	publicInternetPingInterval := 500 * time.Millisecond
	publicInternetPingTimeout := 3 * time.Second

	// Test TPU QUIC connectivity for all Solana validators on DZ.
	var wg sync.WaitGroup
	var sem chan struct{}
	sem = make(chan struct{}, 64)
	defer close(sem)
	var failures []solanaTPUQUICConnectivityFailure
	for _, validator := range validatorsOnDZByIP {
		wg.Add(1)
		go func(validator validtorOnDZ) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Wait for route to be installed for the target validator with a 75 second timeout,
			// ignoring if not found within that time. We assume the validator is no longer running.
			for _, c := range clients {
				err := c.WaitForRoutesWith(ctx, []net.IP{validator.IP.To4()}, waitForRoutesCfg)
				if err != nil {
					log.Info("Validator route not found, ignoring", "host", c.Host, "pk", validator.ValidatorPK, "ip", validator.IP, "port", validator.TPUQUICPort, "error", err)
					return
				}
			}

			tpuQUICAddr := fmt.Sprintf("%s:%d", validator.IP.String(), validator.TPUQUICPort)

			// First test over the public internet from the QA runner.
			pingRes, err := tpuquic.Ping(tpuquic.PingConfig{
				Context:  ctx,
				Dst:      tpuQUICAddr,
				Quiet:    true,
				Count:    publicInternetPingCount,
				Interval: publicInternetPingInterval,
				Timeout:  publicInternetPingTimeout,
			})
			require.NoError(t, err, "failed to test TPU QUIC connectivity over public internet")

			// If the ping fails over the public internet, ignore the validator.
			if !pingRes.Success {
				log.Info("Validator TPU QUIC not reachable over public internet, ignoring", "pk", validator.ValidatorPK, "ip", validator.IP, "port", validator.TPUQUICPort)
				return
			}

			// Then test over DZ from each QA client.
			// var testErr string
			// var testErrMu sync.Mutex
			// var wg sync.WaitGroup
			for _, client := range clients {
				// wg.Add(1)
				// go func(client *qa.Client) {
				// 	defer wg.Done()

				log.Debug("Testing TPU QUIC connectivity over DZ", "from", client.Host, "to", tpuQUICAddr, "pk", validator.ValidatorPK, "ip", validator.IP, "port", validator.TPUQUICPort)
				res, err := client.TestSolanaTPUQUICConnectivity(ctx, tpuQUICAddr)
				require.NoError(t, err, "failed to test TPU QUIC connectivity")
				// if res.Error != "" {
				// 	testErrMu.Lock()
				// 	testErr = res.Error
				// 	testErrMu.Unlock()
				// 	return
				// }
				if res.Error != "" {
					failures = append(failures, solanaTPUQUICConnectivityFailure{
						ValidatorPK: validator.ValidatorPK,
						IP:          validator.IP,
						Port:        validator.TPUQUICPort,
						Device:      validator.Device,
						Reason:      res.Error,
					})
					log.Error("TPU QUIC connectivity over DZ failed", "to", tpuQUICAddr, "pk", validator.ValidatorPK.String(), "ip", validator.IP.String(), "port", validator.TPUQUICPort, "device", validator.Device.Code, "error", res.Error)
					t.Fatalf("validator %s (via %s) TPU QUIC not reachable over DZ", validator.ValidatorPK.String(), validator.Device.Code)
					return
				}
				// require.Empty(t, res.Error, "failed to test TPU QUIC connectivity")
				log.Info("TPU QUIC connectivity over DZ successful", "from", client.Host, "to", tpuQUICAddr, "pk", validator.ValidatorPK, "ip", validator.IP, "port", validator.TPUQUICPort)
				// }(client)
			}
			// wg.Wait()

			// if testErr != "" {
			// 	// If it fails over DZ, check again over the public internet to mitigate race conditions
			// 	// where the validator stopped responding between the two tests.
			// 	// In this case we expect the ping to fail over the public internet.
			// 	pingRes, err = tpuquic.Ping(tpuquic.PingConfig{
			// 		Context:  ctx,
			// 		Dst:      tpuQUICAddr,
			// 		Quiet:    true,
			// 		Duration: publicInternetPingDuration,
			// 		Interval: publicInternetPingInterval,
			// 		Timeout:  publicInternetPingTimeout,
			// 	})
			// 	require.NoError(t, err, "failed to test TPU QUIC connectivity over DZ")
			// 	if pingRes.Success {
			// 		log.Error("TPU QUIC connectivity over DZ failed", "to", tpuQUICAddr, "pk", validator.ValidatorPK.String(), "ip", validator.IP.String(), "port", validator.TPUQUICPort, "device", validator.Device.Code, "error", testErr)
			// 		failures = append(failures, solanaTPUQUICConnectivityFailure{
			// 			ValidatorPK: validator.ValidatorPK,
			// 			IP:          validator.IP,
			// 			Port:        validator.TPUQUICPort,
			// 			Device:      validator.Device,
			// 			Reason:      testErr,
			// 		})
			// 		require.Fail(t, "validator TPU QUIC not reachable over DZ", "error", testErr, "pk", validator.ValidatorPK.String(), "ip", validator.IP.String(), "port", validator.TPUQUICPort, "device", validator.Device.Code)
			// 	}
			// }
		}(validator)
	}
	wg.Wait()

	if len(failures) > 0 {
		t.Logf("=== Summary of Solana TPU QUIC Connectivity Failures ===")
		for _, f := range failures {
			t.Logf("FAIL %s (via %s): %s ", f.ValidatorPK.String(), f.Device.Code, f.Reason)
		}
	}
}

type solanaTPUQUICConnectivityFailure struct {
	ValidatorPK solana.PublicKey
	IP          net.IP
	Port        uint16
	Device      *qa.Device
	Reason      string
}
