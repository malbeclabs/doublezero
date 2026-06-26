package serviceability

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const (
	instructionCreateUser       = 36
	instructionDeleteUser       = 42
	instructionSetDeviceHealth  = 83
	instructionSetLinkHealth    = 84
	instructionSetUserBGPStatus = 106
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

	permissionOnce sync.Once
	permissionPDA  *solana.PublicKey // nil if no Permission account exists for this signer
}

type ExecutorRPCClient interface {
	GetLatestBlockhash(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error)
	SendTransactionWithOpts(ctx context.Context, transaction *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error)
	GetSignatureStatuses(ctx context.Context, searchTransactionHistory bool, transactionSignatures ...solana.Signature) (*solanarpc.GetSignatureStatusesResult, error)
	GetTransaction(ctx context.Context, txSig solana.Signature, opts *solanarpc.GetTransactionOpts) (*solanarpc.GetTransactionResult, error)
	GetAccountInfo(ctx context.Context, account solana.PublicKey) (*solanarpc.GetAccountInfoResult, error)
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
			"error", formatRPCError(err),
		)

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
			"error", formatRPCError(err),
		)

		remaining = append(remaining[:failingIdx], remaining[failingIdx+1:]...)
	}

	return lastSig, ErrAllUpdatesFailed
}

// UserCreateArgs bundles every input the Go executor needs to submit a CreateUser
// instruction (variant 36). The first five fields are borsh-encoded into the
// instruction payload exactly matching Rust's `UserCreateArgs`; the trailing
// DevicePubkey/TenantPubkey are only used to derive AccountMeta entries.
type UserCreateArgs struct {
	UserType       UserUserType
	CyoaType       CyoaType
	ClientIP       [4]byte
	TunnelEndpoint [4]byte
	DzPrefixCount  uint8

	// DevicePubkey identifies the device the user attaches to; required.
	DevicePubkey solana.PublicKey
	// TenantPubkey is the optional tenant association; pass the zero pubkey to omit.
	TenantPubkey solana.PublicKey
}

// CreateUser submits a CreateUser instruction (variant 36) and waits for the user
// PDA to become visible on-chain. Returns the signature and derived user PDA so the
// caller can correlate (e.g., record t_activate against this user).
func (e *Executor) CreateUser(ctx context.Context, args UserCreateArgs) (solana.Signature, solana.PublicKey, error) {
	if e.signer == nil {
		return solana.Signature{}, solana.PublicKey{}, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, solana.PublicKey{}, ErrNoProgramID
	}
	if args.DzPrefixCount == 0 {
		return solana.Signature{}, solana.PublicKey{}, errors.New("UserCreateArgs.DzPrefixCount must be > 0")
	}
	if args.DevicePubkey.IsZero() {
		return solana.Signature{}, solana.PublicKey{}, errors.New("UserCreateArgs.DevicePubkey is required")
	}

	instr, userPDA, err := e.buildCreateUserInstruction(args)
	if err != nil {
		return solana.Signature{}, solana.PublicKey{}, fmt.Errorf("build CreateUser instruction: %w", err)
	}

	sig, _, err := e.executeTransaction(ctx, []solana.Instruction{instr})
	if err != nil {
		return sig, userPDA, err
	}

	if err := e.waitForAccountVisible(ctx, userPDA, e.waitForVisibleTimeout); err != nil {
		return sig, userPDA, fmt.Errorf("post-confirm visibility timeout for user PDA: %w", err)
	}
	return sig, userPDA, nil
}

