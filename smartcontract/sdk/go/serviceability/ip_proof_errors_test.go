package serviceability

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// customErr builds the RPC error shape a serviceability program rejection
// arrives in: InstructionError: [idx, {Custom: code}].
func customErr(code uint32) error {
	return &jsonrpc.RPCError{
		Code:    -32002,
		Message: "Transaction simulation failed",
		Data: map[string]any{
			"err": map[string]any{
				"InstructionError": []any{
					json.Number("0"),
					map[string]any{"Custom": json.Number(fmt.Sprint(code))},
				},
			},
		},
	}
}

func TestProgramErrorNamesCoverRFC27(t *testing.T) {
	t.Parallel()

	// Every RFC-27 rejection class must have a name, or an operator sees a bare
	// code. Codes come from DoubleZeroError in
	// smartcontract/programs/doublezero-serviceability/src/error.rs.
	want := map[uint32]string{
		105: "IpOwnershipProofRequired",
		106: "IpVerifierNotConfigured",
		107: "IpProofPayerMismatch",
		108: "IpProofClientIpMismatch",
		109: "IpProofUserTypeMismatch",
		110: "IpProofEpochOutOfWindow",
		111: "IpProofInstructionsSysvarMissing",
		112: "IpProofEd25519InstructionMissing",
		113: "IpProofEd25519OffsetsInvalid",
		114: "IpProofSignatureCountInvalid",
		115: "IpProofVerifierKeyMismatch",
		116: "IpProofSignatureMismatch",
		117: "IpProofMessageMismatch",
		118: "IpProofVersionUnsupported",
	}
	for code, name := range want {
		assert.Equal(t, name, ProgramErrorMessage(code), "code %d", code)
	}
}

func TestClassifyProgramErrorMapsIPOwnershipProofRequired(t *testing.T) {
	t.Parallel()

	err := ClassifyProgramError(customErr(105))
	require.Error(t, err)

	// The named sentinel matches...
	assert.ErrorIs(t, err, ErrIPOwnershipProofRequired)
	// ...and only that one.
	assert.NotErrorIs(t, err, ErrIPProofEpochOutOfWindow)

	// The original RPC error stays reachable.
	var rpcErr *jsonrpc.RPCError
	assert.ErrorAs(t, err, &rpcErr)

	// The message names the error rather than dumping the RPC blob.
	assert.Contains(t, err.Error(), "IpOwnershipProofRequired")

	pe, ok := AsProgramError(err)
	require.True(t, ok)
	assert.Equal(t, uint32(105), pe.Code)
}

func TestClassifyProgramErrorPassesThroughUnrelatedErrors(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ClassifyProgramError(nil))

	plain := errors.New("connection refused")
	assert.Equal(t, plain, ClassifyProgramError(plain))

	_, ok := AsProgramError(plain)
	assert.False(t, ok)
}

// globalStateBytes encodes a GlobalState account with the given feature flags,
// in the field order DeserializeGlobalState reads.
func globalStateBytes(featureFlags uint64) []byte {
	var b []byte
	b = append(b, 1, 254)              // account_type, bump_seed
	b = append(b, make([]byte, 16)...) // account_index u128
	b = append(b, 0, 0, 0, 0)          // foundation_allowlist len
	b = append(b, 0, 0, 0, 0)          // deprecated device_allowlist len
	b = append(b, 0, 0, 0, 0)          // deprecated user_allowlist len
	b = append(b, make([]byte, 32)...) // activator_authority_pk
	b = append(b, make([]byte, 32)...) // sentinel_authority_pk
	b = append(b, make([]byte, 8)...)  // contributor_airdrop_lamports
	b = append(b, make([]byte, 8)...)  // user_airdrop_lamports
	b = append(b, make([]byte, 32)...) // health_oracle_pk
	b = append(b, 0, 0, 0, 0)          // qa_allowlist len
	flags := make([]byte, 16)          // feature_flags u128, little-endian
	binary.LittleEndian.PutUint64(flags[:8], featureFlags)
	b = append(b, flags...)
	b = append(b, make([]byte, 32)...) // feed_authority_pk
	b = append(b, make([]byte, 32)...) // ip_verifier_authority_pk
	return b
}

