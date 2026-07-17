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
// settlement WRITES (FeedSeatPay/FeedSeatWithdraw) deliberately bypass this and
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
// invoke this before FeedSeatPay to avoid a spurious failure.
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
			return fmt.Errorf("failed to fetch execution controller: %w", err)
		}
		phase := ec.GetPhase()
		if phase != shreds.ExecutionPhaseOpenForRequests {
			c.log.Info("Waiting for OpenForRequests phase", "host", c.Host, "phase", phase.String())
			return fmt.Errorf("program in %q phase, not yet open for requests", phase)
		}
		return nil
	}, backoff.WithContext(exp, ctx))
}

// FeedSeatPay calls the FeedSeatPay RPC to pay for a seat on a device.
// The client's public IP is auto-filled. Instant allocation is the default.
//
// The settlement transaction is submitted against the pool's current (already
// health-selected) Solana RPC endpoint. It deliberately does NOT auto-retry
// across endpoints on failure: a payment that timed out on submission may have
// landed onchain, so a blind retry against another endpoint risks
// double-submission. The construction-time slot-lag selection plus read-path
// failover keep CurrentURL() pointed at a healthy endpoint by the time this is
// called.
func (c *Client) FeedSeatPay(ctx context.Context, devicePubkey string, amount string) error {
	c.log.Debug("Paying for seat", "host", c.Host, "device", devicePubkey, "amount", amount)
	resp, err := c.grpcClient.FeedSeatPay(ctx, &pb.FeedSeatPayRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		Amount:                     amount,
		SolanaRpcUrl:               c.currentSolanaRPCURL(),
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
	})
	if err != nil {
		return fmt.Errorf("failed to pay for seat on host %s: %w", c.Host, err)
	}
	if !resp.GetSuccess() {
		c.log.Error("Seat payment failed", "host", c.Host, "device", devicePubkey, "output", resp.GetOutput())
		return fmt.Errorf("seat payment failed on host %s: %s", c.Host, resp.GetOutput())
	}
	c.log.Debug("Seat payment successful", "host", c.Host, "device", devicePubkey)
	return nil
}

// FeedSeatWithdraw calls the FeedSeatWithdraw RPC to withdraw a seat from a device.
// Instant withdrawal is the default. Like FeedSeatPay, this targets the pool's
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

// isSeatNotFound reports whether a FetchClientSeat error means the seat account
// does not exist. A missing account surfaces as shreds.ErrAccountNotFound
// through the shreds nil-result path and as rpc.ErrNotFound through the live RPC
// (GetAccountInfo) path. Note this matches sentinel errors, not error text, so
// an unrelated "... not found" message (e.g. "Blockhash not found") is not
// mistaken for a missing seat.
func isSeatNotFound(err error) bool {
	return errors.Is(err, shreds.ErrAccountNotFound) || errors.Is(err, rpc.ErrNotFound)
}

// seatIsWithdrawn reads authoritative onchain state to decide whether the seat
// for deviceKey no longer holds an active tenure: the account is gone, or
// TenureEpochs == 0. Used to confirm a withdraw actually took effect rather than
// pattern-matching the external CLI's error text (a transient "Blockhash not
// found" must never read as "already withdrawn").
func (c *Client) seatIsWithdrawn(ctx context.Context, shredsClient *shreds.Client, deviceKey solana.PublicKey, clientIPBits uint32) (bool, error) {
	seat, err := shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
	if isSeatNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return seat.TenureEpochs == 0, nil
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
		if withdrawn, checkErr := c.seatIsWithdrawn(ctx, shredsClient, deviceKey, clientIPBits); checkErr == nil && withdrawn {
			c.log.Info("Seat already withdrawn onchain, nothing to do", "host", c.Host, "device", devicePubkey, "attempt", attempt)
			return nil
		}
		c.log.Warn("Seat withdraw attempt failed, will retry",
			"host", c.Host, "device", devicePubkey, "attempt", attempt,
			"endpoint", redactURL(c.currentSolanaRPCURL()), "error", c.scrubRPCErr(withdrawErr))
		if time.Now().After(deadline) {
			return fmt.Errorf("seat withdraw did not succeed within %s on host %s after %d attempts: %s",
				withdrawRetryTimeout, c.Host, attempt, c.scrubRPCErr(withdrawErr))
		}
		// The stale getMultipleAccounts read behind the in-flight bail lives on
		// this endpoint; rotate so the next submission's preflight reads fresh.
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

// ActiveClientSeats returns all onchain client seats for this client's public
// IP that are still active (TenureEpochs > 0), across any device. It mirrors
// the conflict scan the pay CLI performs via getProgramAccounts, so a seat left
// stuck-active on any device is detected — not just one on a specific device.
func (c *Client) ActiveClientSeats(ctx context.Context) ([]shreds.KeyedClientSeat, error) {
	shredsClient, clientIPBits, err := c.shredsQuery()
	if err != nil {
		return nil, err
	}
	seats, err := shredsClient.FetchAllClientSeats(ctx)
	if err != nil {
		// Scrub: a fetch error can embed the (possibly API-keyed) endpoint URL.
		return nil, fmt.Errorf("failed to fetch client seats on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	return filterActiveSeats(seats, clientIPBits), nil
}

// filterActiveSeats selects seats belonging to clientIPBits that are still
// active (TenureEpochs > 0). Split out from ActiveClientSeats so the selection
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
// withdraws each, polling until it reads TenureEpochs == 0 (or the account
// vanishes). Returns the number of seats healed; safe to call when there are
// none (returns 0, nil). Logs the device key, seat pubkey, and tenure of every
// seat found so a future incident is diagnosable from the run log alone.
func (c *Client) SelfHealStuckSeats(ctx context.Context) (int, error) {
	seats, err := c.ActiveClientSeats(ctx)
	if err != nil {
		return 0, err
	}
	if len(seats) == 0 {
		return 0, nil
	}

	shredsClient, clientIPBits, err := c.shredsQuery()
	if err != nil {
		return 0, err
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
			s, fetchErr := shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
			// A closed/absent seat account is fully healed.
			if isSeatNotFound(fetchErr) {
				return true, nil
			}
			if fetchErr != nil {
				// The withdraw already succeeded; a transient read blip here
				// shouldn't fail the heal. Log (scrubbed) and keep polling until
				// seatHealPollTimeout bounds it.
				c.log.Debug("Seat heal poll read error, retrying", "host", c.Host, "seat", seat.Pubkey, "error", c.scrubRPCErr(fetchErr))
				return false, nil
			}
			return s.TenureEpochs == 0, nil
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

	// This reads state written by the preceding FeedSeatPay. A lagging RPC node
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
