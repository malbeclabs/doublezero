package telemetry_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/near/borsh-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_WriteInternetLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	oracleAgentPK := solana.NewWallet().PublicKey()
	originLocationPK := solana.NewWallet().PublicKey()
	targetLocationPK := solana.NewWallet().PublicKey()
	dataProviderName := "test"
	epoch := uint64(123)
	timestamp := uint64(1_600_000_000)
	samples := []uint32{1, 2, 3, 4}

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           originLocationPK,
		TargetExchangePK:           targetLocationPK,
		DataProviderName:           dataProviderName,
		Epoch:                      epoch,
		StartTimestampMicroseconds: timestamp,
		Samples:                    samples,
	}

	ix, err := telemetry.BuildWriteInternetLatencySamplesInstruction(programID, oracleAgentPK, config)
	require.NoError(t, err)
	require.NotNil(t, ix)

	require.Equal(t, programID, ix.ProgramID())
	accounts := ix.Accounts()
	require.Len(t, accounts, 3)

	require.Equal(t, oracleAgentPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsSigner)
	require.False(t, accounts[1].IsWritable)

	require.Equal(t, solana.SystemProgramID, accounts[2].PublicKey)
	require.False(t, accounts[2].IsSigner)
	require.False(t, accounts[2].IsWritable)

	data, err := ix.Data()
	require.NoError(t, err)
	require.Greater(t, len(data), 0)
	require.Equal(t, uint8(telemetry.WriteInternetLatencySamplesInstructionIndex), data[0])
}

func TestSDK_Telemetry_WriteInternetLatencySamples_MissingFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	base := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test",
		Epoch:                      123,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	tests := []struct {
		name        string
		mutate      func(*telemetry.WriteInternetLatencySamplesInstructionConfig)
		expectError string
	}{
		{
			name: "missing_origin_location_pk",
			mutate: func(c *telemetry.WriteInternetLatencySamplesInstructionConfig) {
				c.OriginExchangePK = solana.PublicKey{}
			},
			expectError: "origin location public key is required",
		},
		{
			name: "missing_target_location_pk",
			mutate: func(c *telemetry.WriteInternetLatencySamplesInstructionConfig) {
				c.TargetExchangePK = solana.PublicKey{}
			},
			expectError: "target location public key is required",
		},
		{
			name: "missing_data_provider_name",
			mutate: func(c *telemetry.WriteInternetLatencySamplesInstructionConfig) {
				c.DataProviderName = ""
			},
			expectError: "data provider name is required",
		},
		{
			name:        "missing_epoch",
			mutate:      func(c *telemetry.WriteInternetLatencySamplesInstructionConfig) { c.Epoch = 0 },
			expectError: "epoch is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)

			signerPK := solana.NewWallet().PublicKey()
			ix, err := telemetry.BuildWriteInternetLatencySamplesInstruction(programID, signerPK, config)
			require.ErrorContains(t, err, tt.expectError)
			require.Nil(t, ix)
		})
	}
}

func TestSDK_Telemetry_WriteInternetLatencySamples_BorshEncoding(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	timestamp := uint64(1_650_000_000)
	samples := []uint32{100, 200, 300}

	config := telemetry.WriteInternetLatencySamplesInstructionConfig{
		OriginExchangePK:           solana.NewWallet().PublicKey(),
		TargetExchangePK:           solana.NewWallet().PublicKey(),
		DataProviderName:           "test",
		Epoch:                      555,
		StartTimestampMicroseconds: timestamp,
		Samples:                    samples,
	}

	signerPK := solana.NewWallet().PublicKey()
	ix, err := telemetry.BuildWriteInternetLatencySamplesInstruction(programID, signerPK, config)
	require.NoError(t, err)

	var decoded struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
	}

	data, err := ix.Data()
	require.NoError(t, err)

	err = borsh.Deserialize(&decoded, data)
	require.NoError(t, err)

	require.Equal(t, uint8(telemetry.WriteInternetLatencySamplesInstructionIndex), decoded.Discriminator)
	require.Equal(t, timestamp, decoded.StartTimestampMicroseconds)
	require.Equal(t, samples, decoded.Samples)
}