// DeleteUser submits a DeleteUser instruction (variant 42) and waits for the user
// PDA to disappear from chain. The function reads the user account first so it
// can derive the device-dependent PDAs and the multicast-publisher flag.
// NOTE: this function does not unsubscribe multicast groups first. That should be
// handled externally./
func (e *Executor) DeleteUser(ctx context.Context, userPubkey solana.PublicKey) (solana.Signature, error) {
	if e.signer == nil {
		return solana.Signature{}, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, ErrNoProgramID
	}

	info, err := e.rpc.GetAccountInfo(ctx, userPubkey)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("fetch user account %s: %w", userPubkey, err)
	}
	if info == nil || info.Value == nil {
		return solana.Signature{}, fmt.Errorf("user account %s not found", userPubkey)
	}
	rawData := info.Value.Data.GetBinary()
	if len(rawData) == 0 {
		return solana.Signature{}, fmt.Errorf("user account %s has empty data", userPubkey)
	}
	var user User
	DeserializeUser(NewByteReader(rawData), &user)
	if user.AccountType != UserType {
		return solana.Signature{}, fmt.Errorf("account %s is not a User (type=%d)", userPubkey, user.AccountType)
	}
	user.PubKey = userPubkey

	// The Rust SDK currently passes dz_prefix_count=1 / multicast_publisher_count=1
	// because all users are created with exactly one DzPrefixBlock. Stress-orchestrator
	// users likewise use DzPrefixCount=1, so 1 is the correct value here. Diverging
	// requires fetching the Device record — out of scope for the SDK primitive.
	const dzPrefixCount uint8 = 1
	const multicastPublisherCount uint8 = 1

	instr, err := e.buildDeleteUserInstruction(userPubkey, user, dzPrefixCount, multicastPublisherCount)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("build DeleteUser instruction: %w", err)
	}

	sig, _, err := e.executeTransaction(ctx, []solana.Instruction{instr})
	if err != nil {
		return sig, err
	}

	if err := e.waitForAccountGone(ctx, userPubkey, e.waitForVisibleTimeout); err != nil {
		return sig, fmt.Errorf("post-confirm visibility timeout waiting for user PDA closure: %w", err)
	}
	return sig, nil
}

// buildCreateUserInstruction packs the variant-36 payload and assembles the account
// list in the order the on-chain processor expects:
//
//	[user_pda, device, accesspass, globalstate,
//	 user_tunnel_block, multicast_publisher_block, device_tunnel_ids,
//	 dz_prefix_block[0..N], optional_tenant, payer, system]
func (e *Executor) buildCreateUserInstruction(args UserCreateArgs) (solana.Instruction, solana.PublicKey, error) {
	data := make([]byte, 12)
	data[0] = instructionCreateUser
	data[1] = byte(args.UserType)
	data[2] = byte(args.CyoaType)
	copy(data[3:7], args.ClientIP[:])
	copy(data[7:11], args.TunnelEndpoint[:])
	data[11] = args.DzPrefixCount

	userPDA, _, err := GetUserPDA(e.programID, args.ClientIP, args.UserType)
	if err != nil {
		return nil, solana.PublicKey{}, fmt.Errorf("derive user PDA: %w", err)
	}
	accessPassPDA, _, err := GetAccessPassPDA(e.programID, args.ClientIP, e.signer.PublicKey())
	if err != nil {
		return nil, userPDA, fmt.Errorf("derive accesspass PDA: %w", err)
	}
	globalStatePDA, _, err := GetGlobalStatePDA(e.programID)
	if err != nil {
		return nil, userPDA, fmt.Errorf("derive globalstate PDA: %w", err)
	}
	userTunnelBlockPDA, _, err := GetUserTunnelBlockPDA(e.programID)
	if err != nil {
		return nil, userPDA, fmt.Errorf("derive user tunnel block PDA: %w", err)
	}
	mcPublisherBlockPDA, _, err := GetMulticastPublisherBlockPDA(e.programID)
	if err != nil {
		return nil, userPDA, fmt.Errorf("derive multicast publisher block PDA: %w", err)
	}
	tunnelIdsPDA, _, err := GetTunnelIdsPDA(e.programID, args.DevicePubkey, 0)
	if err != nil {
		return nil, userPDA, fmt.Errorf("derive device tunnel ids PDA: %w", err)
	}

	accounts := solana.AccountMetaSlice{
		solana.Meta(userPDA).WRITE(),
		solana.Meta(args.DevicePubkey).WRITE(),
		solana.Meta(accessPassPDA).WRITE(),
		solana.Meta(globalStatePDA).WRITE(),
		solana.Meta(userTunnelBlockPDA).WRITE(),
		solana.Meta(mcPublisherBlockPDA).WRITE(),
		solana.Meta(tunnelIdsPDA).WRITE(),
	}
	for i := uint64(0); i < uint64(args.DzPrefixCount); i++ {
		dzPrefixPDA, _, err := GetDzPrefixBlockPDA(e.programID, args.DevicePubkey, i)
		if err != nil {
			return nil, userPDA, fmt.Errorf("derive dz_prefix_block[%d] PDA: %w", i, err)
		}
		accounts = append(accounts, solana.Meta(dzPrefixPDA).WRITE())
	}
	if !args.TenantPubkey.IsZero() {
		accounts = append(accounts, solana.Meta(args.TenantPubkey).WRITE())
	}
	accounts = append(accounts,
		solana.Meta(e.signer.PublicKey()).SIGNER().WRITE(),
		solana.Meta(solana.SystemProgramID),
	)

	return &genericInstruction{
		programID:            e.programID,
		accounts:             accounts,
		data:                 data,
		skipPermissionInject: true,
	}, userPDA, nil
}

