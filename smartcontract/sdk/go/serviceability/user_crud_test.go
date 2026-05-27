package serviceability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadArgsFixture loads a `.bin` payload from sdk/serviceability/testdata/fixtures/
// for the cross-language wire-format check.
func loadArgsFixture(t *testing.T, name string) []byte {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "sdk", "serviceability", "testdata", "fixtures")
	bin, err := os.ReadFile(filepath.Join(dir, name+".bin"))
	require.NoErrorf(t, err, "reading %s.bin", name)
	return bin
}

func TestBuildCreateUserInstruction(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, _ := newTestExecutor(t, rpc)

	args := UserCreateArgs{
		UserType:       UserTypeIBRL,
		CyoaType:       CyoaTypeGREOverDIA,
		ClientIP:       [4]byte{10, 11, 12, 13},
		TunnelEndpoint: [4]byte{192, 168, 1, 2},
		DzPrefixCount:  2,
		DevicePubkey:   solana.NewWallet().PublicKey(),
	}

	instr, userPDA, err := executor.buildCreateUserInstruction(args)
	require.NoError(t, err)

	// Variant byte + 11-byte borsh body matching Rust UserCreateArgs.
	data, err := instr.Data()
	require.NoError(t, err)
	require.Len(t, data, 12, "opcode (1) + borsh UserCreateArgs (11) = 12 bytes")
	assert.Equal(t, byte(instructionCreateUser), data[0])
	assert.Equal(t, loadArgsFixture(t, "user_create_args"), data[1:],
		"borsh body must match Rust-generated user_create_args.bin")

	// User PDA derivation is deterministic from (program_id, client_ip, user_type).
	expectedPDA, _, err := GetUserPDA(executor.programID, args.ClientIP, args.UserType)
	require.NoError(t, err)
	assert.Equal(t, expectedPDA, userPDA)

	// Account count = 7 fixed + DzPrefixCount + payer + system (no tenant).
	accs := instr.Accounts()
	require.Len(t, accs, 7+int(args.DzPrefixCount)+2)
	assert.Equal(t, userPDA, accs[0].PublicKey)
	assert.True(t, accs[0].IsWritable)
	assert.False(t, accs[0].IsSigner)
	assert.Equal(t, args.DevicePubkey, accs[1].PublicKey)
	// Last two slots: signer + system program.
	assert.Equal(t, executor.signer.PublicKey(), accs[len(accs)-2].PublicKey)
	assert.True(t, accs[len(accs)-2].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accs[len(accs)-1].PublicKey)
}

func TestBuildCreateUserInstruction_WithTenant(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, _ := newTestExecutor(t, rpc)
	tenant := solana.NewWallet().PublicKey()

	args := UserCreateArgs{
		UserType:       UserTypeIBRLWithAllocatedIP,
		CyoaType:       CyoaTypeGREOverFabric,
		ClientIP:       [4]byte{198, 51, 100, 7},
		TunnelEndpoint: [4]byte{0, 0, 0, 0},
		DzPrefixCount:  1,
		DevicePubkey:   solana.NewWallet().PublicKey(),
		TenantPubkey:   tenant,
	}
	instr, _, err := executor.buildCreateUserInstruction(args)
	require.NoError(t, err)

	accs := instr.Accounts()
	// Tenant slot sits between dz_prefix_block(s) and the payer/system tail.
	tenantSlot := accs[len(accs)-3]
	assert.Equal(t, tenant, tenantSlot.PublicKey)
	assert.True(t, tenantSlot.IsWritable)
}

