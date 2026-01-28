package serviceability

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRPCClient struct {
	getLatestBlockhashFunc   func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	sendTransactionFunc      func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error)
	getSignatureStatusesFunc func(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error)
	getTransactionFunc       func(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error)
	sentTransactions         []*solana.Transaction
}

func (m *mockRPCClient) GetLatestBlockhash(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
	if m.getLatestBlockhashFunc != nil {
		return m.getLatestBlockhashFunc(ctx, commitment)
	}
	return &solanarpc.GetLatestBlockhashResult{
		Value: &solanarpc.LatestBlockhashResult{
			Blockhash: solana.MustHashFromBase58("4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM"),
		},
	}, nil
}

func (m *mockRPCClient) SendTransactionWithOpts(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
	m.sentTransactions = append(m.sentTransactions, transaction)
	if m.sendTransactionFunc != nil {
		return m.sendTransactionFunc(ctx, transaction, opts)
	}
	return solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"), nil
}

func (m *mockRPCClient) GetSignatureStatuses(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
	if m.getSignatureStatusesFunc != nil {
		return m.getSignatureStatusesFunc(ctx, searchTransactionHistory, transactionSignatures...)
	}
	return &solanarpc.GetSignatureStatusesResult{
		Value: []*solanarpc.SignatureStatusesResult{
			{ConfirmationStatus: solanarpc.ConfirmationStatusFinalized},
		},
	}, nil
}

func (m *mockRPCClient) GetTransaction(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error) {
	if m.getTransactionFunc != nil {
		return m.getTransactionFunc(ctx, txSig, opts)
	}
	return &solanarpc.GetTransactionResult{
		Meta: &solanarpc.TransactionMeta{},
	}, nil
}

func newTestExecutor(t *testing.T, rpc *mockRPCClient) (*Executor, solana.PrivateKey) {
	t.Helper()
	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	log := slog.Default()
	return NewExecutor(log, rpc, &signer, programID), signer
}

func TestNewExecutor(t *testing.T) {
	t.Parallel()

	t.Run("creates executor with defaults", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		assert.NotNil(t, executor)
		assert.Equal(t, 3*time.Second, executor.waitForVisibleTimeout)
	})

	t.Run("applies options", func(t *testing.T) {
		rpc := &mockRPCClient{}
		signer := solana.NewWallet().PrivateKey
		programID := solana.NewWallet().PublicKey()
		log := slog.Default()

		executor := NewExecutor(log, rpc, &signer, programID, WithWaitForVisibleTimeout(10*time.Second))

		assert.Equal(t, 10*time.Second, executor.waitForVisibleTimeout)
	})
}

func TestSetDeviceHealthBatch(t *testing.T) {
	t.Parallel()

	globalStatePubkey := solana.NewWallet().PublicKey()

	t.Run("returns zero signature for empty updates", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		sig, err := executor.SetDeviceHealthBatch(context.Background(), []DeviceHealthUpdate{}, globalStatePubkey)

		require.NoError(t, err)
		assert.Equal(t, solana.Signature{}, sig)
		assert.Empty(t, rpc.sentTransactions)
	})

	t.Run("sends transaction with single device update", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		devicePubkey := solana.NewWallet().PublicKey()
		updates := []DeviceHealthUpdate{
			{DevicePubkey: devicePubkey, Health: DeviceHealthReadyForUsers},
		}

		sig, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		require.Len(t, rpc.sentTransactions, 1)

		tx := rpc.sentTransactions[0]
		assert.Len(t, tx.Message.Instructions, 1)
	})

	t.Run("sends transaction with multiple device updates", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForLinks},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthPending},
		}

		sig, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		require.Len(t, rpc.sentTransactions, 1)

		tx := rpc.sentTransactions[0]
		assert.Len(t, tx.Message.Instructions, 3)
	})

	t.Run("returns error when RPC fails", func(t *testing.T) {
		rpc := &mockRPCClient{
			getLatestBlockhashFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return nil, errors.New("RPC error")
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get latest blockhash")
	})
}

