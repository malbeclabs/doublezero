package dzsdk_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/require"
)

func TestSDK_Client(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	t.Run("test_default_program_id", func(t *testing.T) {
		t.Parallel()

		client, err := dzsdk.New(log, "endpoint")
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		want := solana.MustPublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET)
		require.Equal(t, want, client.Serviceability.ProgramID())
	})

	t.Run("test_override_program_id", func(t *testing.T) {
		t.Parallel()

		programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
		client, err := dzsdk.New(log, "endpoint", dzsdk.WithSigner(&solana.NewWallet().PrivateKey), dzsdk.WithServiceabilityProgramID(programId))
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		want := solana.MustPublicKeyFromBase58(programId)
		require.Equal(t, want, client.Serviceability.ProgramID())
	})

	t.Run("test_with_telemetry_program_id", func(t *testing.T) {
		t.Parallel()

		telemetryProgramID := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
		client, err := dzsdk.New(log, "endpoint", dzsdk.WithSigner(&solana.NewWallet().PrivateKey), dzsdk.WithTelemetryProgramID(telemetryProgramID))
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		want := solana.MustPublicKeyFromBase58(telemetryProgramID)
		require.Equal(t, want, client.Telemetry.ProgramID())
	})

	t.Run("test_default_telemetry_program_id", func(t *testing.T) {
		t.Parallel()

		client, err := dzsdk.New(log, "endpoint", dzsdk.WithSigner(&solana.NewWallet().PrivateKey))
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		want := solana.MustPublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_TESTNET)
		require.Equal(t, want, client.Telemetry.ProgramID())
	})

	t.Run("test_with_signer", func(t *testing.T) {
		t.Parallel()

		privateKey := solana.NewWallet().PrivateKey
		client, err := dzsdk.New(log, "endpoint", dzsdk.WithSigner(&privateKey))
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		require.NotNil(t, client.Telemetry.Signer())
		require.Equal(t, privateKey.PublicKey(), client.Telemetry.Signer().PublicKey())
	})

	t.Run("test_without_signer", func(t *testing.T) {
		t.Parallel()

		client, err := dzsdk.New(log, "endpoint")
		require.NoError(t, err)
		require.NotNil(t, client.Serviceability)
		require.NotNil(t, client.Telemetry)
		require.Nil(t, client.Telemetry.Signer())
	})
}
