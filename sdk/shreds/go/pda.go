package shreds

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

var (
	seedProgramConfig              = []byte("program_config")
	seedExecutionController        = []byte("execution_controller")
	seedClientSeat                 = []byte("client_seat")
	seedPaymentEscrow              = []byte("payment_escrow")
	seedShredDistribution          = []byte("shred_distribution")
	seedValidatorClientRewards     = []byte("validator_client_rewards")
	seedInstantSeatAllocationRequest = []byte("instant_seat_allocation_request")
	seedWithdrawSeatRequest        = []byte("withdraw_seat_request")
	seedMetroHistory               = []byte("metro_history")
	seedDeviceHistory              = []byte("device_history")
)

func DeriveProgramConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedProgramConfig}, programID)
}

func DeriveExecutionControllerPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedExecutionController}, programID)
}

func DeriveClientSeatPDA(programID solana.PublicKey, deviceKey solana.PublicKey, clientIPBits uint32) (solana.PublicKey, uint8, error) {
	ipBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(ipBytes, clientIPBits)
	return solana.FindProgramAddress([][]byte{seedClientSeat, deviceKey[:], ipBytes}, programID)
}

func DerivePaymentEscrowPDA(programID solana.PublicKey, clientSeatKey solana.PublicKey, withdrawAuthorityKey solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPaymentEscrow, clientSeatKey[:], withdrawAuthorityKey[:]}, programID)
}

func DeriveShredDistributionPDA(programID solana.PublicKey, subscriptionEpoch uint64) (solana.PublicKey, uint8, error) {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, subscriptionEpoch)
	return solana.FindProgramAddress([][]byte{seedShredDistribution, epochBytes}, programID)
}

func DeriveValidatorClientRewardsPDA(programID solana.PublicKey, clientID uint16) (solana.PublicKey, uint8, error) {
	idBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(idBytes, clientID)
	return solana.FindProgramAddress([][]byte{seedValidatorClientRewards, idBytes}, programID)
}

func DeriveInstantSeatAllocationRequestPDA(programID solana.PublicKey, deviceKey solana.PublicKey, clientIPBits uint32) (solana.PublicKey, uint8, error) {
	ipBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(ipBytes, clientIPBits)
	return solana.FindProgramAddress([][]byte{seedInstantSeatAllocationRequest, deviceKey[:], ipBytes}, programID)
}

func DeriveWithdrawSeatRequestPDA(programID solana.PublicKey, clientSeatKey solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedWithdrawSeatRequest, clientSeatKey[:]}, programID)
}

func DeriveMetroHistoryPDA(programID solana.PublicKey, exchangeKey solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedMetroHistory, exchangeKey[:]}, programID)
}

func DeriveDeviceHistoryPDA(programID solana.PublicKey, deviceKey solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedDeviceHistory, deviceKey[:]}, programID)
}
