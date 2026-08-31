package qa

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
	"google.golang.org/protobuf/types/known/emptypb"
)

// seatReadTimeout/seatReadInterval bound the poll-until-visible window for
// reading post-write seat state from a possibly-lagging RPC node.
const (
	seatReadTimeout  = 30 * time.Second
	seatReadInterval = 2 * time.Second
)

// withdrawRetryTimeout/withdrawRetryInterval bound the cleanup/self-heal
// withdraw retry loop. The spurious "request in flight" preflight bail (a stale
// RPC read of a just-closed InstantSeatAllocationRequest) and transient RPC
// failures both clear within a minute or two, so a few attempts over this
// window heal them without a single-shot failure poisoning every future run.
const (
	withdrawRetryTimeout  = 2 * time.Minute
	withdrawRetryInterval = 15 * time.Second
)

// seatHealPollTimeout bounds how long SelfHealStuckSeats waits for a withdrawn
// seat to read TenureEpochs == 0 (or vanish) onchain.
const seatHealPollTimeout = 2 * time.Minute

// currentSolanaRPCURL returns the pool's current endpoint URL, falling back to
// the static SolanaRPCURL field for callers constructed without a pool (e.g.
// hand-built test clients).
func (c *Client) currentSolanaRPCURL() string {
	if c.solanaRPC != nil {
		return c.solanaRPC.CurrentURL()
	}
	return c.SolanaRPCURL
}

// scrubRPCErr redacts any endpoint credential embedded in an RPC error string
// (solana-go embeds the full request URL, which may carry an API key, in its
// connectivity/HTTP error messages). Returns the plain error string when there
// is no pool to source endpoint URLs from.
func (c *Client) scrubRPCErr(err error) string {
	if err == nil {
		return ""
	}
	if c.solanaRPC != nil {
		return c.solanaRPC.scrubErr(err)
	}
	return err.Error()
}

// shredsClient builds a shred-subscription client backed by the failover RPC
// pool when present, so reads transparently fail over a dead or lagging
// endpoint. Falls back to a single-endpoint client for hand-built test clients.
func (c *Client) shredsClient(programID solana.PublicKey) *shreds.Client {
	if c.solanaRPC != nil {
		return shreds.New(c.solanaRPC.RPC(), programID)
	}
	return shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID)
}

// FeedEnable calls the FeedEnable RPC to start the doublezerod reconciler.
func (c *Client) FeedEnable(ctx context.Context) error {
	c.log.Debug("Enabling reconciler", "host", c.Host)
	resp, err := c.grpcClient.FeedEnable(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to enable reconciler on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("enable failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Reconciler enabled", "host", c.Host)
	return nil
}

// ClosestDevice returns the reachable device with the lowest average latency.
// It calls GetLatency and looks up the result in the client's devices map.
func (c *Client) ClosestDevice(ctx context.Context) (*Device, error) {
	latencies, err := c.GetLatency(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latency on host %s: %w", c.Host, err)
	}

	var bestLatency *pb.Latency
	var bestAvg uint64 = math.MaxUint64
	for _, l := range latencies {
		if !l.Reachable {
			continue
		}
		if l.AvgLatencyNs < bestAvg {
			bestAvg = l.AvgLatencyNs
			bestLatency = l
		}
	}
	if bestLatency == nil {
		return nil, fmt.Errorf("no reachable devices found on host %s", c.Host)
	}

	// Look up device by code in the devices map.
	device, ok := c.devices[bestLatency.DeviceCode]
	if !ok {
		return nil, fmt.Errorf("closest device %q (pk=%s) not found in devices map on host %s", bestLatency.DeviceCode, bestLatency.DevicePk, c.Host)
	}

	c.log.Debug("Determined closest device", "host", c.Host, "deviceCode", device.Code, "avgLatencyNs", bestAvg)
	return device, nil
}

// RetransmitOnlyExchangeKeys returns the set of metro exchange pubkeys (base58)
// whose MetroHistory has the retransmit-only flag set. A retransmit-only metro
// serves the retransmit multicast group only to every device in it. This is one
// batched read of all MetroHistory accounts.
func (c *Client) RetransmitOnlyExchangeKeys(ctx context.Context) (map[string]bool, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	metros, err := c.shredsClient(programID).FetchAllMetroHistories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metro histories on host %s: %w", c.Host, err)
	}
	retransmitOnly := make(map[string]bool)
	for _, metro := range metros {
		if metro.IsRetransmitOnlyEnabled() {
			retransmitOnly[metro.ExchangeKey.String()] = true
		}
	}
	return retransmitOnly, nil
}

