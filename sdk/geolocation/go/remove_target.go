package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/near/borsh-go"
)

type RemoveTargetInstructionConfig struct {
	Code                      string
	ProbePK                   solana.PublicKey
	TargetType                GeoLocationTargetType
	IPAddress                 [4]uint8
	TargetPK                  solana.PublicKey
	ServiceabilityGlobalState solana.PublicKey
}

func (c *RemoveTargetInstructionConfig) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("code is required")
	}
	if len(c.Code) > MaxCodeLength {
		return fmt.Errorf("code length %d exceeds max %d", len(c.Code), MaxCodeLength)
	}
	if c.ProbePK.IsZero() {
		return fmt.Errorf("probe public key is required")
	}
	if c.ServiceabilityGlobalState.IsZero() {
		return fmt.Errorf("serviceability global state public key is required")
	}
	if c.TargetType > GeoLocationTargetTypeOutboundIcmp {
		return fmt.Errorf("unknown target type: %d", c.TargetType)
	}
	// Inbound targets are matched onchain by target_pk alone, so a zero target_pk
	// would always fail with TargetNotFound. Match the add-target precondition
	// and reject it up front.
	if c.TargetType == GeoLocationTargetTypeInbound && c.TargetPK.IsZero() {
		return fmt.Errorf("target public key is required for inbound target type")
	}
	return nil
}

// BuildRemoveTargetInstructions returns the instruction list to remove a target: a
// SetComputeUnitLimit prefix sized for MaxTargets followed by the RemoveTarget call.
// The onchain handler does an O(n) scan to find the target by fields; same budget
// rationale as BuildAddTargetInstructions. Pass the returned slice to
// executor.ExecuteTransactions.
func BuildRemoveTargetInstructions(
	programID solana.PublicKey,
	signerPK solana.PublicKey,
	config RemoveTargetInstructionConfig,
) ([]solana.Instruction, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	// Serialize the instruction data.
	data, err := borsh.Serialize(struct {
		Discriminator uint8
		TargetType    uint8
		IPAddress     [4]uint8
		TargetPK      [32]byte
	}{
		Discriminator: uint8(RemoveTargetInstructionIndex),
		TargetType:    uint8(config.TargetType),
		IPAddress:     config.IPAddress,
		TargetPK:      config.TargetPK,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize args: %w", err)
	}

	// Derive PDAs.
	userPDA, _, err := DeriveGeolocationUserPDA(programID, config.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to derive user PDA: %w", err)
	}
	configPDA, _, err := DeriveProgramConfigPDA(programID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive config PDA: %w", err)
	}

	// Build accounts.
	accounts := []*solana.AccountMeta{
		{PublicKey: userPDA, IsSigner: false, IsWritable: true},
		{PublicKey: config.ProbePK, IsSigner: false, IsWritable: true},
		{PublicKey: configPDA, IsSigner: false, IsWritable: false},
		{PublicKey: config.ServiceabilityGlobalState, IsSigner: false, IsWritable: false},
		{PublicKey: signerPK, IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
	}

	mainIx := &solana.GenericInstruction{
		ProgID:        programID,
		AccountValues: accounts,
		DataBytes:     data,
	}

	budgetIx := computebudget.NewSetComputeUnitLimitInstruction(TargetMutationComputeUnitLimit).Build()

	return []solana.Instruction{budgetIx, mainIx}, nil
}