// buildDeleteUserInstruction packs the variant-42 payload and assembles the account
// list in the order the on-chain processor expects:
//
//	[user, accesspass, globalstate, device,
//	 user_tunnel_block, multicast_publisher_block, device_tunnel_ids,
//	 dz_prefix_block[0..N], optional_tenant, owner, payer, system]
//
// `multicastPublisherCount` mirrors the Rust SDK's behavior: the on-chain processor
// consumes the MulticastPublisherBlock slot unconditionally for the variant-42
// layout, so DeleteUser's caller passes 1 even when the user was not created as a
// publisher. Exposed as a parameter so the byte-encoding can be tested independently.
func (e *Executor) buildDeleteUserInstruction(userPubkey solana.PublicKey, user User, dzPrefixCount, multicastPublisherCount uint8) (solana.Instruction, error) {
	data := []byte{instructionDeleteUser, dzPrefixCount, multicastPublisherCount}

	accessPassPDA, _, err := GetAccessPassPDA(e.programID, user.ClientIp, user.Owner)
	if err != nil {
		return nil, fmt.Errorf("derive accesspass PDA: %w", err)
	}
	globalStatePDA, _, err := GetGlobalStatePDA(e.programID)
	if err != nil {
		return nil, fmt.Errorf("derive globalstate PDA: %w", err)
	}
	devicePubkey := solana.PublicKeyFromBytes(user.DevicePubKey[:])
	userTunnelBlockPDA, _, err := GetUserTunnelBlockPDA(e.programID)
	if err != nil {
		return nil, fmt.Errorf("derive user tunnel block PDA: %w", err)
	}
	mcPublisherBlockPDA, _, err := GetMulticastPublisherBlockPDA(e.programID)
	if err != nil {
		return nil, fmt.Errorf("derive multicast publisher block PDA: %w", err)
	}
	tunnelIdsPDA, _, err := GetTunnelIdsPDA(e.programID, devicePubkey, 0)
	if err != nil {
		return nil, fmt.Errorf("derive device tunnel ids PDA: %w", err)
	}

	accounts := solana.AccountMetaSlice{
		solana.Meta(userPubkey).WRITE(),
		solana.Meta(accessPassPDA).WRITE(),
		solana.Meta(globalStatePDA).WRITE(),
		solana.Meta(devicePubkey).WRITE(),
		solana.Meta(userTunnelBlockPDA).WRITE(),
		solana.Meta(mcPublisherBlockPDA).WRITE(),
		solana.Meta(tunnelIdsPDA).WRITE(),
	}
	for i := uint64(0); i < uint64(dzPrefixCount); i++ {
		dzPrefixPDA, _, err := GetDzPrefixBlockPDA(e.programID, devicePubkey, i)
		if err != nil {
			return nil, fmt.Errorf("derive dz_prefix_block[%d] PDA: %w", i, err)
		}
		accounts = append(accounts, solana.Meta(dzPrefixPDA).WRITE())
	}
	var zeroPK [32]uint8
	if user.TenantPubKey != zeroPK {
		accounts = append(accounts, solana.Meta(solana.PublicKeyFromBytes(user.TenantPubKey[:])).WRITE())
	}
	accounts = append(accounts,
		solana.Meta(solana.PublicKeyFromBytes(user.Owner[:])).WRITE(),
		solana.Meta(e.signer.PublicKey()).SIGNER().WRITE(),
		solana.Meta(solana.SystemProgramID),
	)

	return &genericInstruction{
		programID:            e.programID,
		accounts:             accounts,
		data:                 data,
		skipPermissionInject: true,
	}, nil
}

// UserBGPStatusUpdate holds the parameters for a single SetUserBGPStatus submission.
type UserBGPStatusUpdate struct {
	UserPubkey   solana.PublicKey
	DevicePubkey solana.PublicKey
	Status       BGPStatus
	// BgpRttNs is the smoothed BGP TCP RTT in nanoseconds, sourced from the
	// kernel via INET_DIAG on the device. 0 means no sample. Old programs that
	// predate this field ignore the trailing bytes via BorshDeserializeIncremental.
	BgpRttNs uint64
}

