package geolocation_test

import (
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/geolocation"
	"github.com/stretchr/testify/require"
)

// TestSDK_Geolocation_BuildInitProgramConfigInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildInitProgramConfigInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.InitProgramConfigInstructionConfig{
		Payer:                   solana.NewWallet().PublicKey(),
		ServiceabilityProgramID: solana.NewWallet().PublicKey(),
	}

	instr, err := geolocation.BuildInitProgramConfigInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 4, "should have 4 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
}

// TestSDK_Geolocation_BuildInitProgramConfigInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildInitProgramConfigInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.InitProgramConfigInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.InitProgramConfigInstructionConfig{
				Payer:                   solana.PublicKey{},
				ServiceabilityProgramID: solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing serviceability program ID",
			config: geolocation.InitProgramConfigInstructionConfig{
				Payer:                   solana.NewWallet().PublicKey(),
				ServiceabilityProgramID: solana.PublicKey{},
			},
			errMsg: "serviceability program ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildInitProgramConfigInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSDK_Geolocation_BuildUpdateProgramConfigInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildUpdateProgramConfigInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	serviceabilityProgramID := solana.NewWallet().PublicKey()
	config := geolocation.UpdateProgramConfigInstructionConfig{
		Payer:                   solana.NewWallet().PublicKey(),
		ServiceabilityProgramID: &serviceabilityProgramID,
	}

	instr, err := geolocation.BuildUpdateProgramConfigInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 4, "should have 4 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
}

// TestSDK_Geolocation_BuildUpdateProgramConfigInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildUpdateProgramConfigInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	config := geolocation.UpdateProgramConfigInstructionConfig{
		Payer:                   solana.PublicKey{},
		ServiceabilityProgramID: nil,
	}

	_, err := geolocation.BuildUpdateProgramConfigInstruction(programID, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "payer public key is required")
}

// TestSDK_Geolocation_BuildCreateGeoProbeInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildCreateGeoProbeInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.CreateGeoProbeInstructionConfig{
		Payer:                       solana.NewWallet().PublicKey(),
		Code:                        "ams-probe-01",
		ExchangePK:                  solana.NewWallet().PublicKey(),
		ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
		PublicIP:                    [4]uint8{10, 0, 1, 42},
		LocationOffsetPort:          8923,
		LatencyThresholdNs:          1_000_000,
		MetricsPublisherPK:          solana.NewWallet().PublicKey(),
	}

	instr, err := geolocation.BuildCreateGeoProbeInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 6, "should have 6 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
}

// TestSDK_Geolocation_BuildCreateGeoProbeInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildCreateGeoProbeInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.CreateGeoProbeInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.CreateGeoProbeInstructionConfig{
				Payer:                       solana.PublicKey{},
				Code:                        "test-code",
				ExchangePK:                  solana.NewWallet().PublicKey(),
				ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
				MetricsPublisherPK:          solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing code",
			config: geolocation.CreateGeoProbeInstructionConfig{
				Payer:                       solana.NewWallet().PublicKey(),
				Code:                        "",
				ExchangePK:                  solana.NewWallet().PublicKey(),
				ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
				MetricsPublisherPK:          solana.NewWallet().PublicKey(),
			},
			errMsg: "code is required",
		},
		{
			name: "missing exchange public key",
			config: geolocation.CreateGeoProbeInstructionConfig{
				Payer:                       solana.NewWallet().PublicKey(),
				Code:                        "test-code",
				ExchangePK:                  solana.PublicKey{},
				ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
				MetricsPublisherPK:          solana.NewWallet().PublicKey(),
			},
			errMsg: "exchange public key is required",
		},
		{
			name: "missing serviceability global state public key",
			config: geolocation.CreateGeoProbeInstructionConfig{
				Payer:                       solana.NewWallet().PublicKey(),
				Code:                        "test-code",
				ExchangePK:                  solana.NewWallet().PublicKey(),
				ServiceabilityGlobalStatePK: solana.PublicKey{},
				MetricsPublisherPK:          solana.NewWallet().PublicKey(),
			},
			errMsg: "serviceability global state public key is required",
		},
		{
			name: "missing metrics publisher public key",
			config: geolocation.CreateGeoProbeInstructionConfig{
				Payer:                       solana.NewWallet().PublicKey(),
				Code:                        "test-code",
				ExchangePK:                  solana.NewWallet().PublicKey(),
				ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
				MetricsPublisherPK:          solana.PublicKey{},
			},
			errMsg: "metrics publisher public key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildCreateGeoProbeInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSDK_Geolocation_BuildCreateGeoProbeInstruction_CodeTooLong tests code length validation
func TestSDK_Geolocation_BuildCreateGeoProbeInstruction_CodeTooLong(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.CreateGeoProbeInstructionConfig{
		Payer:                       solana.NewWallet().PublicKey(),
		Code:                        strings.Repeat("a", geolocation.MaxCodeLength+1),
		ExchangePK:                  solana.NewWallet().PublicKey(),
		ServiceabilityGlobalStatePK: solana.NewWallet().PublicKey(),
		MetricsPublisherPK:          solana.NewWallet().PublicKey(),
	}

	_, err := geolocation.BuildCreateGeoProbeInstruction(programID, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}

// TestSDK_Geolocation_BuildUpdateGeoProbeInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildUpdateGeoProbeInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	publicIP := [4]uint8{192, 168, 1, 1}
	port := uint16(9000)
	latency := uint64(500_000)
	metricsPublisher := solana.NewWallet().PublicKey()

	config := geolocation.UpdateGeoProbeInstructionConfig{
		Payer:              solana.NewWallet().PublicKey(),
		ProbePK:            solana.NewWallet().PublicKey(),
		PublicIP:           &publicIP,
		LocationOffsetPort: &port,
		LatencyThresholdNs: &latency,
		MetricsPublisherPK: &metricsPublisher,
	}

	instr, err := geolocation.BuildUpdateGeoProbeInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 3, "should have 3 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
}

// TestSDK_Geolocation_BuildUpdateGeoProbeInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildUpdateGeoProbeInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.UpdateGeoProbeInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.UpdateGeoProbeInstructionConfig{
				Payer:   solana.PublicKey{},
				ProbePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing probe public key",
			config: geolocation.UpdateGeoProbeInstructionConfig{
				Payer:   solana.NewWallet().PublicKey(),
				ProbePK: solana.PublicKey{},
			},
			errMsg: "probe public key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildUpdateGeoProbeInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSDK_Geolocation_BuildDeleteGeoProbeInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildDeleteGeoProbeInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.DeleteGeoProbeInstructionConfig{
		Payer:   solana.NewWallet().PublicKey(),
		ProbePK: solana.NewWallet().PublicKey(),
	}

	instr, err := geolocation.BuildDeleteGeoProbeInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 2, "should have 2 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
	// probe is writable (being closed); payer is writable (receives rent refund)
	require.True(t, accounts[0].IsWritable, "probe account must be writable")
	require.True(t, accounts[1].IsWritable, "payer must be writable to receive rent refund")
	require.True(t, accounts[1].IsSigner, "payer must be signer")
}

