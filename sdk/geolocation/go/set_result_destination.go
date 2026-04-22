package geolocation

import (
	"context"
	"errors"
	"fmt"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/near/borsh-go"
)

type SetResultDestinationInstructionConfig struct {
	Code        string
	Destination string
	ProbePKs    []solana.PublicKey
}

func (c *SetResultDestinationInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	return nil
}

func BuildSetResultDestinationInstruction(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config SetResultDestinationInstructionConfig,
) (solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator uint8
		Destination   string
	}{
		Discriminator: uint8(SetResultDestinationInstructionIndex),
		Destination:   config.Destination,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive the user PDA.
	userPDA, _, err := DeriveGeolocationUserPDA(programID, config.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user PDA: %w", err)
	}

	// Build accounts: user_pda, then each probe PK, then signer, then system program.
	accounts := make([]*solana.AccountMeta, 0, 3+len(config.ProbePKs))
	accounts = append(accounts, &solana.AccountMeta{PublicKey: userPDA, IsSigner: false, IsWritable: true})
	for _, probePK := range config.ProbePKs {
		accounts = append(accounts, &solana.AccountMeta{PublicKey: probePK, IsSigner: false, IsWritable: true})
	}
	accounts = append(accounts, &solana.AccountMeta{PublicKey: signerPK, IsSigner: true, IsWritable: true})
	accounts = append(accounts, &solana.AccountMeta{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false})

	return &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}, nil
}

// DeriveUniqueProbePKs returns the unique geoprobe public keys referenced by the
// given targets, preserving first-occurrence order. The onchain program requires
// the SetResultDestination instruction to carry exactly this set of probe accounts.
func DeriveUniqueProbePKs(targets []GeolocationTarget) []solana.PublicKey {
	seen := make(map[solana.PublicKey]struct{}, len(targets))
	probes := make([]solana.PublicKey, 0, len(targets))
	for _, t := range targets {
		if _, ok := seen[t.GeoProbePK]; ok {
			continue
		}
		seen[t.GeoProbePK] = struct{}{}
		probes = append(probes, t.GeoProbePK)
	}
	return probes
}

// SetResultDestination fetches the GeolocationUser account, derives the set of unique
// probe accounts from its targets, and submits a SetResultDestination instruction.
// Pass an empty destination to clear.
func (e *executor) SetResultDestination(ctx context.Context, code, destination string, opts *ExecuteTransactionOptions) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if e.signer == nil {
		return solana.Signature{}, nil, ErrNoPrivateKey
	}
	if e.programID.IsZero() {
		return solana.Signature{}, nil, ErrNoProgramID
	}

	userPDA, _, err := DeriveGeolocationUserPDA(e.programID, code)
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to derive user PDA: %w", err)
	}

	account, err := e.rpc.GetAccountInfo(ctx, userPDA)
	if err != nil {
		if errors.Is(err, solanarpc.ErrNotFound) {
			return solana.Signature{}, nil, ErrAccountNotFound
		}
		return solana.Signature{}, nil, fmt.Errorf("failed to get user account: %w", err)
	}
	if account == nil || account.Value == nil {
		return solana.Signature{}, nil, ErrAccountNotFound
	}
	if account.Value.Owner != e.programID {
		return solana.Signature{}, nil, fmt.Errorf("%w: got %s, want %s", ErrOwnerMismatch, account.Value.Owner, e.programID)
	}

	user, err := DeserializeGeolocationUser(account.Value.Data.GetBinary())
	if err != nil {
		return solana.Signature{}, nil, fmt.Errorf("failed to deserialize user: %w", err)
	}

	ix, err := BuildSetResultDestinationInstruction(e.programID, e.signer.PublicKey(), SetResultDestinationInstructionConfig{
		Code:        code,
		Destination: destination,
		ProbePKs:    DeriveUniqueProbePKs(user.Targets),
	})
	if err != nil {
		return solana.Signature{}, nil, err
	}

	return e.ExecuteTransaction(ctx, ix, opts)
}
