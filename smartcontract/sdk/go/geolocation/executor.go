package geolocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

var (
	// ErrNoPrivateKey is returned when a transaction signing operation is attempted without a configured private key.
	ErrNoPrivateKey = errors.New("no private key configured")

	// ErrNoProgramID is returned when a transaction signing operation is attempted without a configured program ID.
	ErrNoProgramID = errors.New("no program ID configured")
)

type executor struct {
	log                   *slog.Logger
	rpc                   RPCClient
	signer                *solana.PrivateKey
	programID             solana.PublicKey
	waitForVisibleTimeout time.Duration
}

type ExecutorOption func(*executor)

func WithWaitForVisibleTimeout(timeout time.Duration) ExecutorOption {
	return func(e *executor) {
		e.waitForVisibleTimeout = timeout
	}
}

func NewExecutor(log *slog.Logger, rpc RPCClient, signer *solana.PrivateKey, programID solana.PublicKey, opts ...ExecutorOption) *executor {
	e := &executor{
		log:                   log,
		rpc:                   rpc,
		signer:                signer,
		programID:             programID,
		waitForVisibleTimeout: 3 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

type ExecuteTransactionOptions struct {
	SkipPreflight bool
}

func (e *executor) ExecuteTransaction(ctx context.Context, instruction solana.Instruction, opts *ExecuteTransactionOptions) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return e.ExecuteTransactions(ctx, []solana.Instruction{instruction}, opts)
}

func (e *executor) ExecuteTransactions(ctx context.Context, instructions []solana.Instruction, opts *ExecuteTransactionOptions) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if opts == nil {
		opts = &ExecuteTransactionOptions{}
	}

	if e.signer == nil {
		return solana.Signature{}, nil, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, nil, ErrNoProgramID
	}

	// Get latest blockhash
	blockhashResult, err := e.rpc.GetLatestBlockhash(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

	// Build transaction
	tx, err := solana.NewTransaction(
		instructions,
		blockhashResult.Value.Blockhash,
		solana.TransactionPayer(e.signer.PublicKey()),
	)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to build transaction: %w", err)
	}
	if tx == nil {
		return solana.Signature{}, nil, errors.New("transaction build failed: nil result")
	}

	// Sign transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(e.signer.PublicKey()) {
			return e.signer
		}
		return nil
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to sign transaction (likely missing signer): %w", err)
	}
	if len(tx.Signatures) == 0 {
		return solana.Signature{}, nil, errors.New("signed transaction appears malformed")
	}

	// Send transaction
	sig, err := e.rpc.SendTransactionWithOpts(ctx, tx, solanarpc.TransactionOpts{
		SkipPreflight: opts.SkipPreflight,
	})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// Wait for the signature to be visible
	err = e.waitForSignatureVisible(ctx, sig, e.waitForVisibleTimeout)
	if err != nil {
		if opts.SkipPreflight {
			return solana.Signature{}, nil, fmt.Errorf("transaction dropped or rejected before cluster saw it. make sure you have sufficient funds for the transaction: %w", err)
		}
		return solana.Signature{}, nil, fmt.Errorf("transaction dropped or rejected before cluster saw it: %w", err)
	}

	// Wait for the transaction to be finalized
	res, err := e.waitForTransactionFinalized(ctx, sig)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	return sig, res, nil
}

func (e *executor) waitForSignatureVisible(ctx context.Context, sig solana.Signature, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := e.rpc.GetSignatureStatuses(ctx, true, sig)
		if err != nil {
			return err
		}
		if len(resp.Value) > 0 && resp.Value[0] != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("signature not found after wait")
}

func (e *executor) waitForTransactionFinalized(ctx context.Context, sig solana.Signature) (*solanarpc.GetTransactionResult, error) {
	e.log.Debug("--> Waiting for transaction to be finalized", "sig", sig)
	start := time.Now()
	for {
		statusResp, err := e.rpc.GetSignatureStatuses(ctx, true, sig)
		if err != nil {
			return nil, err
		}
		if len(statusResp.Value) == 0 {
			return nil, errors.New("transaction not found")
		}
		status := statusResp.Value[0]
		if status != nil && status.ConfirmationStatus == solanarpc.ConfirmationStatusFinalized {
			e.log.Debug("--> Transaction finalized", "sig", sig, "duration", time.Since(start))
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
			if time.Since(start)/time.Second%5 == 0 {
				e.log.Debug("--> Still waiting for transaction to be finalized", "sig", sig, "elapsed", time.Since(start))
			}
		}
	}

	tx, err := e.rpc.GetTransaction(ctx, sig, &solanarpc.GetTransactionOpts{
		Encoding:   solana.EncodingBase64,
		Commitment: solanarpc.CommitmentFinalized,
	})
	if err != nil {
		return nil, err
	}
	if tx == nil || tx.Meta == nil {
		return nil, errors.New("transaction not found or missing metadata after finalization")
	}
	return tx, nil
}