// TestSDK_Geolocation_BuildDeleteGeoProbeInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildDeleteGeoProbeInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.DeleteGeoProbeInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.DeleteGeoProbeInstructionConfig{
				Payer:   solana.PublicKey{},
				ProbePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing probe public key",
			config: geolocation.DeleteGeoProbeInstructionConfig{
				Payer:   solana.NewWallet().PublicKey(),
				ProbePK: solana.PublicKey{},
			},
			errMsg: "probe public key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildDeleteGeoProbeInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSDK_Geolocation_BuildAddParentDeviceInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildAddParentDeviceInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.AddParentDeviceInstructionConfig{
		Payer:    solana.NewWallet().PublicKey(),
		ProbePK:  solana.NewWallet().PublicKey(),
		DevicePK: solana.NewWallet().PublicKey(),
	}

	instr, err := geolocation.BuildAddParentDeviceInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 4, "should have 4 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
	// probe is writable (being modified); payer is writable (funds any realloc rent)
	require.True(t, accounts[0].IsWritable, "probe account must be writable")
	require.True(t, accounts[2].IsWritable, "payer must be writable to fund realloc")
	require.True(t, accounts[2].IsSigner, "payer must be signer")
}

// TestSDK_Geolocation_BuildAddParentDeviceInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildAddParentDeviceInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.AddParentDeviceInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.AddParentDeviceInstructionConfig{
				Payer:    solana.PublicKey{},
				ProbePK:  solana.NewWallet().PublicKey(),
				DevicePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing probe public key",
			config: geolocation.AddParentDeviceInstructionConfig{
				Payer:    solana.NewWallet().PublicKey(),
				ProbePK:  solana.PublicKey{},
				DevicePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "probe public key is required",
		},
		{
			name: "missing device public key",
			config: geolocation.AddParentDeviceInstructionConfig{
				Payer:    solana.NewWallet().PublicKey(),
				ProbePK:  solana.NewWallet().PublicKey(),
				DevicePK: solana.PublicKey{},
			},
			errMsg: "device public key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildAddParentDeviceInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestSDK_Geolocation_BuildRemoveParentDeviceInstruction_HappyPath tests successful instruction creation
func TestSDK_Geolocation_BuildRemoveParentDeviceInstruction_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	config := geolocation.RemoveParentDeviceInstructionConfig{
		Payer:    solana.NewWallet().PublicKey(),
		ProbePK:  solana.NewWallet().PublicKey(),
		DevicePK: solana.NewWallet().PublicKey(),
	}

	instr, err := geolocation.BuildRemoveParentDeviceInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, instr)

	accounts := instr.Accounts()
	require.Len(t, accounts, 3, "should have 3 accounts")
	instrData, err := instr.Data()
	require.NoError(t, err)
	require.NotEmpty(t, instrData, "instruction data should not be empty")
	require.Equal(t, programID, instr.ProgramID(), "program ID should match")
}

// TestSDK_Geolocation_BuildRemoveParentDeviceInstruction_MissingRequiredFields tests validation errors
func TestSDK_Geolocation_BuildRemoveParentDeviceInstruction_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()

	tests := []struct {
		name   string
		config geolocation.RemoveParentDeviceInstructionConfig
		errMsg string
	}{
		{
			name: "missing payer",
			config: geolocation.RemoveParentDeviceInstructionConfig{
				Payer:    solana.PublicKey{},
				ProbePK:  solana.NewWallet().PublicKey(),
				DevicePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "payer public key is required",
		},
		{
			name: "missing probe public key",
			config: geolocation.RemoveParentDeviceInstructionConfig{
				Payer:    solana.NewWallet().PublicKey(),
				ProbePK:  solana.PublicKey{},
				DevicePK: solana.NewWallet().PublicKey(),
			},
			errMsg: "probe public key is required",
		},
		{
			name: "missing device public key",
			config: geolocation.RemoveParentDeviceInstructionConfig{
				Payer:    solana.NewWallet().PublicKey(),
				ProbePK:  solana.NewWallet().PublicKey(),
				DevicePK: solana.PublicKey{},
			},
			errMsg: "device public key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := geolocation.BuildRemoveParentDeviceInstruction(programID, tt.config)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errMsg)
		})
	}
}