func TestBuildCreateUserInstruction_RejectsZeroDzPrefix(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, _ := newTestExecutor(t, rpc)
	_, _, err := executor.CreateUser(context.Background(), UserCreateArgs{
		UserType:      UserTypeIBRL,
		CyoaType:      CyoaTypeGREOverDIA,
		DzPrefixCount: 0,
		DevicePubkey:  solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DzPrefixCount must be > 0")
}

func TestBuildDeleteUserInstruction(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, _ := newTestExecutor(t, rpc)

	userPubkey := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()
	owner := solana.NewWallet().PublicKey()
	user := User{
		AccountType:  UserType,
		Owner:        owner,
		UserType:     UserTypeIBRL,
		DevicePubKey: device,
		ClientIp:     [4]byte{10, 0, 0, 5},
	}

	// Use the fixture's (3, 1) values to exercise the borsh layout end-to-end
	// against Rust output; production DeleteUser hard-codes (1, 1) — see the
	// constant in DeleteUser itself.
	instr, err := executor.buildDeleteUserInstruction(userPubkey, user, 3, 1)
	require.NoError(t, err)

	data, err := instr.Data()
	require.NoError(t, err)
	require.Len(t, data, 3, "opcode (1) + borsh UserDeleteArgs (2) = 3 bytes")
	assert.Equal(t, byte(instructionDeleteUser), data[0])
	assert.Equal(t, loadArgsFixture(t, "user_delete_args"), data[1:],
		"borsh body must match Rust-generated user_delete_args.bin")

	accs := instr.Accounts()
	// 7 fixed + 3 dz_prefix + owner + payer + system = 13 accounts (no tenant).
	require.Len(t, accs, 13)
	assert.Equal(t, userPubkey, accs[0].PublicKey)
	assert.Equal(t, device, accs[3].PublicKey)
	ownerSlot := accs[len(accs)-3]
	assert.Equal(t, owner, ownerSlot.PublicKey)
	assert.True(t, ownerSlot.IsWritable)
	assert.Equal(t, executor.signer.PublicKey(), accs[len(accs)-2].PublicKey)
	assert.True(t, accs[len(accs)-2].IsSigner)
	assert.Equal(t, solana.SystemProgramID, accs[len(accs)-1].PublicKey)
}

func TestBuildDeleteUserInstruction_WithTenant(t *testing.T) {
	t.Parallel()

	rpc := &mockRPCClient{}
	executor, _ := newTestExecutor(t, rpc)

	tenant := solana.NewWallet().PublicKey()
	user := User{
		AccountType:  UserType,
		Owner:        solana.NewWallet().PublicKey(),
		TenantPubKey: tenant,
		DevicePubKey: solana.NewWallet().PublicKey(),
		UserType:     UserTypeIBRL,
		ClientIp:     [4]byte{10, 0, 0, 5},
	}

	instr, err := executor.buildDeleteUserInstruction(solana.NewWallet().PublicKey(), user, 1, 1)
	require.NoError(t, err)

	accs := instr.Accounts()
	// Tenant sits before the owner/payer/system tail (3 trailing slots).
	tenantSlot := accs[len(accs)-4]
	assert.Equal(t, tenant, tenantSlot.PublicKey)
	assert.True(t, tenantSlot.IsWritable)
}

func TestCreateUserWaitsForAccountVisible(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()
	args := UserCreateArgs{
		UserType:      UserTypeIBRL,
		CyoaType:      CyoaTypeGREOverDIA,
		ClientIP:      [4]byte{10, 0, 0, 1},
		DzPrefixCount: 1,
		DevicePubkey:  device,
	}
	expectedPDA, _, err := GetUserPDA(programID, args.ClientIP, args.UserType)
	require.NoError(t, err)

	// First call (permission probe) returns nil; the user-PDA probe then returns
	// a non-nil Value so the visibility wait completes immediately.
	var lookups atomic.Int32
	rpc := &mockRPCClient{
		getAccountInfoFunc: func(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			n := lookups.Add(1)
			if account.Equals(expectedPDA) && n >= 2 {
				return &solanarpc.GetAccountInfoResult{
					Value: &solanarpc.Account{Owner: programID},
				}, nil
			}
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}
	executor := NewExecutor(slog.Default(), rpc, &signer, programID, WithWaitForVisibleTimeout(500*time.Millisecond))

	sig, userPDA, err := executor.CreateUser(context.Background(), args)
	require.NoError(t, err)
	assert.NotEqual(t, solana.Signature{}, sig)
	assert.Equal(t, expectedPDA, userPDA)
	require.NotEmpty(t, rpc.sentTransactions)
}

func TestCreateUserReportsVisibilityTimeout(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	rpc := &mockRPCClient{} // default: GetAccountInfo always returns nil
	executor := NewExecutor(slog.Default(), rpc, &signer, programID, WithWaitForVisibleTimeout(50*time.Millisecond))

	sig, userPDA, err := executor.CreateUser(context.Background(), UserCreateArgs{
		UserType:      UserTypeIBRL,
		CyoaType:      CyoaTypeGREOverDIA,
		ClientIP:      [4]byte{10, 0, 0, 1},
		DzPrefixCount: 1,
		DevicePubkey:  solana.NewWallet().PublicKey(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post-confirm visibility timeout")
	// Signature and PDA are still returned so callers can correlate.
	assert.NotEqual(t, solana.Signature{}, sig)
	assert.NotEqual(t, solana.PublicKey{}, userPDA)
}

func TestDeleteUserWaitsForAccountGone(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	userPubkey := solana.NewWallet().PublicKey()

	// Construct a borsh-serialized minimal User account body via DeserializeUser's
	// inverse: we just write the fields by hand.
	owner := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()
	userBytes := makeMinimalUserBytes(owner, device, [4]byte{10, 0, 0, 5})

	// Sequence: GetAccountInfo returns user bytes once (initial DeleteUser read), nil
	// thereafter (visibility wait sees account gone). Permission probe returns nil.
	var lookups atomic.Int32
	rpc := &mockRPCClient{
		getAccountInfoFunc: func(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			n := lookups.Add(1)
			if account.Equals(userPubkey) && n == 1 {
				return &solanarpc.GetAccountInfoResult{
					Value: &solanarpc.Account{
						Owner: programID,
						Data:  solanarpc.DataBytesOrJSONFromBytes(userBytes),
					},
				}, nil
			}
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}
	executor := NewExecutor(slog.Default(), rpc, &signer, programID, WithWaitForVisibleTimeout(500*time.Millisecond))

	sig, err := executor.DeleteUser(context.Background(), userPubkey)
	require.NoError(t, err)
	assert.NotEqual(t, solana.Signature{}, sig)
	require.NotEmpty(t, rpc.sentTransactions)

	// Verify the submitted transaction references the device pulled from the User.
	tx := rpc.sentTransactions[0]
	keys := tx.Message.AccountKeys
	foundDevice := false
	for _, k := range keys {
		if k.Equals(device) {
			foundDevice = true
			break
		}
	}
	assert.True(t, foundDevice, "device referenced by the user account must appear in the DeleteUser tx")
}

func TestDeleteUserNotFound(t *testing.T) {
	t.Parallel()

	signer := solana.NewWallet().PrivateKey
	programID := solana.NewWallet().PublicKey()
	rpc := &mockRPCClient{
		getAccountInfoFunc: func(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
			return &solanarpc.GetAccountInfoResult{Value: nil}, nil
		},
	}
	executor := NewExecutor(slog.Default(), rpc, &signer, programID)

	_, err := executor.DeleteUser(context.Background(), solana.NewWallet().PublicKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWaitForAccountVisible_TimeoutVsCancel(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when account appears", func(t *testing.T) {
		var n atomic.Int32
		rpc := &mockRPCClient{
			getAccountInfoFunc: func(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
				if n.Add(1) >= 2 {
					return &solanarpc.GetAccountInfoResult{Value: &solanarpc.Account{}}, nil
				}
				return &solanarpc.GetAccountInfoResult{Value: nil}, nil
			},
		}
		executor, _ := newTestExecutor(t, rpc)
		require.NoError(t, executor.waitForAccountVisible(context.Background(), solana.NewWallet().PublicKey(), time.Second))
	})

	t.Run("returns error past deadline", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)
		err := executor.waitForAccountVisible(context.Background(), solana.NewWallet().PublicKey(), 50*time.Millisecond)
		require.Error(t, err)
	})

	t.Run("returns context error on cancel", func(t *testing.T) {
		rpc := &mockRPCClient{}
		executor, _ := newTestExecutor(t, rpc)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := executor.waitForAccountVisible(ctx, solana.NewWallet().PublicKey(), time.Second)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})
}

// makeMinimalUserBytes hand-encodes a User account body matching DeserializeUser's
// field order. Most fields are zero — only AccountType, Owner, DevicePubKey, and
// ClientIp are populated, which is enough for buildDeleteUserInstruction.
func makeMinimalUserBytes(owner, device solana.PublicKey, clientIP [4]byte) []byte {
	b := make([]byte, 0, 256)
	b = append(b, byte(UserType))           // AccountType
	b = append(b, owner[:]...)              // Owner: 32 bytes
	b = append(b, make([]byte, 16)...)      // Index: u128 = 16 bytes
	b = append(b, 0)                        // BumpSeed
	b = append(b, byte(UserTypeIBRL))       // UserType
	b = append(b, make([]byte, 32)...)      // TenantPubKey (zero)
	b = append(b, device[:]...)             // DevicePubKey: 32 bytes
	b = append(b, byte(CyoaTypeGREOverDIA)) // CyoaType
	b = append(b, clientIP[:]...)           // ClientIp: 4 bytes
	b = append(b, make([]byte, 4)...)       // DzIp: 4 bytes
	b = append(b, 0, 0)                     // TunnelId: u16
	b = append(b, make([]byte, 5)...)       // TunnelNet: 5 bytes
	b = append(b, byte(UserStatusActivated))
	b = append(b, 0, 0, 0, 0)          // Publishers: u32 len = 0
	b = append(b, 0, 0, 0, 0)          // Subscribers: u32 len = 0
	b = append(b, make([]byte, 32)...) // ValidatorPubKey
	b = append(b, make([]byte, 4)...)  // TunnelEndpoint
	b = append(b, 0)                   // TunnelFlags
	b = append(b, 0)                   // BgpStatus
	b = append(b, make([]byte, 8)...)  // LastBgpUpAt
	b = append(b, make([]byte, 8)...)  // LastBgpReportedAt
	b = append(b, make([]byte, 8)...)  // BgpRttNs
	return b
}
