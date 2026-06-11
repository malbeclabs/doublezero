package qa

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
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

// IsSeatProratingEnabled returns true if the shred-subscription program config
// has prorated-service enabled (testnet-style: seat withdrawal refunds the
// unused portion of the epoch). Reads the program config account directly
// rather than relying on an externally-supplied flag.
func (c *Client) IsSeatProratingEnabled(ctx context.Context) (bool, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return false, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	shredsClient := shreds.New(shreds.NewRPCClient(c.SolanaRPCURL), programID)
	cfg, err := shredsClient.FetchProgramConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch program config on host %s: %w", c.Host, err)
	}
	return cfg.IsProratedServiceEnabled(), nil
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

	var result *rpc.GetTokenAccountBalanceResult
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 1 * time.Second
	exp.MaxElapsedTime = 30 * time.Second
	retryPolicy := backoff.WithMaxRetries(exp, 5)
	retryPolicy = backoff.WithContext(retryPolicy, ctx)

	if err := backoff.Retry(func() error {
		var rpcErr error
		result, rpcErr = solanaClient.GetTokenAccountBalance(ctx, ata, rpc.CommitmentConfirmed)
		if rpcErr != nil {
			c.log.Debug("Retryable RPC error fetching USDC balance", "host", c.Host, "ata", ata, "error", rpcErr)
			return rpcErr
		}
		return nil
	}, retryPolicy); err != nil {
		return 0, fmt.Errorf("failed to get token account balance for ATA %s on host %s: %w", ata, c.Host, err)
	}

	balance, err := strconv.ParseUint(result.Value.Amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse balance %q: %w", result.Value.Amount, err)
	}

	c.log.Debug("USDC balance retrieved", "host", c.Host, "owner", ownerPubkey, "ata", ata, "balance", balance)
	return balance, nil
}
