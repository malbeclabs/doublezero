package serviceability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const (
	instructionSetDeviceHealth = 83
	instructionSetLinkHealth   = 84
)

var (
	ErrNoPrivateKey      = errors.New("no private key configured")
	ErrNoProgramID       = errors.New("no program ID configured")
	ErrAllUpdatesFailed  = errors.New("all updates in batch failed")
	ErrInstructionFailed = errors.New("instruction failed")
)

type Executor struct {
	log                   *slog.Logger
	rpc                   ExecutorRPCClient
	signer                *solana.PrivateKey
	programID             solana.PublicKey
	waitForVisibleTimeout time.Duration
}

type ExecutorRPCClient interface {
	GetLatestBlockhash(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	SendTransactionWithOpts(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error)
	GetSignatureStatuses(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error)
	GetTransaction(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error)
}

type ExecutorOption func(*Executor)

func WithWaitForVisibleTimeout(timeout time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.waitForVisibleTimeout = timeout
	}
}

func NewExecutor(log *slog.Logger, rpc ExecutorRPCClient, signer *solana.PrivateKey, programID solana.PublicKey, opts ...ExecutorOption) *Executor {
	e := &Executor{
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

type DeviceHealthUpdate struct {
	DevicePubkey solana.PublicKey
	Health       DeviceHealth
}

type LinkHealthUpdate struct {
	LinkPubkey solana.PublicKey
	Health     LinkHealth
}

func (e *Executor) SetDeviceHealthBatch(ctx context.Context, updates []DeviceHealthUpdate, globalStatePubkey solana.PublicKey) (solana.Signature, error) {
	if len(updates) == 0 {
		return solana.Signature{}, nil
	}

	remaining := updates
	var lastSig solana.Signature

	for len(remaining) > 0 {
		instructions := make([]solana.Instruction, len(remaining))
		for i, update := range remaining {
			instructions[i] = e.buildSetDeviceHealthInstruction(update.DevicePubkey, globalStatePubkey, update.Health)
		}

		sig, _, err := e.executeTransaction(ctx, instructions)
		if err == nil {
			return sig, nil
		}
		lastSig = sig

		failingIdx, parseErr := parseFailingInstructionIndex(err)
		if parseErr != nil {
			return sig, err
		}

		if failingIdx < 0 || failingIdx >= len(remaining) {
			return sig, fmt.Errorf("invalid failing instruction index %d for batch size %d: %w", failingIdx, len(remaining), err)
		}

		failedUpdate := remaining[failingIdx]
		e.log.Warn("Device health update failed, removing from batch and retrying",
			"failingIndex", failingIdx,
			"devicePubkey", failedUpdate.DevicePubkey.String(),
			"remainingBefore", len(remaining),
			"error", err)

		remaining = append(remaining[:failingIdx], remaining[failingIdx+1:]...)
	}

	return lastSig, ErrAllUpdatesFailed
}

func (e *Executor) SetLinkHealthBatch(ctx context.Context, updates []LinkHealthUpdate, globalStatePubkey solana.PublicKey) (solana.Signature, error) {
	if len(updates) == 0 {
		return solana.Signature{}, nil
	}

	remaining := updates
	var lastSig solana.Signature

	for len(remaining) > 0 {
		instructions := make([]solana.Instruction, len(remaining))
		for i, update := range remaining {
			instructions[i] = e.buildSetLinkHealthInstruction(update.LinkPubkey, globalStatePubkey, update.Health)
		}

		sig, _, err := e.executeTransaction(ctx, instructions)
		if err == nil {
			return sig, nil
		}
		lastSig = sig

		failingIdx, parseErr := parseFailingInstructionIndex(err)
		if parseErr != nil {
			return sig, err
		}

		if failingIdx < 0 || failingIdx >= len(remaining) {
			return sig, fmt.Errorf("invalid failing instruction index %d for batch size %d: %w", failingIdx, len(remaining), err)
		}

		failedUpdate := remaining[failingIdx]
		e.log.Warn("Link health update failed, removing from batch and retrying",
			"failingIndex", failingIdx,
			"linkPubkey", failedUpdate.LinkPubkey.String(),
			"remainingBefore", len(remaining),
			"error", err)

		remaining = append(remaining[:failingIdx], remaining[failingIdx+1:]...)
	}

	return lastSig, ErrAllUpdatesFailed
}

func (e *Executor) buildSetDeviceHealthInstruction(devicePubkey, globalStatePubkey solana.PublicKey, health DeviceHealth) solana.Instruction {
	return &genericInstruction{
		programID: e.programID,
		accounts: solana.AccountMetaSlice{
			solana.Meta(devicePubkey).WRITE(),
			solana.Meta(globalStatePubkey),
			solana.Meta(e.signer.PublicKey()).SIGNER().WRITE(),
			solana.Meta(solana.SystemProgramID),
		},
		data: []byte{instructionSetDeviceHealth, byte(health)},
	}
}

func (e *Executor) buildSetLinkHealthInstruction(linkPubkey, globalStatePubkey solana.PublicKey, health LinkHealth) solana.Instruction {
	return &genericInstruction{
		programID: e.programID,
		accounts: solana.AccountMetaSlice{
			solana.Meta(linkPubkey).WRITE(),
			solana.Meta(globalStatePubkey),
			solana.Meta(e.signer.PublicKey()).SIGNER().WRITE(),
			solana.Meta(solana.SystemProgramID),
		},
		data: []byte{instructionSetLinkHealth, byte(health)},
	}
}

type genericInstruction struct {
	programID solana.PublicKey
	accounts  solana.AccountMetaSlice
	data      []byte
}

func (i *genericInstruction) ProgramID() solana.PublicKey {
	return i.programID
}

func (i *genericInstruction) Accounts() []*solana.AccountMeta {
	return i.accounts
}

func (i *genericInstruction) Data() ([]byte, error) {
	return i.data, nil
}

func (e *Executor) executeTransaction(ctx context.Context, instructions []solana.Instruction) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if e.signer == nil {
		return solana.Signature{}, nil, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, nil, ErrNoProgramID
	}

	blockhashResult, err := e.rpc.GetLatestBlockhash(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to get latest blockhash: %w", err)
	}

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

	sig, err := e.rpc.SendTransactionWithOpts(ctx, tx, solanarpc.TransactionOpts{})
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	err = e.waitForSignatureVisible(ctx, sig, e.waitForVisibleTimeout)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("transaction dropped or rejected before cluster saw it: %w", err)
	}

	res, err := e.waitForTransactionFinalized(ctx, sig)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	return sig, res, nil
}

