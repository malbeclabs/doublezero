package revdist

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type mockRPC struct {
	accounts map[solana.PublicKey]*rpc.Account
}

func (m *mockRPC) GetAccountInfo(_ context.Context, account solana.PublicKey) (*rpc.GetAccountInfoResult, error) {
	acct, ok := m.accounts[account]
	if !ok {
		return &rpc.GetAccountInfoResult{}, nil
	}
	return &rpc.GetAccountInfoResult{Value: acct}, nil
}

func (m *mockRPC) GetProgramAccounts(_ context.Context, _ solana.PublicKey) (rpc.GetProgramAccountsResult, error) {
	return nil, nil
}

func (m *mockRPC) GetProgramAccountsWithOpts(_ context.Context, _ solana.PublicKey, _ *rpc.GetProgramAccountsOpts) (rpc.GetProgramAccountsResult, error) {
	return nil, nil
}

func (m *mockRPC) GetMinimumBalanceForRentExemption(_ context.Context, _ uint64, _ rpc.CommitmentType) (uint64, error) {
	return 890880, nil
}

func buildAccountData(disc [8]byte, structSize int) []byte {
	data := make([]byte, discriminatorSize+structSize)
	copy(data[:8], disc[:])
	return data
}

func TestFetchJournal(t *testing.T) {
	programID := testProgramID
	journalAddr, _, _ := DeriveJournalPDA(programID)

	data := buildAccountData(DiscriminatorJournal, 64)
	// Set TotalSOLBalance at offset 8+8 = 16 from start of data.
	binary.LittleEndian.PutUint64(data[discriminatorSize+8:], 12345)

	mock := &mockRPC{
		accounts: map[solana.PublicKey]*rpc.Account{
			journalAddr: {
				Data: rpc.DataBytesOrJSONFromBytes(data),
			},
		},
	}

	client := New(mock, programID)
	journal, err := client.FetchJournal(context.Background())
	if err != nil {
		t.Fatalf("FetchJournal: %v", err)
	}
	if journal.TotalSOLBalance != 12345 {
		t.Errorf("TotalSOLBalance = %d, want 12345", journal.TotalSOLBalance)
	}
}

func TestFetchConfig(t *testing.T) {
	programID := testProgramID
	configAddr, _, _ := DeriveConfigPDA(programID)

	data := buildAccountData(DiscriminatorProgramConfig, 600)
	// Set NextCompletedDZEpoch at offset 8+8 = 16 from start.
	binary.LittleEndian.PutUint64(data[discriminatorSize+8:], 42)

	mock := &mockRPC{
		accounts: map[solana.PublicKey]*rpc.Account{
			configAddr: {
				Data: rpc.DataBytesOrJSONFromBytes(data),
			},
		},
	}

	client := New(mock, programID)
	config, err := client.FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if config.NextCompletedDZEpoch != 42 {
		t.Errorf("NextCompletedDZEpoch = %d, want 42", config.NextCompletedDZEpoch)
	}
}

func TestFetchAccountNotFound(t *testing.T) {
	mock := &mockRPC{accounts: map[solana.PublicKey]*rpc.Account{}}
	client := New(mock, testProgramID)
	_, err := client.FetchJournal(context.Background())
	if err != ErrAccountNotFound {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestInvalidDiscriminator(t *testing.T) {
	programID := testProgramID
	journalAddr, _, _ := DeriveJournalPDA(programID)

	data := make([]byte, discriminatorSize+64)
	// Leave discriminator as zeros (invalid).

	mock := &mockRPC{
		accounts: map[solana.PublicKey]*rpc.Account{
			journalAddr: {
				Data: rpc.DataBytesOrJSONFromBytes(data),
			},
		},
	}

	client := New(mock, programID)
	_, err := client.FetchJournal(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid discriminator")
	}
}

func TestFetchValidatorDebtsNoLedger(t *testing.T) {
	client := New(&mockRPC{accounts: map[solana.PublicKey]*rpc.Account{}}, testProgramID)
	_, err := client.FetchValidatorDebts(context.Background(), 1)
	if err != ErrLedgerClientNil {
		t.Errorf("expected ErrLedgerClientNil, got %v", err)
	}
}
