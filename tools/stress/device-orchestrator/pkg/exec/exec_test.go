package exec

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPForIndex(t *testing.T) {
	t.Parallel()

	base := [4]byte{100, 64, 0, 0}
	tests := []struct {
		idx  int
		want [4]byte
	}{
		{0, [4]byte{100, 64, 0, 0}},
		{1, [4]byte{100, 64, 0, 1}},
		{255, [4]byte{100, 64, 0, 255}},
		{256, [4]byte{100, 64, 1, 0}},
		{1000, [4]byte{100, 64, 3, 232}},
	}
	for _, tc := range tests {
		got := ipForIndex(base, tc.idx)
		assert.Equal(t, tc.want, got, "idx=%d", tc.idx)
	}
}

// stubRPC implements serviceability.RPCClient for fetchTunnelID tests.
type stubRPC struct {
	accountInfo *solanarpc.GetAccountInfoResult
	err         error
}

func (s *stubRPC) GetProgramAccounts(context.Context, solana.PublicKey) (solanarpc.GetProgramAccountsResult, error) {
	return nil, nil
}

func (s *stubRPC) GetAccountInfo(context.Context, solana.PublicKey) (*solanarpc.GetAccountInfoResult, error) {
	return s.accountInfo, s.err
}

func TestFetchTunnelID_ReadsFromUserAccount(t *testing.T) {
	t.Parallel()

	owner := solana.NewWallet().PublicKey()
	device := solana.NewWallet().PublicKey()

	// Hand-encode a User account body matching DeserializeUser's field order.
	// All fields zero except TunnelId so the test pin-points that read path.
	const tunnelID uint16 = 4242
	body := makeUserAccountBytes(owner, device, [4]byte{10, 0, 0, 5}, tunnelID)

	stub := &stubRPC{
		accountInfo: &solanarpc.GetAccountInfoResult{
			Value: &solanarpc.Account{
				Data: solanarpc.DataBytesOrJSONFromBytes(body),
			},
		},
	}
	live := &Live{cfg: Config{RPC: stub}}

	got, err := live.fetchTunnelID(context.Background(), solana.NewWallet().PublicKey())
	require.NoError(t, err)
	assert.Equal(t, tunnelID, got)
}

func TestFetchTunnelID_AccountMissing(t *testing.T) {
	t.Parallel()

	live := &Live{cfg: Config{RPC: &stubRPC{accountInfo: &solanarpc.GetAccountInfoResult{Value: nil}}}}
	_, err := live.fetchTunnelID(context.Background(), solana.NewWallet().PublicKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// makeUserAccountBytes serializes a User account body with the minimum fields
// the test needs. Matches the field order in serviceability.DeserializeUser.
func makeUserAccountBytes(owner, device solana.PublicKey, clientIP [4]byte, tunnelID uint16) []byte {
	b := make([]byte, 0, 256)
	b = append(b, byte(serviceability.UserType)) // AccountType
	b = append(b, owner[:]...)                   // Owner [32]
	b = append(b, make([]byte, 16)...)           // Index u128
	b = append(b, 0)                             // BumpSeed
	b = append(b, byte(serviceability.UserTypeIBRL))
	b = append(b, make([]byte, 32)...) // TenantPubKey (zero)
	b = append(b, device[:]...)        // DevicePubKey
	b = append(b, byte(serviceability.CyoaTypeGREOverDIA))
	b = append(b, clientIP[:]...)     // ClientIp [4]
	b = append(b, make([]byte, 4)...) // DzIp [4]
	var tidBuf [2]byte
	binary.LittleEndian.PutUint16(tidBuf[:], tunnelID)
	b = append(b, tidBuf[:]...)       // TunnelId u16 LE
	b = append(b, make([]byte, 5)...) // TunnelNet
	b = append(b, byte(serviceability.UserStatusActivated))
	b = append(b, 0, 0, 0, 0)          // Publishers len
	b = append(b, 0, 0, 0, 0)          // Subscribers len
	b = append(b, make([]byte, 32)...) // ValidatorPubKey
	b = append(b, make([]byte, 4)...)  // TunnelEndpoint
	b = append(b, 0)                   // TunnelFlags
	b = append(b, 0)                   // BgpStatus
	b = append(b, make([]byte, 8)...)  // LastBgpUpAt
	b = append(b, make([]byte, 8)...)  // LastBgpReportedAt
	b = append(b, make([]byte, 8)...)  // BgpRttNs
	return b
}