func TestSetLinkHealthBatch(t *testing.T) {
	t.Parallel()

	globalStatePubkey := solana.NewWallet().PublicKey()

	t.Run("returns zero signature for empty updates", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		sig, err := executor.SetLinkHealthBatch(context.Background(), []LinkHealthUpdate{}, globalStatePubkey)

		require.NoError(t, err)
		assert.Equal(t, solana.Signature{}, sig)
		assert.Empty(t, rpc.sentTransactions)
	})

	t.Run("sends transaction with single link update", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		linkPubkey := solana.NewWallet().PublicKey()
		updates := []LinkHealthUpdate{
			{LinkPubkey: linkPubkey, Health: LinkHealthReadyForService},
		}

		sig, err := executor.SetLinkHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		require.Len(t, rpc.sentTransactions, 1)

		tx := rpc.sentTransactions[0]
		assert.Len(t, tx.Message.Instructions, 1)
	})

	t.Run("sends transaction with multiple link updates", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)

		updates := []LinkHealthUpdate{
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthReadyForService},
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthPending},
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthImpaired},
		}

		sig, err := executor.SetLinkHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		require.Len(t, rpc.sentTransactions, 1)

		tx := rpc.sentTransactions[0]
		assert.Len(t, tx.Message.Instructions, 3)
	})
}

func TestBuildSetDeviceHealthInstruction(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, signer := newTestExecutor(t, rpc)

	devicePubkey := solana.NewWallet().PublicKey()
	globalStatePubkey := solana.NewWallet().PublicKey()

	instruction := executor.buildSetDeviceHealthInstruction(devicePubkey, globalStatePubkey, DeviceHealthReadyForUsers)

	assert.Equal(t, executor.programID, instruction.ProgramID())

	accounts := instruction.Accounts()
	require.Len(t, accounts, 4)
	assert.Equal(t, devicePubkey, accounts[0].PublicKey)
	assert.True(t, accounts[0].IsWritable)
	assert.Equal(t, globalStatePubkey, accounts[1].PublicKey)
	assert.Equal(t, signer.PublicKey(), accounts[2].PublicKey)
	assert.True(t, accounts[2].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accounts[3].PublicKey)

	data, err := instruction.Data()
	require.NoError(t, err)
	assert.Equal(t, []byte{instructionSetDeviceHealth, byte(DeviceHealthReadyForUsers)}, data)
}

func TestBuildSetLinkHealthInstruction(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, signer := newTestExecutor(t, rpc)

	linkPubkey := solana.NewWallet().PublicKey()
	globalStatePubkey := solana.NewWallet().PublicKey()

	instruction := executor.buildSetLinkHealthInstruction(linkPubkey, globalStatePubkey, LinkHealthReadyForService)

	assert.Equal(t, executor.programID, instruction.ProgramID())

	accounts := instruction.Accounts()
	require.Len(t, accounts, 4)
	assert.Equal(t, linkPubkey, accounts[0].PublicKey)
	assert.True(t, accounts[0].IsWritable)
	assert.Equal(t, globalStatePubkey, accounts[1].PublicKey)
	assert.Equal(t, signer.PublicKey(), accounts[2].PublicKey)
	assert.True(t, accounts[2].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accounts[3].PublicKey)

	data, err := instruction.Data()
	require.NoError(t, err)
	assert.Equal(t, []byte{instructionSetLinkHealth, byte(LinkHealthReadyForService)}, data)
}