func TestGlobalStateIsFeatureEnabled(t *testing.T) {
	t.Parallel()

	// Round-trip through the real deserializer so the Lo64 wart is exercised
	// rather than assumed.
	parse := func(flags uint64) *GlobalState {
		var gs GlobalState
		DeserializeGlobalState(NewByteReader(globalStateBytes(flags)), &gs)
		return &gs
	}

	none := parse(0)
	assert.False(t, none.IsFeatureEnabled(FeatureRequireIPOwnershipProof))
	assert.False(t, none.IsFeatureEnabled(FeatureRequirePermissionAccounts))

	// Bit 2 only.
	ipOnly := parse(1 << 2)
	assert.True(t, ipOnly.IsFeatureEnabled(FeatureRequireIPOwnershipProof))
	assert.False(t, ipOnly.IsFeatureEnabled(FeatureRequirePermissionAccounts))
	assert.False(t, ipOnly.IsFeatureEnabled(FeatureOnChainAllocationDeprecated))

	// Bit 1 must not be mistaken for bit 2.
	permOnly := parse(1 << 1)
	assert.False(t, permOnly.IsFeatureEnabled(FeatureRequireIPOwnershipProof))
	assert.True(t, permOnly.IsFeatureEnabled(FeatureRequirePermissionAccounts))

	var nilGS *GlobalState
	assert.False(t, nilGS.IsFeatureEnabled(FeatureRequireIPOwnershipProof))

	assert.Equal(t, "require-ip-ownership-proof", FeatureRequireIPOwnershipProof.String())
}

// globalStateRPC serves a GlobalState account carrying the given flags for the
// program's global-state PDA, and reports how many times it was read.
func globalStateRPC(t *testing.T, programID solana.PublicKey, flags uint64, reads *int) *mockRPCClient {
	t.Helper()
	pda, _, err := GetGlobalStatePDA(programID)
	require.NoError(t, err)
	return &mockRPCClient{
		getAccountInfoFunc: func(_ context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			if account.Equals(pda) {
				*reads++
				return &solanarpc.GetAccountInfoResult{
					Value: &solanarpc.Account{
						Data: solanarpc.DataBytesOrJSONFromBytes(globalStateBytes(flags)),
					},
				}, nil
			}
			return nil, solanarpc.ErrNotFound
		},
	}
}

func TestCreateUserRefusesWhenProofRequired(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	reads := 0
	rpc := globalStateRPC(t, programID, 1<<2, &reads)
	executor := NewExecutor(slog.Default(), rpc, &signer, programID)

	args := UserCreateArgs{
		UserType:       UserTypeIBRL,
		CyoaType:       CyoaTypeGREOverDIA,
		ClientIP:       [4]byte{10, 11, 12, 13},
		TunnelEndpoint: [4]byte{192, 168, 1, 2},
		DzPrefixCount:  1,
		DevicePubkey:   solana.NewWallet().PublicKey(),
	}

	sig, userPDA, err := executor.CreateUser(context.Background(), args)
	require.ErrorIs(t, err, ErrIPOwnershipProofUnsupported)

	// Nothing was submitted: the point is to fail before spending a transaction.
	assert.Empty(t, rpc.sentTransactions)
	assert.True(t, sig.IsZero())

	// The PDA is still returned so a caller can correlate the refusal.
	expectedPDA, _, derr := GetUserPDA(programID, args.ClientIP, args.UserType)
	require.NoError(t, derr)
	assert.Equal(t, expectedPDA, userPDA)

	// The message has to tell an operator what to do about it.
	assert.Contains(t, err.Error(), "require-ip-ownership-proof")

	// A second attempt reuses the cached flags rather than re-reading.
	_, _, err = executor.CreateUser(context.Background(), args)
	require.ErrorIs(t, err, ErrIPOwnershipProofUnsupported)
	assert.Equal(t, 1, reads, "feature flags must be read once and cached")
}

func TestCreateUserProceedsWhenProofNotRequired(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()

	reads := 0
	rpc := globalStateRPC(t, programID, 0, &reads)
	executor := NewExecutor(slog.Default(), rpc, &signer, programID)

	args := UserCreateArgs{
		UserType:       UserTypeIBRL,
		CyoaType:       CyoaTypeGREOverDIA,
		ClientIP:       [4]byte{10, 11, 12, 14},
		TunnelEndpoint: [4]byte{192, 168, 1, 2},
		DzPrefixCount:  1,
		DevicePubkey:   solana.NewWallet().PublicKey(),
	}

	// The user PDA never becomes visible against this mock, so the call fails at
	// the visibility wait — after the transaction was submitted, which is what
	// this test cares about.
	_, _, err := executor.CreateUser(context.Background(), args)
	assert.NotErrorIs(t, err, ErrIPOwnershipProofUnsupported)
	assert.Len(t, rpc.sentTransactions, 1, "the flag is clear, so the create must be submitted")
}
