package qa

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

// FeedSeatPrice calls the FeedSeatPrice RPC to query device seat prices.
func (c *Client) FeedSeatPrice(ctx context.Context) ([]*pb.DevicePrice, error) {
	c.log.Debug("Querying seat prices", "host", c.Host)
	resp, err := c.grpcClient.FeedSeatPrice(ctx, &pb.FeedSeatPriceRequest{
		SolanaRpcUrl:               c.SolanaRPCURL,
		DzLedgerUrl:                c.DZLedgerURL,
		UsdcMint:                   c.USDCMint,
		Keypair:                    c.Keypair,
		ShredSubscriptionProgramId: c.ShredSubscriptionProgramID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get seat prices on host %s: %w", c.Host, err)
	}
	c.log.Debug("Seat prices retrieved", "host", c.Host, "count", len(resp.GetPrices()))
	return resp.GetPrices(), nil
}

// FeedSeatPay calls the FeedSeatPay RPC to pay for a seat on a device.
// The client's public IP is auto-filled. Instant allocation is the default.
func (c *Client) FeedSeatPay(ctx context.Context, devicePubkey string, amount string) error {
	c.log.Debug("Paying for seat", "host", c.Host, "device", devicePubkey, "amount", amount)
	resp, err := c.grpcClient.FeedSeatPay(ctx, &pb.FeedSeatPayRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		Amount:                     amount,
		SolanaRpcUrl:               c.SolanaRPCURL,
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
// Instant withdrawal is the default.
func (c *Client) FeedSeatWithdraw(ctx context.Context, devicePubkey string) error {
	c.log.Debug("Withdrawing seat", "host", c.Host, "device", devicePubkey)
	resp, err := c.grpcClient.FeedSeatWithdraw(ctx, &pb.FeedSeatWithdrawRequest{
		DevicePubkey:               devicePubkey,
		ClientIp:                   c.publicIP.To4().String(),
		SolanaRpcUrl:               c.SolanaRPCURL,
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
	shredsClient := shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID)
	seat, err := shredsClient.FetchClientSeat(ctx, deviceKey, clientIPBits)
	if err != nil {
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

	solanaClient := rpc.New(c.SolanaRPCURL)
	result, err := solanaClient.GetTokenAccountBalance(ctx, ata, rpc.CommitmentConfirmed)
	if err != nil {
		return 0, fmt.Errorf("failed to get token account balance for ATA %s on host %s: %w", ata, c.Host, err)
	}

	balance, err := strconv.ParseUint(result.Value.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse balance %q: %w", result.Value.Amount, err)
	}

	c.log.Debug("USDC balance retrieved", "host", c.Host, "owner", ownerPubkey, "ata", ata, "balance", balance)
	return balance, nil
}

// minSlotsPerEpochWarmup mirrors solana_sdk::epoch_schedule::MINIMUM_SLOTS_PER_EPOCH.
const minSlotsPerEpochWarmup uint64 = 32

// GetSlot returns the current confirmed slot from the Solana RPC endpoint the
// client uses (DZ ledger for testnet/devnet, Solana proper for mainnet).
func (c *Client) GetSlot(ctx context.Context) (uint64, error) {
	solanaClient := rpc.New(c.SolanaRPCURL)
	slot, err := solanaClient.GetSlot(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return 0, fmt.Errorf("failed to get slot on host %s: %w", c.Host, err)
	}
	return slot, nil
}

// ComputeProratedRefundBounds returns the upper/lower bounds of the expected
// prorated refund (raw USDC) for a seat withdrawal whose Clock::get().slot lies
// in [slotPre, slotPost]. Mirrors ClientSeat::prorated_usdc_amount in
// doublezero-shreds. Upper corresponds to slotPre (larger remaining slots →
// larger refund); lower corresponds to slotPost.
func (c *Client) ComputeProratedRefundBounds(
	ctx context.Context,
	effectivePrice, slotPre, slotPost uint64,
) (upper, lower uint64, err error) {
	if slotPost < slotPre {
		return 0, 0, fmt.Errorf("slotPost (%d) must be >= slotPre (%d)", slotPost, slotPre)
	}

	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	shredsClient := shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID)
	controller, err := shredsClient.FetchExecutionController(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch execution controller on host %s: %w", c.Host, err)
	}

	solanaClient := rpc.New(c.SolanaRPCURL)
	schedule, err := solanaClient.GetEpochSchedule(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get epoch schedule on host %s: %w", c.Host, err)
	}
	if schedule.SlotsPerEpoch == 0 {
		return 0, 0, fmt.Errorf("invalid epoch schedule: slots_per_epoch is zero")
	}

	endSlot := firstSlotInEpoch(schedule, controller.CurrentSubscriptionEpoch)
	remainingUpper := saturatingSubU64(endSlot, slotPre)  // larger refund
	remainingLower := saturatingSubU64(endSlot, slotPost) // smaller refund

	upper = proratedAmount(effectivePrice, remainingUpper, schedule.SlotsPerEpoch)
	lower = proratedAmount(effectivePrice, remainingLower, schedule.SlotsPerEpoch)
	return upper, lower, nil
}

// firstSlotInEpoch mirrors solana_sdk::epoch_schedule::EpochSchedule::
// get_first_slot_in_epoch. Public clusters run with warmup=false, but we honor
// the schedule as returned so this works on any cluster.
func firstSlotInEpoch(s *rpc.GetEpochScheduleResult, epoch uint64) uint64 {
	if s.Warmup && epoch <= s.FirstNormalEpoch {
		if epoch >= 63 {
			return math.MaxUint64
		}
		return ((uint64(1) << epoch) - 1) * minSlotsPerEpochWarmup
	}
	return (epoch-s.FirstNormalEpoch)*s.SlotsPerEpoch + s.FirstNormalSlot
}

func saturatingSubU64(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// proratedAmount computes (price * remaining) / slotsPerEpoch using math/big to
// match the program's u128 intermediate arithmetic and truncating division.
func proratedAmount(price, remaining, slotsPerEpoch uint64) uint64 {
	if remaining == 0 || slotsPerEpoch == 0 {
		return 0
	}
	var num, denom, out big.Int
	num.Mul(new(big.Int).SetUint64(price), new(big.Int).SetUint64(remaining))
	denom.SetUint64(slotsPerEpoch)
	out.Quo(&num, &denom)
	if !out.IsUint64() {
		return math.MaxUint64
	}
	return out.Uint64()
}