func TestExecuteTransaction_ErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("returns error when program ID is zero", func(t *testing.T) {
		rpc := &mockRPCClient{}
		signer := solana.NewWallet().PrivateKey
		executor := NewExecutor(slog.Default(), rpc, &signer, solana.PublicKey{})

		globalStatePubkey := solana.NewWallet().PublicKey()
		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoProgramID)
	})

	t.Run("returns error when send transaction fails", func(t *testing.T) {
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				return solana.Signature{}, errors.New("send failed")
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		globalStatePubkey := solana.NewWallet().PublicKey()
		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send transaction")
	})

	t.Run("returns error when signature not visible", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			getSignatureStatusesFunc: func(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error) {
				callCount++
				return &solanarpc.GetSignatureStatusesResult{
					Value: []*solanarpc.SignatureStatusesResult{nil},
				}, nil
			},
		}
		signer := solana.NewWallet().PrivateKey
		programID := solana.NewWallet().PublicKey()
		executor := NewExecutor(slog.Default(), rpc, &signer, programID, WithWaitForVisibleTimeout(100*time.Millisecond))

		globalStatePubkey := solana.NewWallet().PublicKey()
		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "transaction dropped or rejected")
	})
}

func TestGetGlobalStatePDA(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	pda, bump, err := GetGlobalStatePDA(programID)

	require.NoError(t, err)
	assert.NotEqual(t, solana.PublicKey{}, pda)
	assert.Greater(t, bump, uint8(0))

	// Verify deterministic - same inputs should produce same outputs
	pda2, bump2, err := GetGlobalStatePDA(programID)
	require.NoError(t, err)
	assert.Equal(t, pda, pda2)
	assert.Equal(t, bump, bump2)
}

func makeInstructionError(instructionIndex int) *jsonrpc.RPCError {
	return &jsonrpc.RPCError{
		Code:    -32000,
		Message: "Transaction simulation failed",
		Data: map[string]any{
			"err": map[string]any{
				"InstructionError": []any{
					json.Number(string(rune('0' + instructionIndex))),
					map[string]any{
						"Custom": json.Number("1001"),
					},
				},
			},
		},
	}
}

func TestSetDeviceHealthBatch_RetryOnFailure(t *testing.T) {
	t.Parallel()

	globalStatePubkey := solana.NewWallet().PublicKey()

	t.Run("retries batch after removing failing instruction", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				callCount++
				if callCount == 1 {
					// First call fails on instruction index 1
					return solana.Signature{}, makeInstructionError(1)
				}
				// Second call succeeds
				return solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"), nil
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		device1 := solana.NewWallet().PublicKey()
		device2 := solana.NewWallet().PublicKey() // This one will fail
		device3 := solana.NewWallet().PublicKey()
		updates := []DeviceHealthUpdate{
			{DevicePubkey: device1, Health: DeviceHealthReadyForUsers},
			{DevicePubkey: device2, Health: DeviceHealthReadyForUsers},
			{DevicePubkey: device3, Health: DeviceHealthReadyForUsers},
		}

		sig, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		assert.Equal(t, 2, callCount, "should have retried once")
		require.Len(t, rpc.sentTransactions, 2)

		// First transaction should have 3 instructions
		assert.Len(t, rpc.sentTransactions[0].Message.Instructions, 3)
		// Second transaction should have 2 instructions (device2 removed)
		assert.Len(t, rpc.sentTransactions[1].Message.Instructions, 2)
	})

	t.Run("retries multiple times until success", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				callCount++
				switch callCount {
				case 1:
					return solana.Signature{}, makeInstructionError(2) // Fail on index 2
				case 2:
					return solana.Signature{}, makeInstructionError(0) // Fail on index 0
				default:
					return solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"), nil
				}
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		sig, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		assert.Equal(t, 3, callCount, "should have retried twice")
		require.Len(t, rpc.sentTransactions, 3)

		assert.Len(t, rpc.sentTransactions[0].Message.Instructions, 3)
		assert.Len(t, rpc.sentTransactions[1].Message.Instructions, 2)
		assert.Len(t, rpc.sentTransactions[2].Message.Instructions, 1)
	})

	t.Run("returns ErrAllUpdatesFailed when all updates fail", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				callCount++
				return solana.Signature{}, makeInstructionError(0) // Always fail on first instruction
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAllUpdatesFailed)
		assert.Equal(t, 2, callCount, "should have tried for each update")
	})

	t.Run("does not retry on non-instruction errors", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				callCount++
				return solana.Signature{}, errors.New("network error")
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []DeviceHealthUpdate{
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
			{DevicePubkey: solana.NewWallet().PublicKey(), Health: DeviceHealthReadyForUsers},
		}

		_, err := executor.SetDeviceHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.Equal(t, 1, callCount, "should not have retried")
		assert.Contains(t, err.Error(), "failed to send transaction")
	})
}

