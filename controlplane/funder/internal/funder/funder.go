package funder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/funder/internal/metrics"
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
	minBalanceLamports := f.cfg.MinBalanceLamports()
	topUpLamports := f.cfg.TopUpLamports()

	f.log.Info("Starting funder",
		"interval", f.cfg.Interval,
		"signer", f.cfg.Signer.PublicKey(),
		"minBalanceLamports", minBalanceLamports,
		"topUpLamports", topUpLamports,
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
				metrics.Errors.WithLabelValues(metrics.ErrorTypeLoadServiceabilityState).Inc()
				continue
			}

			// Check balance of funder.
			balance, err := f.cfg.Solana.GetBalance(ctx, f.cfg.Signer.PublicKey(), solanarpc.CommitmentFinalized)
			if err != nil {
				f.log.Error("Failed to get balance", "error", err)
				metrics.Errors.WithLabelValues(metrics.ErrorTypeGetFunderAccountBalance).Inc()
				continue
			}
			balanceLamports := balance.Value
			f.log.Debug("Funder balance", "account", f.cfg.Signer.PublicKey(), "balanceLamports", balanceLamports)
			balanceSOL := float64(balanceLamports) / float64(solana.LAMPORTS_PER_SOL)
			metrics.FunderAccountBalanceSOL.WithLabelValues(f.cfg.Signer.PublicKey().String()).Set(balanceSOL)

			// Check that we have enough SOL to top up metrics publishers.
			if balanceLamports < topUpLamports {
				f.log.Error("Funder balance is below minimum", "balanceLamports", balanceLamports, "minBalanceLamports", minBalanceLamports)
				metrics.Errors.WithLabelValues(metrics.ErrorTypeFunderAccountBalanceBelowMinimum).Inc()
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
					metrics.Errors.WithLabelValues(metrics.ErrorTypeGetMetricsPublisherAccountBalance).Inc()
					continue
				}
				balanceLamports := balance.Value
				f.log.Debug("Metrics publisher balance", "device", devicePK, "metricsPublisher", metricsPublisherPK, "balanceLamports", balanceLamports, "minBalanceLamports", minBalanceLamports)

				// If balance is below minimum, top it up.
				if balanceLamports < minBalanceLamports {
					f.log.Info("Topping up metrics publisher", "device", devicePK, "metricsPublisher", metricsPublisherPK, "balanceLamports", balanceLamports, "topUpLamports", topUpLamports)

					_, err := transferFunds(ctx, f.cfg.Solana, f.cfg.Signer, metricsPublisherPK, topUpLamports, nil)
					if err != nil {
						f.log.Error("Failed to transfer SOL", "error", err)
						metrics.Errors.WithLabelValues(metrics.ErrorTypeTransferFundsToMetricsPublisher).Inc()
						continue
					}

					// Wait for the transfer to complete.
					f.log.Debug("Waiting for balance", "account", metricsPublisherPK, "expected", minBalanceLamports, "current", balanceLamports)
					err = waitForBalance(ctx, f.cfg.Solana, metricsPublisherPK, minBalanceLamports, f.cfg.WaitForBalanceTimeout, f.cfg.WaitForBalancePollInterval)
					if err != nil {
						f.log.Error("Failed to wait for balance", "error", err)
						metrics.Errors.WithLabelValues(metrics.ErrorTypeWaitForMetricsPublisherBalance).Inc()
						continue
					}

					f.log.Info("Transferred SOL to metrics publisher", "device", devicePK, "metricsPublisher", metricsPublisherPK, "topUpLamports", topUpLamports)
				}
			}
		}
	}
}

func waitForBalance(ctx context.Context, client SolanaClient, account solana.PublicKey, minBalanceLamports uint64, timeout time.Duration, pollInterval time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		balance, err := client.GetBalance(ctx, account, solanarpc.CommitmentFinalized)
		if err != nil {
			return fmt.Errorf("failed to get balance: %w", err)
		}
		balanceLamports := balance.Value
		if balanceLamports >= minBalanceLamports {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("timeout waiting for balance: account=%s, expected balance=%d, current balance=%d", account, minBalanceLamports, balanceLamports)
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
