package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// ProgramError reports a transaction that finalized onchain while the program rejected the
// instruction it carried. The instruction did not take effect, and re-sending it unchanged will be
// rejected the same way, so callers should treat it as a permanent failure for that input rather
// than retry it.
type ProgramError struct {
	// Err is the transaction error the ledger reported, e.g.
	// map[InstructionError:[0 map[Custom:1001]]] for TelemetryError::UnauthorizedAgent.
	Err any

	// Logs is the program's log output for the transaction. It is empty when the RPC returned the
	// failure on the signature status but could not return the transaction itself.
	Logs []string
}

func (e *ProgramError) Error() string {
	if msgs := e.ProgramLogMessages(); len(msgs) > 0 {
		return fmt.Sprintf("transaction finalized with program error: %v (program logs: %v)", e.Err, msgs)
	}
	return fmt.Sprintf("transaction finalized with program error: %v", e.Err)
}

// ProgramLogMessages returns the program's own log lines with the runtime's invoke/consumed/success
// boilerplate and the instruction-name echo removed. These are the lines that say why the program
// rejected the instruction, e.g. "Agent <pubkey> is not authorized for origin device <pubkey>".
func (e *ProgramError) ProgramLogMessages() []string {
	var msgs []string
	for _, line := range e.Logs {
		msg, ok := strings.CutPrefix(line, "Program log: ")
		if !ok || strings.HasPrefix(msg, "Instruction: ") {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

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
		// A program rejection is not a failure to read the transaction; pass it through so the
		// reason stays at the front of the message.
		var programErr *ProgramError
		if errors.As(err, &programErr) {
			return solana.Signature{}, nil, err
		}
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
	var finalStatus *solanarpc.SignatureStatusesResult
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
			finalStatus = status
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

	// Finalization only says the cluster agreed on the transaction, not that the program accepted
	// it: a rejected instruction finalizes and carries the rejection in Err. Reporting that as
	// success leaves the caller believing an account it never got was written, and the program
	// error never reaches the log.
	if finalStatus.Err != nil {
		return nil, &ProgramError{Err: finalStatus.Err, Logs: e.transactionLogs(ctx, sig)}
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
	// The same rejection is carried on the transaction metadata. Checked here as well because the
	// two come from separate RPC calls, and a node that omits it on the status still reports it here.
	if tx.Meta.Err != nil {
		return nil, &ProgramError{Err: tx.Meta.Err, Logs: tx.Meta.LogMessages}
	}
	return tx, nil
}

// transactionLogs fetches the program logs for a finalized transaction, best effort. They are
// context for a failure that is already known, so a node that cannot return the transaction costs
// the logs rather than replacing the program error with an RPC error.
func (e *executor) transactionLogs(ctx context.Context, sig solana.Signature) []string {
	tx, err := e.rpc.GetTransaction(ctx, sig, &solanarpc.GetTransactionOpts{
		Encoding:   solana.EncodingBase64,
		Commitment: solanarpc.CommitmentFinalized,
	})
	if err != nil || tx == nil || tx.Meta == nil {
		e.log.Debug("--> Could not fetch program logs for failed transaction", "sig", sig, "error", err)
		return nil
	}
	return tx.Meta.LogMessages
}