// ClosestRetransmitOnlyDevice returns the reachable device with the lowest
// average latency whose metro is flagged retransmit-only, together with the set
// of retransmit-only metro exchange pubkeys it considered.
//
// Returning the set lets the caller distinguish two cases the device alone
// cannot: an empty set means no metro is flagged retransmit-only (the feature
// is not configured on this network, so the caller may skip), while a non-empty
// set with a nil device means metros are flagged but none of their devices is
// reachable and eligible for a new shred seat — the feature under test cannot
// be exercised, so the caller should fail rather than silently skip and lose
// the alert signal. Serviceability status and capacity are deliberately not
// considered: the QA user pubkey is on that program's qa_allowlist. Shred seat
// capacity is independent and must be considered because a seat fund does not
// have the same bypass.
func (c *Client) ClosestRetransmitOnlyDevice(ctx context.Context) (*Device, map[string]bool, error) {
	retransmitOnly, err := c.RetransmitOnlyExchangeKeys(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(retransmitOnly) == 0 {
		return nil, retransmitOnly, nil
	}

	device, err := c.closestDeviceInMetros(ctx, retransmitOnly, true)
	if err != nil {
		return nil, retransmitOnly, err
	}
	return device, retransmitOnly, nil
}

// ClosestNonRetransmitOnlyDevice returns the reachable device with the lowest
// average latency whose metro is not flagged retransmit-only. A nil device means
// every reachable metro is flagged, so no metro is left to reject a new seat.
func (c *Client) ClosestNonRetransmitOnlyDevice(ctx context.Context) (*Device, error) {
	retransmitOnly, err := c.RetransmitOnlyExchangeKeys(ctx)
	if err != nil {
		return nil, err
	}
	return c.closestDeviceInMetros(ctx, retransmitOnly, false)
}

// closestDeviceInMetros returns the lowest-latency reachable device whose metro
// membership in exchangeKeys equals want and which can accept a new shred seat.
func (c *Client) closestDeviceInMetros(ctx context.Context, exchangeKeys map[string]bool, want bool) (*Device, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	histories, err := c.shredsClient(programID).FetchAllDeviceHistories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch device histories on host %s: %w", c.Host, err)
	}
	availableDeviceKeys := availableShredDeviceKeys(histories)

	latencies, err := c.GetLatency(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latency on host %s: %w", c.Host, err)
	}

	bestDevice, bestAvg := closestAvailableDeviceInMetros(latencies, c.devices, exchangeKeys, availableDeviceKeys, want)
	if bestDevice != nil {
		c.log.Debug("Determined closest device", "host", c.Host, "deviceCode", bestDevice.Code,
			"avgLatencyNs", bestAvg, "retransmitOnly", want)
	}
	return bestDevice, nil
}

// availableShredDeviceKeys returns device pubkeys that the shred price command
// would report without --all. Use the active header counts rather than the
// subscription ring: instant allocations and withdrawals update these fields
// immediately, so they represent the capacity available at selection time.
func availableShredDeviceKeys(histories []shreds.KeyedDeviceHistory) map[string]bool {
	available := make(map[string]bool)
	for _, history := range histories {
		if history.IsEnabled() && history.ActiveGrantedSeats < history.ActiveTotalAvailableSeats {
			available[history.DeviceKey.String()] = true
		}
	}
	return available
}

func closestAvailableDeviceInMetros(
	latencies []*pb.Latency,
	devices map[string]*Device,
	exchangeKeys map[string]bool,
	availableDeviceKeys map[string]bool,
	want bool,
) (*Device, uint64) {
	var bestDevice *Device
	bestAvg := uint64(math.MaxUint64)
	for _, l := range latencies {
		if !l.Reachable {
			continue
		}
		device, ok := devices[l.DeviceCode]
		if !ok || !availableDeviceKeys[device.PubKey] || exchangeKeys[device.ExchangePubKey] != want {
			continue
		}
		if l.AvgLatencyNs < bestAvg {
			bestAvg = l.AvgLatencyNs
			bestDevice = device
		}
	}
	return bestDevice, bestAvg
}

