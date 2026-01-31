package revdist

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"unsafe"
)

func newReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

func TestStructSizes(t *testing.T) {
	tests := []struct {
		name     string
		size     uintptr
		expected uintptr
	}{
		{"ProgramConfig", unsafe.Sizeof(ProgramConfig{}), 600},
		{"Distribution", unsafe.Sizeof(Distribution{}), 448},
		{"SolanaValidatorDeposit", unsafe.Sizeof(SolanaValidatorDeposit{}), 96},
		{"ContributorRewards", unsafe.Sizeof(ContributorRewards{}), 600},
		{"Journal", unsafe.Sizeof(Journal{}), 64},
		{"RecipientShare", unsafe.Sizeof(RecipientShare{}), 34},
		{"RecipientShares", unsafe.Sizeof(RecipientShares{}), 272},
		{"DistributionParameters", unsafe.Sizeof(DistributionParameters{}), 328},
		{"CommunityBurnRateParameters", unsafe.Sizeof(CommunityBurnRateParameters{}), 24},
		{"SolanaValidatorFeeParameters", unsafe.Sizeof(SolanaValidatorFeeParameters{}), 40},
		{"RelayParameters", unsafe.Sizeof(RelayParameters{}), 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.size != tt.expected {
				t.Errorf("sizeof(%s) = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}
}

func TestJournalDeserialization(t *testing.T) {
	// Build a known Journal byte sequence.
	data := make([]byte, 64)
	data[0] = 1   // BumpSeed
	data[1] = 2   // Token2ZPDABumpSeed
	binary.LittleEndian.PutUint64(data[8:], 1000)  // TotalSOLBalance
	binary.LittleEndian.PutUint64(data[16:], 2000) // Total2ZBalance
	binary.LittleEndian.PutUint64(data[24:], 3000) // Swap2ZDestinationBalance
	binary.LittleEndian.PutUint64(data[32:], 4000) // SwappedSOLAmount
	binary.LittleEndian.PutUint64(data[40:], 5)    // NextDZEpochToSweepTokens

	var journal Journal
	if err := binary.Read(newReader(data), binary.LittleEndian, &journal); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if journal.BumpSeed != 1 {
		t.Errorf("BumpSeed = %d, want 1", journal.BumpSeed)
	}
	if journal.TotalSOLBalance != 1000 {
		t.Errorf("TotalSOLBalance = %d, want 1000", journal.TotalSOLBalance)
	}
	if journal.NextDZEpochToSweepTokens != 5 {
		t.Errorf("NextDZEpochToSweepTokens = %d, want 5", journal.NextDZEpochToSweepTokens)
	}
}

func TestSolanaValidatorDepositDeserialization(t *testing.T) {
	data := make([]byte, 96)
	// Set NodeID to a known pattern.
	for i := range 32 {
		data[i] = byte(i + 1)
	}
	binary.LittleEndian.PutUint64(data[32:], 999)

	var deposit SolanaValidatorDeposit
	if err := binary.Read(newReader(data), binary.LittleEndian, &deposit); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if deposit.NodeID[0] != 1 || deposit.NodeID[31] != 32 {
		t.Error("NodeID not deserialized correctly")
	}
	if deposit.WrittenOffSOLDebt != 999 {
		t.Errorf("WrittenOffSOLDebt = %d, want 999", deposit.WrittenOffSOLDebt)
	}
}
