package telemetry_test

import (
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/near/borsh-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_InitializeInternetLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	agentPK := solana.NewWallet().PublicKey()
	originLocationPK := solana.NewWallet().PublicKey()
	targetLocationPK := solana.NewWallet().PublicKey()
	dataProviderName := "test"
	epoch := uint64(42)
	interval := uint64(100_000)

	config := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginLocationPK:             originLocationPK,
		TargetLocationPK:             targetLocationPK,
		DataProviderName:             dataProviderName,
		Epoch:                        epoch,
		SamplingIntervalMicroseconds: interval,
	}

	ix, err := telemetry.BuildInitializeInternetLatencySamplesInstruction(programID, agentPK, config)
	require.NoError(t, err)
	require.NotNil(t, ix)

	require.Equal(t, programID, ix.ProgramID(), "program ID should match")

	accounts := ix.Accounts()
	require.Len(t, accounts, 5)

	require.Equal(t, agentPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsSigner)
	require.True(t, accounts[1].IsWritable)

	require.Equal(t, solana.SystemProgramID, accounts[4].PublicKey)
	require.False(t, accounts[4].IsSigner)
	require.False(t, accounts[4].IsWritable)

	data, err := ix.Data()
	require.NoError(t, err)
	require.Greater(t, len(data), 0, "instruction data should not be empty")
	require.Equal(t, uint8(telemetry.InitializeInternetLatencySamplesInstructionIndex), data[0], "discriminator mismatch")
}
func TestSDK_Telemetry_InitializeInternetLatencySamples_MissingFields(t *testing.T) {
	t.Parallel()

	base := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginLocationPK:             solana.NewWallet().PublicKey(),
		TargetLocationPK:             solana.NewWallet().PublicKey(),
		DataProviderName:             "test",
		Epoch:                        42,
		SamplingIntervalMicroseconds: 100_000,
	}

	tests := []struct {
		name        string
		mutate      func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig)
		expectError string
	}{
		{
			name: "missing_origin_location_pk",
			mutate: func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) {
				c.OriginLocationPK = solana.PublicKey{}
			},
			expectError: "origin location public key is required",
		},
		{
			name: "missing_target_location_pk",
			mutate: func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) {
				c.TargetLocationPK = solana.PublicKey{}
			},
			expectError: "target location public key is required",
		},
		{
			name: "missing_data_provider_name",
			mutate: func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) {
				c.DataProviderName = ""
			},
			expectError: "data provider name is required",
		},
		{
			name: "data_provider_name_too_long",
			mutate: func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) {
				c.DataProviderName = strings.Repeat("a", telemetry.MaxInternetLatencyDataProviderNameLength+1)
			},
			expectError: "data provider name is too long, max length is 32",
		},
		{
			name:        "missing_epoch",
			mutate:      func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) { c.Epoch = 0 },
			expectError: "epoch is required",
		},
		{
			name: "missing_sampling_interval",
			mutate: func(c *telemetry.InitializeInternetLatencySamplesInstructionConfig) {
				c.SamplingIntervalMicroseconds = 0
			},
			expectError: "sampling interval microseconds is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)

			programID := solana.NewWallet().PublicKey()
			signerPK := solana.NewWallet().PublicKey()
			ix, err := telemetry.BuildInitializeInternetLatencySamplesInstruction(programID, signerPK, config)
			require.ErrorContains(t, err, tt.expectError)
			require.Nil(t, ix)
		})
	}
}

func TestSDK_Telemetry_InitializeInternetLatencySamples_BorshEncoding(t *testing.T) {
	t.Parallel()

	config := telemetry.InitializeInternetLatencySamplesInstructionConfig{
		OriginLocationPK:             solana.NewWallet().PublicKey(),
		TargetLocationPK:             solana.NewWallet().PublicKey(),
		DataProviderName:             "test",
		Epoch:                        99,
		SamplingIntervalMicroseconds: 250_000,
	}

	programID := solana.NewWallet().PublicKey()
	signerPK := solana.NewWallet().PublicKey()
	ix, err := telemetry.BuildInitializeInternetLatencySamplesInstruction(programID, signerPK, config)
	require.NoError(t, err)

	var decoded struct {
		Discriminator                uint8
		DataProviderName             string
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}

	data, err := ix.Data()
	require.NoError(t, err)

	err = borsh.Deserialize(&decoded, data)
	require.NoError(t, err)

	require.Equal(t, uint8(telemetry.InitializeInternetLatencySamplesInstructionIndex), decoded.Discriminator)
	require.Equal(t, config.DataProviderName, decoded.DataProviderName)
	require.Equal(t, config.Epoch, decoded.Epoch)
	require.Equal(t, config.SamplingIntervalMicroseconds, decoded.SamplingIntervalMicroseconds)
}