// FeedSeatPrice calls the FeedSeatPrice RPC to query seat pricing for a single
// device (by pubkey). Querying by pubkey avoids device-code resolution, which
// the CLI refuses when it can't classify the cluster (e.g. a private Solana
// devnet RPC URL). This is an idempotent read, so on RPC failure it fails over
// to the next endpoint and retries.
func (c *Client) FeedSeatPrice(ctx context.Context, devicePubkey string) ([]*pb.DevicePrice, error) {
	c.log.Debug("Querying seat prices", "host", c.Host, "device", devicePubkey)
	var prices []*pb.DevicePrice
	err := c.withReadFailover(func(rpcURL string) error {
		resp, err := c.grpcClient.FeedSeatPrice(ctx, &pb.FeedSeatPriceRequest{
			SolanaRpcUrl:               rpcURL,
			DzLedgerUrl:                c.DZLedgerURL,
			UsdcMint:                   c.USDCMint,
			Keypair:                    c.Keypair,
			ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
			DevicePubkey:               devicePubkey,
		})
		if err != nil {
			return err
		}
		prices = resp.GetPrices()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get seat prices on host %s: %w", c.Host, err)
	}
	c.log.Debug("Seat prices retrieved", "host", c.Host, "count", len(prices))
	return prices, nil
}

// withReadFailover runs an agent-driven settlement query against the pool's
// current Solana RPC endpoint, failing over to the next endpoint on a retryable
// error. It is ONLY safe for idempotent operations (reads/queries): the
// settlement WRITES (FeedSeatWithdraw) deliberately bypass this and
// do not retry across endpoints, since a write that timed out on submission may
// have landed onchain and a blind retry risks double-submission. With a single
// endpoint (or no pool) it runs fn exactly once.
func (c *Client) withReadFailover(fn func(rpcURL string) error) error {
	attempts := 1
	if c.solanaRPC != nil {
		attempts = c.solanaRPC.EndpointCount()
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		lastErr = fn(c.currentSolanaRPCURL())
		if lastErr == nil {
			return nil
		}
		// Only fail over on retryable (connectivity/timeout) failures; a genuine
		// business error should surface rather than burn the remaining endpoints.
		if c.solanaRPC != nil && isRetryableRPCErr(lastErr) {
			c.log.Warn("Settlement query failed, failing over to next endpoint",
				"host", c.Host, "endpoint", redactURL(c.currentSolanaRPCURL()), "error", c.solanaRPC.scrubErr(lastErr))
			c.solanaRPC.Failover()
		} else {
			return lastErr
		}
	}
	return lastErr
}

// WaitForOpenForRequests polls the on-chain execution controller until the
// shred-subscription program enters OpenForRequests phase, which is the only
// phase that accepts FundPaymentEscrowUsdc transactions. The UpdatingPrices
// window is short but can collide with a scheduled CI run; callers should
// invoke this before a fund write to avoid a spurious failure.
func (c *Client) WaitForOpenForRequests(ctx context.Context) error {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	shredsClient := c.shredsClient(programID)

	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 2 * time.Second
	exp.MaxInterval = 10 * time.Second
	exp.MaxElapsedTime = 2 * time.Minute

	return backoff.Retry(func() error {
		ec, err := shredsClient.FetchExecutionController(ctx)
		if err != nil {
			// Scrub: a fetch error can embed the (possibly API-keyed) endpoint
			// URL, and this message reaches CI logs via require.NoError.
			return fmt.Errorf("failed to fetch execution controller: %s", c.scrubRPCErr(err))
		}
		phase := ec.GetPhase()
		if phase != shreds.ExecutionPhaseOpenForRequests {
			c.log.Info("Waiting for OpenForRequests phase", "host", c.Host, "phase", phase.String())
			return fmt.Errorf("program in %q phase, not yet open for requests", phase)
		}
		return nil
	}, backoff.WithContext(exp, ctx))
}

// FeedSeatWithdraw calls the FeedSeatWithdraw RPC to withdraw a seat from a device.
// Instant withdrawal is the default. This targets the pool's
// current endpoint and does not auto-retry across endpoints to avoid
// double-submitting a settlement transaction.
func (c *Client) FeedSeatWithdraw(ctx context.Context, devicePubkey string) error {
	c.log.Debug("Withdrawing seat", "host", c.Host, "device", devicePubkey)
	resp, err := c.grpcClient.FeedSeatWithdraw(ctx, &pb.FeedSeatWithdrawRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		SolanaRpcUrl:               c.currentSolanaRPCURL(),
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
	})
	if err != nil {
		return fmt.Errorf("failed to withdraw seat on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("seat withdrawal failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Seat withdrawal successful", "host", c.Host, "device", devicePubkey)
	return nil
}

// shredsQuery parses the program ID and derives this client's public-IP bits,
// returning a pool-backed shreds client for reading seat state. Consolidates the
// parse/derive preamble shared by the seat helpers. The To4() invariant is
// enforced at Client construction (NewClient rejects a non-IPv4 public IP).
func (c *Client) shredsQuery() (*shreds.Client, uint32, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	clientIPBits := binary.BigEndian.Uint32(c.publicIP.To4())
	return c.shredsClient(programID), clientIPBits, nil
}

// isAccountNotFound reports whether a shreds fetch error means the account does
// not exist. A missing account surfaces as shreds.ErrAccountNotFound through the
// shreds nil-result path and as rpc.ErrNotFound through the live RPC
// (GetAccountInfo) path. Note this matches sentinel errors, not error text, so
// an unrelated "... not found" message (e.g. "Blockhash not found") is not
// mistaken for a missing account.
func isAccountNotFound(err error) bool {
	return errors.Is(err, shreds.ErrAccountNotFound) || errors.Is(err, rpc.ErrNotFound)
}

// seatIsWithdrawn reads authoritative onchain state to decide whether the seat
// for deviceKey no longer holds an active tenure: the account is gone, or
// TenureEpochs == 0 with no pending instant allocation request. Withdrawal is
// blocked onchain while the pending flag is set, so a pending seat is
// definitionally not withdrawn regardless of what tenure reads. Used to confirm
// a withdraw actually took effect rather than pattern-matching the external
// CLI's error text (a transient "Blockhash not found" must never read as
// "already withdrawn").
//
// Withdrawal zeroes TenureEpochs but does not close the seat account, so
// not-found normally means the seat was never initialized. A seat can also be
// minutes old (first pay against a device), which a stale endpoint may not see
// yet, so a single not-found read is not trusted: it counts as withdrawn only
// when a second read — taken after rotating to a different pool endpoint —
// agrees.
func (c *Client) seatIsWithdrawn(ctx context.Context, shredsClient *shreds.Client, deviceKey solana.PublicKey, clientIPBits uint32) (bool, error) {
	seat, err := shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
	if isAccountNotFound(err) {
		if c.solanaRPC != nil && c.solanaRPC.EndpointCount() > 1 {
			c.solanaRPC.Failover()
		}
		seat, err = shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
		if isAccountNotFound(err) {
			return true, nil
		}
	}
	if err != nil {
		return false, err
	}
	return seat.TenureEpochs == 0 && !seat.HasPendingInstantRequest(), nil
}

// isInFlightPreflightBail reports whether a FeedSeatWithdraw error is the
// client-side preflight rejection that wrongly reports a just-closed
// InstantSeatAllocationRequest as still "in flight". This is a stale
// getMultipleAccounts read on the CLI's current Solana RPC endpoint (a fixed
// endpoint can serve the closed request PDA as existing for seconds to hours),
// so the fix is to rotate endpoints and retry. It is a pre-submission rejection
// — no transaction was sent — so rotating is safe from double-submission.
// Submission timeouts are deliberately NOT matched here: a timed-out submission
// may have landed onchain, so it must not trigger an endpoint rotation.
func isInFlightPreflightBail(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "in flight") || strings.Contains(msg, "in-flight")
}

// WithdrawSeatWithRetry withdraws a seat, retrying over a bounded window on the
// spurious "request in flight" preflight bail and transient RPC failures. A
// single-shot withdraw is unsafe for cleanup and self-heal: the same transient
// rejection failed on every hourly run, leaving the seat active onchain and
// growing the payment escrow by one epoch price per run. Each attempt logs its
// (redacted) endpoint so a stuck withdraw is diagnosable from the run log alone.
//
// When FeedSeatWithdraw reports an error, the seat's authoritative onchain state
// is consulted before deciding: if the seat is gone or reads TenureEpochs == 0,
// the withdraw is treated as done (it may have landed despite a timed-out
// submission, or a prior attempt/run already withdrew it). Otherwise the attempt
// is retried. Confirming onchain — rather than pattern-matching the external CLI
// error text — keeps a transient failure (e.g. "Blockhash not found") from being
// misread as success and silently leaving the seat stuck. The onchain read uses
// getAccountInfo, which stays fresh even on an endpoint whose getMultipleAccounts
// (used by the CLI preflight) is stale.
//
// On the in-flight preflight bail specifically, the loop rotates to a different
// Solana RPC endpoint (pool Failover) before retrying, so the next submission
// preflights against fresh state; a fixed endpoint can stay stale far longer than
// the retry window. This rotation fires only for the pre-submission in-flight
// bail — never for submission timeouts, which may have landed onchain.
func (c *Client) WithdrawSeatWithRetry(ctx context.Context, devicePubkey string) error {
	deviceKey, err := solana.PublicKeyFromBase58(devicePubkey)
	if err != nil {
		return fmt.Errorf("failed to parse device pubkey %q: %w", devicePubkey, err)
	}
	shredsClient, clientIPBits, err := c.shredsQuery()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(withdrawRetryTimeout)
	attempt := 0
	for {
		attempt++
		withdrawErr := c.FeedSeatWithdraw(ctx, devicePubkey)
		if withdrawErr == nil {
			c.log.Info("Seat withdraw succeeded", "host", c.Host, "device", devicePubkey, "attempt", attempt)
			return nil
		}
		withdrawn, checkErr := c.seatIsWithdrawn(ctx, shredsClient, deviceKey, clientIPBits)
		if checkErr == nil && withdrawn {
			c.log.Info("Seat already withdrawn onchain, nothing to do", "host", c.Host, "device", devicePubkey, "attempt", attempt)
			return nil
		}
		warnAttrs := []any{
			"host", c.Host, "device", devicePubkey, "attempt", attempt,
			"endpoint", redactURL(c.currentSolanaRPCURL()), "error", c.scrubRPCErr(withdrawErr),
		}
		if checkErr != nil {
			warnAttrs = append(warnAttrs, "confirm_read_error", c.scrubRPCErr(checkErr))
		}
		c.log.Warn("Seat withdraw attempt failed, will retry", warnAttrs...)
		if time.Now().After(deadline) {
			return fmt.Errorf("seat withdraw did not succeed within %s on host %s after %d attempts: %s",
				withdrawRetryTimeout, c.Host, attempt, c.scrubRPCErr(withdrawErr))
		}
		// The stale getMultipleAccounts read behind the in-flight bail lives on
		// this endpoint; rotate so the next submission's preflight reads fresh.
		// A genuine in-flight rejection (the withdraw racing the oracle's ack)
		// matches the same marker and also rotates — harmless: every pool
		// endpoint is valid for the shared reads that follow, and rotation is
		// bounded by the retry window.
		if isInFlightPreflightBail(withdrawErr) && c.solanaRPC != nil && c.solanaRPC.EndpointCount() > 1 {
			c.solanaRPC.Failover()
			c.log.Info("Rotated Solana RPC endpoint after in-flight preflight bail",
				"host", c.Host, "device", devicePubkey, "endpoint", redactURL(c.currentSolanaRPCURL()))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(withdrawRetryInterval):
		}
	}
}

// filterActiveSeats selects seats belonging to clientIPBits that are still
// active (TenureEpochs > 0). Split out from SelfHealStuckSeats so the selection
// logic is unit-testable without an RPC.
func filterActiveSeats(seats []shreds.KeyedClientSeat, clientIPBits uint32) []shreds.KeyedClientSeat {
	var active []shreds.KeyedClientSeat
	for _, seat := range seats {
		if seat.ClientIPBits == clientIPBits && seat.TenureEpochs > 0 {
			active = append(active, seat)
		}
	}
	return active
}

// SelfHealStuckSeats detects client seats stuck active onchain for this
// client's public IP — the poisoned state left when a previous run's withdraw
// spuriously bailed, leaving TenureEpochs > 0 and an open payment escrow — and
// withdraws each, polling until the withdraw confirmation (seatIsWithdrawn)
// reports the seat cleared. The scan mirrors the conflict scan the pay CLI performs via
// getProgramAccounts, so a stuck seat on any device is detected — not just one
// on a specific device. Returns the number of seats healed; safe to call when
// there are none (returns 0, nil). Logs the device key, seat pubkey, and tenure
// of every seat found so a future incident is diagnosable from the run log
// alone.
func (c *Client) SelfHealStuckSeats(ctx context.Context) (int, error) {
	shredsClient, clientIPBits, err := c.shredsQuery()
	if err != nil {
		return 0, err
	}
	allSeats, err := shredsClient.FetchAllClientSeats(ctx)
	if err != nil {
		// Scrub: a fetch error can embed the (possibly API-keyed) endpoint URL.
		return 0, fmt.Errorf("failed to fetch client seats on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	seats := filterActiveSeats(allSeats, clientIPBits)
	if len(seats) == 0 {
		return 0, nil
	}

	healed := 0
	for _, seat := range seats {
		deviceKey := seat.DeviceKey
		c.log.Warn("Found stuck-active client seat, self-healing",
			"host", c.Host, "seat", seat.Pubkey, "device", deviceKey, "tenure_epochs", seat.TenureEpochs)

		if err := c.WithdrawSeatWithRetry(ctx, deviceKey.String()); err != nil {
			return healed, fmt.Errorf("failed to withdraw stuck seat %s on device %s (host %s): %w", seat.Pubkey, deviceKey, c.Host, err)
		}

		if err := poll.Until(ctx, func() (bool, error) {
			// Reuse the withdraw confirmation predicate: tenure cleared, no
			// pending request, and a not-found trusted only after a second read
			// on a rotated endpoint (withdrawal does not close the seat account,
			// so a single not-found here can only be a stale read).
			withdrawn, fetchErr := c.seatIsWithdrawn(ctx, shredsClient, deviceKey, clientIPBits)
			if fetchErr != nil {
				// The withdraw already succeeded; a transient read blip here
				// shouldn't fail the heal. Log (scrubbed) and keep polling until
				// seatHealPollTimeout bounds it.
				c.log.Debug("Seat heal poll read error, retrying", "host", c.Host, "seat", seat.Pubkey, "error", c.scrubRPCErr(fetchErr))
				return false, nil
			}
			return withdrawn, nil
		}, seatHealPollTimeout, seatReadInterval); err != nil {
			return healed, fmt.Errorf("stuck seat %s on device %s (host %s) did not clear to TenureEpochs==0: %w", seat.Pubkey, deviceKey, c.Host, err)
		}

		c.log.Info("Stuck-active client seat healed", "host", c.Host, "seat", seat.Pubkey, "device", deviceKey)
		healed++
	}
	return healed, nil
}

// GetEffectiveSeatPrice returns the effective per-epoch price for the client's
// seat on the given device, in raw USDC (6 decimals). If the client seat has a
// price override, the override is returned; otherwise the epoch price (in whole
// dollars, converted to micro-USDC) is used.
func (c *Client) GetEffectiveSeatPrice(ctx context.Context, devicePubkey string, epochPrice uint64) (uint64, error) {
	deviceKey, err := solana.PublicKeyFromBase58(devicePubkey)
	if err != nil {
		return 0, fmt.Errorf("failed to parse device pubkey %q: %w", devicePubkey, err)
	}

	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return 0, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	clientIPBits := binary.BigEndian.Uint32(c.publicIP.To4())
	shredsClient := c.shredsClient(programID)

	// This reads state written by the preceding fund. A lagging RPC node
	// can briefly serve a view in which the seat account does not yet exist, so
	// poll until it is visible rather than failing on a single stale read.
	// (The failover pool fails over on RPC errors, but an account-not-found is a
	// valid empty read, not an error, so it needs poll-until.)
	var seat *shreds.ClientSeat
	if err := poll.Until(ctx, func() (bool, error) {
		s, fetchErr := shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
		// A missing account surfaces as rpc.ErrNotFound through the live RPC
		// path (gagliardetto GetAccountInfo) and as shreds.ErrAccountNotFound
		// through the shreds nil-result path; treat both as "not yet visible".
		if errors.Is(fetchErr, shreds.ErrAccountNotFound) || errors.Is(fetchErr, rpc.ErrNotFound) {
			c.log.Debug("Client seat not yet visible, polling", "host", c.Host)
			return false, nil
		}
		if fetchErr != nil {
			return false, fetchErr
		}
		seat = s
		return true, nil
	}, seatReadTimeout, seatReadInterval); err != nil {
		return 0, fmt.Errorf("failed to fetch client seat on host %s: %w", c.Host, err)
	}

	if seat.HasPriceOverride() {
		price := uint64(seat.OverrideUSDCPriceDollars) * 1_000_000
		c.log.Debug("Seat has price override", "host", c.Host, "override_dollars", seat.OverrideUSDCPriceDollars, "price_usdc", price)
		return price, nil
	}

	price := epochPrice * 1_000_000
	c.log.Debug("Seat using epoch price", "host", c.Host, "epoch_price_dollars", epochPrice, "price_usdc", price)
	return price, nil
}

// SeatPrices are the whole-dollar seat prices the shred-subscription program
// computes for a device, at the two epochs a settlement probe has to tell
// apart. Prices are unprorated: proration only ever reduces the charge, so
// funding the full price is always sufficient, and a prorated figure is a
// function of the slot it was read at and would race the payment. The per-seat
// price override is not applied — GetEffectiveSeatPrice owns that, mirroring
// the program's checked_override_usdc_price_dollars().
type SeatPrices struct {
	// InstantAllocationDollars is what request_instant_seat_allocation charges,
	// read at LastSettledEpoch.
	InstantAllocationDollars uint64
	LastSettledEpoch         uint64

	// CurrentEpochDollars is what a recurring subscriber pays for
	// CurrentSubscriptionEpoch — the figure `shreds price` reports as
	// epoch_price. Valid only when HasCurrentEpoch is set: during the
	// UpdatingPrices phase the ring entry for the current epoch may not have
	// been written yet for this metro or device.
	CurrentEpochDollars      uint64
	HasCurrentEpoch          bool
	CurrentSubscriptionEpoch uint64
}

// SeatPrices reads the chain and computes both seat prices for the given
// device.
//
// The program charges the price recorded at last_settled_epoch, while
// `doublezero-solana shreds price` reports the entry one epoch newer as
// epoch_price. Computing both from chain state gives QA an oracle independent
// of the CLI under test, which is the only way the probe can check that the
// CLI's two figures point at the ring entries they claim to — and it needs no
// CLI release to reach the version-pinned QA hosts.
func (c *Client) SeatPrices(ctx context.Context, devicePubkey string) (*SeatPrices, error) {
	deviceKey, err := solana.PublicKeyFromBase58(devicePubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device pubkey %q: %w", devicePubkey, err)
	}
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	shredsClient := c.shredsClient(programID)

	var prices SeatPrices
	var deviceHistory *shreds.DeviceHistory
	var metroHistory *shreds.MetroHistory

	// All three accounts long predate a QA run, but a lagging RPC node can serve
	// a view in which one is not yet visible. An account-not-found is a valid
	// empty read rather than an RPC error, so the failover pool does not retry
	// it — poll until visible instead of failing on a single stale read.
	if err := poll.Until(ctx, func() (bool, error) {
		controller, fetchErr := shredsClient.FetchExecutionController(ctx)
		if fetchErr != nil {
			if isAccountNotFound(fetchErr) {
				c.log.Debug("Execution controller not yet visible, polling", "host", c.Host)
				return false, nil
			}
			// Scrub: a fetch error can embed the (possibly API-keyed) endpoint URL.
			return false, fmt.Errorf("failed to fetch execution controller: %s", c.scrubRPCErr(fetchErr))
		}

		device, fetchErr := shredsClient.FetchDeviceHistory(ctx, deviceKey)
		if fetchErr != nil {
			if isAccountNotFound(fetchErr) {
				c.log.Debug("Device history not yet visible, polling", "host", c.Host, "device", devicePubkey)
				return false, nil
			}
			return false, fmt.Errorf("failed to fetch device history for %s: %s", devicePubkey, c.scrubRPCErr(fetchErr))
		}

		// The device history carries its metro's exchange key, so the metro
		// history needs no separate exchange lookup.
		metro, fetchErr := shredsClient.FetchMetroHistory(ctx, device.MetroExchangeKey)
		if fetchErr != nil {
			if isAccountNotFound(fetchErr) {
				c.log.Debug("Metro history not yet visible, polling", "host", c.Host, "exchange", device.MetroExchangeKey)
				return false, nil
			}
			return false, fmt.Errorf("failed to fetch metro history for exchange %s: %s", device.MetroExchangeKey, c.scrubRPCErr(fetchErr))
		}

		prices.LastSettledEpoch = controller.LastSettledEpoch
		prices.CurrentSubscriptionEpoch = controller.CurrentSubscriptionEpoch
		deviceHistory = device
		metroHistory = metro
		return true, nil
	}, seatReadTimeout, seatReadInterval); err != nil {
		return nil, fmt.Errorf("failed to read shred pricing state on host %s: %w", c.Host, err)
	}

	// Combine the metro price with the device's signed premium at a specific
	// epoch, exactly as the program does. Never fall back to the ring's current
	// entry on a miss — reading a different entry than the one asked for is
	// precisely the divergence this probe exists to catch.
	priceAt := func(epoch uint64) (uint64, error) {
		metroEntry, ok := metroHistory.Prices.Find(epoch)
		if !ok {
			return 0, fmt.Errorf("metro exchange %s has no price entry for epoch %d", deviceHistory.MetroExchangeKey, epoch)
		}
		deviceEntry, ok := deviceHistory.Subscriptions.Find(epoch)
		if !ok {
			return 0, fmt.Errorf("device %s has no subscription entry for epoch %d", devicePubkey, epoch)
		}
		return uint64(deviceEntry.Subscription.USDCPriceDollars(&metroEntry.Price)), nil
	}

	// The settled-epoch lookup is the same one the program performs, and the
	// program rejects the allocation when either ring misses, so a miss here is
	// fatal.
	prices.InstantAllocationDollars, err = priceAt(prices.LastSettledEpoch)
	if err != nil {
		return nil, fmt.Errorf("cannot compute the instant-allocation seat price on host %s: %w", c.Host, err)
	}

	// A miss on the current epoch is expected part-way through the UpdatingPrices
	// phase, when the ring has not yet been advanced for this metro or device, so
	// report it as unavailable rather than failing the whole read.
	if current, currentErr := priceAt(prices.CurrentSubscriptionEpoch); currentErr == nil {
		prices.CurrentEpochDollars = current
		prices.HasCurrentEpoch = true
	} else {
		c.log.Debug("No onchain price for the current subscription epoch",
			"host", c.Host, "device", devicePubkey, "reason", currentErr)
	}

	c.log.Debug("Onchain seat prices", "host", c.Host, "device", devicePubkey,
		"instant_allocation_dollars", prices.InstantAllocationDollars,
		"last_settled_epoch", prices.LastSettledEpoch,
		"current_epoch_dollars", prices.CurrentEpochDollars,
		"has_current_epoch", prices.HasCurrentEpoch,
		"current_subscription_epoch", prices.CurrentSubscriptionEpoch)
	return &prices, nil
}

// IsSeatProratingEnabled returns true if the shred-subscription program config
// has prorated-service enabled (testnet-style: seat withdrawal refunds the
// unused portion of the epoch). Reads the program config account directly
// rather than relying on an externally-supplied flag.
func (c *Client) IsSeatProratingEnabled(ctx context.Context) (bool, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return false, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	shredsClient := c.shredsClient(programID)
	cfg, err := shredsClient.FetchProgramConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch program config on host %s: %w", c.Host, err)
	}
	return cfg.IsProratedServiceEnabled(), nil
}

func (c *Client) IsRetransmitOnlyOnboardingEnforced(ctx context.Context) (bool, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return false, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	cfg, err := c.shredsClient(programID).FetchProgramConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch program config on host %s: %w", c.Host, err)
	}
	return cfg.IsRetransmitOnlyOnboardingEnforced(), nil
}

// IsProgramPaused returns true if the shred-subscription program config has
// the paused flag set. While paused, the oracle cannot ack instant seat
// allocation requests, which leaves the seat un-withdrawable.
func (c *Client) IsProgramPaused(ctx context.Context) (bool, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return false, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	cfg, err := shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID).FetchProgramConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch program config on host %s: %w", c.Host, err)
	}
	return cfg.IsPaused(), nil
}