func (e *Executor) waitForSignatureVisible(ctx context.Context, sig solana.Signature, timeout time.Duration) error {
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

func (e *Executor) waitForTransactionFinalized(ctx context.Context, sig solana.Signature) (*solanarpc.GetTransactionResult, error) {
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

func GetGlobalStatePDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{
			[]byte("doublezero"),
			[]byte("globalstate"),
		},
		programID,
	)
}

// parseFailingInstructionIndex extracts the failing instruction index from a Solana RPC error.
// Solana returns errors in the format: {"err": {"InstructionError": [index, errorDetails]}}
func parseFailingInstructionIndex(err error) (int, error) {
	var rpcErr *jsonrpc.RPCError
	if !errors.As(err, &rpcErr) {
		return -1, fmt.Errorf("not an RPC error: %w", ErrInstructionFailed)
	}

	data, ok := rpcErr.Data.(map[string]any)
	if !ok {
		return -1, fmt.Errorf("unexpected RPC error data type: %w", ErrInstructionFailed)
	}

	errField, ok := data["err"]
	if !ok {
		return -1, fmt.Errorf("no err field in RPC error: %w", ErrInstructionFailed)
	}

	errMap, ok := errField.(map[string]any)
	if !ok {
		return -1, fmt.Errorf("err field is not a map: %w", ErrInstructionFailed)
	}

	instructionError, ok := errMap["InstructionError"].([]any)
	if !ok || len(instructionError) < 2 {
		return -1, fmt.Errorf("no InstructionError in err: %w", ErrInstructionFailed)
	}

	// The first element is the instruction index
	switch idx := instructionError[0].(type) {
	case json.Number:
		i, err := idx.Int64()
		if err != nil {
			return -1, fmt.Errorf("failed to parse instruction index: %w", ErrInstructionFailed)
		}
		return int(i), nil
	case float64:
		return int(idx), nil
	default:
		return -1, fmt.Errorf("unexpected instruction index type %T: %w", idx, ErrInstructionFailed)
	}
}