func TestSetLinkHealthBatch_RetryOnFailure(t *testing.T) {
	t.Parallel()

	globalStatePubkey := solana.NewWallet().PublicKey()

	t.Run("retries batch after removing failing instruction", func(t *testing.T) {
		callCount := 0
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				callCount++
				if callCount == 1 {
					return solana.Signature{}, makeInstructionError(0)
				}
				return solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"), nil
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []LinkHealthUpdate{
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthReadyForService},
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthReadyForService},
		}

		sig, err := executor.SetLinkHealthBatch(context.Background(), updates, globalStatePubkey)

		require.NoError(t, err)
		assert.NotEqual(t, solana.Signature{}, sig)
		assert.Equal(t, 2, callCount)
		require.Len(t, rpc.sentTransactions, 2)

		assert.Len(t, rpc.sentTransactions[0].Message.Instructions, 2)
		assert.Len(t, rpc.sentTransactions[1].Message.Instructions, 1)
	})

	t.Run("returns ErrAllUpdatesFailed when all updates fail", func(t *testing.T) {
		rpc := &mockRPCClient{
			sendTransactionFunc: func(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				return solana.Signature{}, makeInstructionError(0)
			},
		}
		executor, _ := newTestExecutor(t, rpc)

		updates := []LinkHealthUpdate{
			{LinkPubkey: solana.NewWallet().PublicKey(), Health: LinkHealthReadyForService},
		}

		_, err := executor.SetLinkHealthBatch(context.Background(), updates, globalStatePubkey)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAllUpdatesFailed)
	})
}

func TestParseFailingInstructionIndex(t *testing.T) {
	t.Parallel()

	t.Run("parses instruction index from json.Number", func(t *testing.T) {
		err := makeInstructionError(5)

		idx, parseErr := parseFailingInstructionIndex(err)

		require.NoError(t, parseErr)
		assert.Equal(t, 5, idx)
	})

	t.Run("parses instruction index from float64", func(t *testing.T) {
		err := &jsonrpc.RPCError{
			Code:    -32000,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						float64(3),
						map[string]any{"Custom": float64(1001)},
					},
				},
			},
		}

		idx, parseErr := parseFailingInstructionIndex(err)

		require.NoError(t, parseErr)
		assert.Equal(t, 3, idx)
	})

	t.Run("returns error for non-RPC error", func(t *testing.T) {
		err := errors.New("not an RPC error")

		_, parseErr := parseFailingInstructionIndex(err)

		require.Error(t, parseErr)
		assert.ErrorIs(t, parseErr, ErrInstructionFailed)
	})

	t.Run("returns error for missing err field", func(t *testing.T) {
		err := &jsonrpc.RPCError{
			Code:    -32000,
			Message: "error",
			Data:    map[string]any{},
		}

		_, parseErr := parseFailingInstructionIndex(err)

		require.Error(t, parseErr)
		assert.ErrorIs(t, parseErr, ErrInstructionFailed)
	})

	t.Run("returns error for missing InstructionError", func(t *testing.T) {
		err := &jsonrpc.RPCError{
			Code:    -32000,
			Message: "error",
			Data: map[string]any{
				"err": map[string]any{
					"SomeOtherError": "value",
				},
			},
		}

		_, parseErr := parseFailingInstructionIndex(err)

		require.Error(t, parseErr)
		assert.ErrorIs(t, parseErr, ErrInstructionFailed)
	})
}