// GetWalletPubkey calls the GetWalletPubkey RPC to read the keypair file on the
// remote host and return the base58-encoded public key.
func (c *Client) GetWalletPubkey(ctx context.Context) (solana.PublicKey, error) {
	resp, err := c.grpcClient.GetWalletPubkey(ctx, &pb.GetWalletPubkeyRequest{
		Keypair: c.Keypair,
	})
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to get wallet pubkey on host %s: %w", c.Host, err)
	}
	pubkey, err := solana.PublicKeyFromBase58(resp.GetPubkey())
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to parse wallet pubkey %q: %w", resp.GetPubkey(), err)
	}
	c.log.Debug("Wallet pubkey retrieved", "host", c.Host, "pubkey", pubkey)
	return pubkey, nil
}

// GetUSDCBalance queries the USDC token balance for the client's wallet.
// It derives the associated token account from the wallet pubkey and USDC mint,
// then queries the balance via the Solana RPC (which points to the DZ ledger
// on testnet/devnet and Solana proper on mainnet).
func (c *Client) GetUSDCBalance(ctx context.Context) (uint64, error) {
	ownerPubkey, err := c.GetWalletPubkey(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get wallet pubkey on host %s: %w", c.Host, err)
	}

	usdcMint, err := solana.PublicKeyFromBase58(c.USDCMint)
	if err != nil {
		return 0, fmt.Errorf("failed to parse USDC mint %q: %w", c.USDCMint, err)
	}

	ata, _, err := solana.FindAssociatedTokenAddress(ownerPubkey, usdcMint)
	if err != nil {
		return 0, fmt.Errorf("failed to derive ATA for owner %s and mint %s: %w", ownerPubkey, usdcMint, err)
	}

	// Use the failover pool when present so a dead/lagging endpoint is replaced
	// transparently; fall back to a single-endpoint client for hand-built test
	// clients. Before reading, actively fail over off a lagging node so a
	// stale-but-valid read can't produce a spurious assertion failure.
	var solanaClient *rpc.Client
	budget := rpcBudgetFromEnv()
	if c.solanaRPC != nil {
		c.solanaRPC.SelectHealthiestEndpoint(ctx)
		solanaClient = c.solanaRPC.RPC()
		budget = c.solanaRPC.budget
	} else {
		solanaClient = rpc.New(c.SolanaRPCURL)
	}

	var result *rpc.GetTokenAccountBalanceResult
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = budget.initialBackoff
	exp.MaxElapsedTime = budget.maxElapsed
	retryPolicy := backoff.WithMaxRetries(exp, budget.maxRetries)
	retryPolicy = backoff.WithContext(retryPolicy, ctx)

	if err := backoff.Retry(func() error {
		var rpcErr error
		result, rpcErr = solanaClient.GetTokenAccountBalance(ctx, ata, rpc.CommitmentConfirmed)
		if rpcErr != nil {
			// Scrub: solana-go embeds the (possibly API-keyed) endpoint URL in
			// its error strings, so never log/return the raw error.
			c.log.Debug("Retryable RPC error fetching USDC balance", "host", c.Host, "ata", ata, "error", c.scrubRPCErr(rpcErr))
			return rpcErr
		}
		return nil
	}, retryPolicy); err != nil {
		return 0, fmt.Errorf("failed to get token account balance for ATA %s on host %s: %s", ata, c.Host, c.scrubRPCErr(err))
	}

	balance, err := strconv.ParseUint(result.Value.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse balance %q: %w", result.Value.Amount, err)
	}

	c.log.Debug("USDC balance retrieved", "host", c.Host, "owner", ownerPubkey, "ata", ata, "balance", balance)
	return balance, nil
}
