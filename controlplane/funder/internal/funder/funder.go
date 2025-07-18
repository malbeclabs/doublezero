package funder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type Funder struct {
	log *slog.Logger
	cfg Config
}

func New(cfg Config) (*Funder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Funder{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

func (f *Funder) Run(ctx context.Context) error {
	f.log.Info("Starting funder",
		"interval", f.cfg.Interval,
		"signer", f.cfg.Signer.PublicKey(),
		"minBalanceSOL", f.cfg.MinBalanceSOL,
		"topUpSOL", f.cfg.TopUpSOL,
		"serviceabilityProgramID", f.cfg.Serviceability.ProgramID(),
	)

	ticker := time.NewTicker(f.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.log.Info("Funder stopped by context", "error", ctx.Err())
			return nil
		case <-ticker.C:
			err := f.cfg.Serviceability.Load(ctx)
			if err != nil {
				f.log.Error("Failed to load serviceability state", "error", err)
				continue
			}

			// Check balance of funder.
			balance, err := f.cfg.Solana.GetBalance(ctx, f.cfg.Signer.PublicKey(), solanarpc.CommitmentFinalized)
			if err != nil {
				f.log.Error("Failed to get balance", "error", err)
				continue
			}
			balanceLamports := balance.Value
			f.log.Debug("Funder balance", "balance", balanceLamports)

			// Check that we have enough SOL to top up metrics publishers.
			if balanceLamports < uint64(f.cfg.TopUpSOL*float64(solana.LAMPORTS_PER_SOL)) {
				f.log.Error("Funder balance is below minimum", "balance", balanceLamports, "minBalance", f.cfg.TopUpSOL)
				continue
			}

			// Check balance of metrics publishers.
			devices := f.cfg.Serviceability.GetDevices()
			f.log.Debug("Checking devices", "count", len(devices))
			for _, device := range devices {
				devicePK := solana.PublicKeyFromBytes(device.PubKey[:])
				metricsPublisherPK := solana.PublicKeyFromBytes(device.MetricsPublisherPubKey[:])
				if metricsPublisherPK.IsZero() {
					f.log.Debug("Metrics publisher pubkey is zero, ignoring device", "device", devicePK, "metricsPublisher", metricsPublisherPK)
					continue
				}

				// Get device metrics publisher balance.
				balance, err := f.cfg.Solana.GetBalance(ctx, metricsPublisherPK, solanarpc.CommitmentFinalized)
				if err != nil {
					f.log.Error("Failed to get balance", "error", err)
					continue
				}
				balanceLamports := balance.Value
				f.log.Debug("Metrics publisher balance", "device", devicePK, "metricsPublisher", metricsPublisherPK, "balance", balanceLamports, "minBalance", f.cfg.MinBalanceSOL)

				// If balance is below minimum, top it up.
				if balanceLamports < uint64(f.cfg.MinBalanceSOL*float64(solana.LAMPORTS_PER_SOL)) {
					f.log.Info("Topping up metrics publisher", "device", devicePK, "metricsPublisher", metricsPublisherPK, "balance", balanceLamports, "topUp", f.cfg.TopUpSOL)

					_, err := transferFunds(ctx, f.cfg.Solana, f.cfg.Signer, metricsPublisherPK, uint64(f.cfg.TopUpSOL*float64(solana.LAMPORTS_PER_SOL)), nil)
					if err != nil {
						f.log.Error("Failed to transfer SOL", "error", err)
						continue
					}

					// Wait for the transfer to complete.
					err = waitForBalance(ctx, f.cfg.Solana, metricsPublisherPK, f.cfg.MinBalanceSOL, f.cfg.WaitForBalanceTimeout, f.cfg.WaitForBalancePollInterval)
					if err != nil {
						f.log.Error("Failed to wait for balance", "error", err)
						continue
					}

					f.log.Info("Transferred SOL to metrics publisher", "device", devicePK, "metricsPublisher", metricsPublisherPK, "amount", f.cfg.TopUpSOL)
				}
			}
		}
	}
}

func waitForBalance(ctx context.Context, client SolanaClient, account solana.PublicKey, minBalanceSOL float64, timeout time.Duration, pollInterval time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		balance, err := client.GetBalance(ctx, account, solanarpc.CommitmentFinalized)
		if err != nil {
			return fmt.Errorf("failed to get balance: %w", err)
		}
		balanceLamports := balance.Value
		if balanceLamports >= uint64(minBalanceSOL*float64(solana.LAMPORTS_PER_SOL)) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("timeout waiting for balance: account=%s, expected balance=%.2f SOL", account, minBalanceSOL)
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

func transferFunds(
	ctx context.Context,
	client SolanaClient,
	sender solana.PrivateKey,
	recipient solana.PublicKey,
	lamports uint64,
	opts *solanarpc.TransactionOpts,
) (solana.Signature, error) {
	if opts == nil {
		opts = &solanarpc.TransactionOpts{
			SkipPreflight:       true,
			MaxRetries:          nil,
			PreflightCommitment: solanarpc.CommitmentFinalized,
		}
	}

	recentBlockhash, err := client.GetLatestBlockhash(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, err
	}

	ix := system.NewTransferInstruction(lamports, sender.PublicKey(), recipient).Build()

	tx, err := solana.NewTransaction(
		[]solana.Instruction{ix},
		recentBlockhash.Value.Blockhash,
		solana.TransactionPayer(sender.PublicKey()),
	)
	if err != nil {
		return solana.Signature{}, err
	}

	_, err = tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if key.Equals(sender.PublicKey()) {
				return &sender
			}
			return nil
		},
	)
	if err != nil {
		return solana.Signature{}, err
	}

	sig, err := client.SendTransactionWithOpts(
		ctx,
		tx,
		*opts,
	)
	if err != nil {
		return solana.Signature{}, err
	}

	return sig, nil
}