// SetUserBGPStatus submits a SetUserBGPStatus instruction for a single user.
// The executor's signer must be the device's metrics_publisher_pk.
func (e *Executor) SetUserBGPStatus(ctx context.Context, u UserBGPStatusUpdate) (solana.Signature, error) {
	instr := e.buildSetUserBGPStatusInstruction(u.UserPubkey, u.DevicePubkey, u.Status, u.BgpRttNs)
	sig, _, err := e.executeTransaction(ctx, []solana.Instruction{instr})
	return sig, err
}

func (e *Executor) buildSetUserBGPStatusInstruction(userPubkey, devicePubkey solana.PublicKey, status BGPStatus, bgpRttNs uint64) solana.Instruction {
	data := make([]byte, 10)
	data[0] = instructionSetUserBGPStatus
	data[1] = byte(status)
	binary.LittleEndian.PutUint64(data[2:], bgpRttNs)
	return &genericInstruction{
		programID: e.programID,
		accounts: solana.AccountMetaSlice{
			solana.Meta(userPubkey).WRITE(),
			solana.Meta(devicePubkey),
			solana.Meta(e.signer.PublicKey()).SIGNER().WRITE(),
			solana.Meta(solana.SystemProgramID),
		},
		data: data,
	}
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
	// skipPermissionInject suppresses the executor's auto-appending of the Permission PDA.
	// CreateUser/DeleteUser opt out because the on-chain processor uses accounts.len()
	// to detect the optional tenant account; appending a trailing Permission shifts that
	// count and would mis-classify accounts.
	skipPermissionInject bool
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

// resolvePermissionPDA checks on-chain (once) whether a Permission account exists for this
// executor's signer and caches the PDA address. Subsequent calls are no-ops.
func (e *Executor) resolvePermissionPDA(ctx context.Context) {
	e.permissionOnce.Do(func() {
		if e.signer == nil {
			return
		}
		pda, _, err := GetPermissionPDA(e.programID, e.signer.PublicKey())
		if err != nil {
			e.log.Warn("failed to derive Permission PDA", "error", err)
			return
		}
		info, err := e.rpc.GetAccountInfo(ctx, pda)
		if err != nil || info == nil || info.Value == nil {
			return
		}
		e.permissionPDA = &pda
		e.log.Debug("Permission account found, will include in transactions", "pda", pda)
	})
}

func (e *Executor) executeTransaction(ctx context.Context, instructions []solana.Instruction) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if e.signer == nil {
		return solana.Signature{}, nil, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, nil, ErrNoProgramID
	}

	// Resolve and inject the Permission PDA into every instruction when the account exists.
	e.resolvePermissionPDA(ctx)
	if e.permissionPDA != nil {
		for _, instr := range instructions {
			if gi, ok := instr.(*genericInstruction); ok && !gi.skipPermissionInject {
				gi.accounts = append(gi.accounts, solana.Meta(*e.permissionPDA))
			}
		}
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

// waitForAccountVisible polls GetAccountInfo until the given account is observable
// on-chain, or the deadline expires. Used post-CreateUser to give the caller a
// timestamp anchored to when the user PDA actually appears.
func (e *Executor) waitForAccountVisible(ctx context.Context, pubkey solana.PublicKey, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := e.rpc.GetAccountInfo(ctx, pubkey)
		if err == nil && info != nil && info.Value != nil {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("account %s not visible: %w", pubkey, err)
			}
			return fmt.Errorf("account %s not visible before deadline", pubkey)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// waitForAccountGone polls GetAccountInfo until the given account no longer exists,
// or the deadline expires. Used post-DeleteUser to detect closure.
//
// The gagliardetto RPC client surfaces a missing account as (nil, ErrNotFound)
// rather than (&Result{Value: nil}, nil). Both shapes mean the same thing —
// closure succeeded — so both are treated as the success signal. Any other
// error is transient: retry until the deadline.
func (e *Executor) waitForAccountGone(ctx context.Context, pubkey solana.PublicKey, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := e.rpc.GetAccountInfo(ctx, pubkey)
		if errors.Is(err, solanarpc.ErrNotFound) {
			return nil
		}
		if err == nil && (info == nil || info.Value == nil) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("account %s still present: %w", pubkey, err)
			}
			return fmt.Errorf("account %s still present before deadline", pubkey)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
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

	// A finalized status carries a non-nil Err when the transaction executed
	// but the program returned an error (e.g. a doublezero-serviceability
	// validation rejection). Without this check the caller assumes success,
	// the post-confirm visibility poll hits a missing account, and the error
	// surfaces as a misleading "account not visible" timeout instead of the
	// actual program error.
	if finalStatus.Err != nil {
		return nil, fmt.Errorf("transaction finalized with error: %v", finalStatus.Err)
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
