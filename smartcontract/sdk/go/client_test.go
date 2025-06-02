package dzsdk

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var configPayload = `
02baae1ce3bce5130ae5f46b6d47884ab60b6d22f55b0c0cfac
f14abe7ea3118aefd4cfe0000e9fd0000ac10000010a9fe0000
10df00000004a2aa7d81b23bd270048af6aae3813deae93cdb4
c7630e96007cba72863408022
`

var locationPayload = `
030a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e482897806000000000000000000000000000000fea2
e3b2a599d54140b03f0a3a80786140000000000103000000747
96f05000000546f6b796f020000004a5065483c031c496dd52f
fd841907413a9259d8668196f939b255c274ee9fde6363
`

var exchangePayload = `
040a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e48289780c000000000000000000000000000000ff35
71de7a8e0f494029845566ba482140000000000104000000786
67261090000004672616e6b6675727456afd04ec486ee9054d1
2e32370a06c9c9a7213a21c08df00dcc44358fbaa4d2
`

var devicePayload = `
050a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e482897816000000000000000000000000000000ff65
483c031c496dd52ffd841907413a9259d8668196f939b255c27
4ee9fde636334e9df9b428bf14a9c4dbd07ca455b10aa67fbd9
0d17007ee2ad45706bd9a5a000b4579a7001080000007479322
d647a303101000000b4579a701d903a23e92446591b0bb98794
f3e278aeafc84fd20ad064acb8cc2f8198607689
`

var tunnelPayload = `
060a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e48289781e000000000000000000000000000000fb90
3a23e92446591b0bb98794f3e278aeafc84fd20ad064acb8cc2
f8198607689246e25c9403fba46e89122ff5d0fcc1febb51d4b
4ce64f17ad56c47b3d1d7f3f0100e40b5402000000282300008
0c3c9010000000080969800000000000500ac10000a1f011100
00007479322d647a30313a6c61322d647a3031ad2570a0cf277
61cab55a3f26d85fb2081f89e9285b8054ce53ae2a71cc6a7bd
`

var userPayload = `
07baae1ce3bce5130ae5f46b6d47884ab60b6d22f55b0c0cfac
f14abe7ea3118ae1f000000000000000000000000000000fc00
000000000000000000000000000000000000000000000000000
0000000000000d2b30c6593b3dd99bbdde9c8e29eb9291adefb
c11544a47f17d9472cae13fdfc010a0000010a000001f401a9f
e00001f010000000000000000fcef68d5d9eae991fd7d6284da
d2f2d755ed5b09e8b5e76f5988360d7687fac6
`

var multicastgroupPayload = `
08baae1ce3bce5130ae5f46b6d47884ab60b6d22f55b0c0cfac
f14abe7ea3118ae23000000000000000000000000000000ff00
000000000000000000000000000000000000000000000000000
00000000000d000000000ca9a3b0000000001040000006a6974
6f01000000baae1ce3bce5130ae5f46b6d47884ab60b6d22f55
b0c0cfacf14abe7ea3118ae01000000baae1ce3bce5130ae5f4
6b6d47884ab60b6d22f55b0c0cfacf14abe7ea3118ae0200000
059d127e5abbd5ce88c1de4abe70b132b4c79d4a1ffe781952a
8bdf13801d2cb63a316a4505a39d6026a55bf2894e30bad33bc
1631ce1bd925f02ab4c7994e9d40200000041c6964053cf55d2
925472dbe01afbc327f5abfdb917ec234ecabc09e5290b2b3a3
16a4505a39d6026a55bf2894e30bad33bc1631ce1bd925f02ab
4c7994e9d4b745f92183e1b409bb7006560f858cf3bfa557c75
cd967182a00392200b5de78
`

type mockSolanaClient struct {
	payload string
}

func (m *mockSolanaClient) GetProgramAccounts(context.Context, solana.PublicKey) (rpc.GetProgramAccountsResult, error) {
	data, err := hex.DecodeString(strings.ReplaceAll(m.payload, "\n", ""))
	if err != nil {
		return nil, err
	}
	return []*rpc.KeyedAccount{
		{
			Pubkey: solana.MustPublicKeyFromBase58(PROGRAM_ID_DEVNET),
			Account: &rpc.Account{
				Data: rpc.DataBytesOrJSONFromBytes(data),
			},
		},
	}, nil
}

func getOwner(payload string) [32]byte {
	return getPubKeyOffset(payload, 1, 33)
}

func getPubKey(payload string) [32]byte {
	p, _ := hex.DecodeString(strings.ReplaceAll(payload, "\n", ""))

	return getPubKeyOffset(payload, len(p)-32, len(p))
}

func getPubKeyOffset(payload string, start, end int) [32]byte {
	var d [32]byte
	p, _ := hex.DecodeString(strings.ReplaceAll(payload, "\n", ""))
	copy(d[:], p[start:end])
	return d
}

func TestRpcClient(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		Payload     string
		Want        *Client
	}{
		{
			Name:        "parse_valid_config",
			Description: "parse and populate a valid config struct",
			Payload:     strings.TrimSuffix(configPayload, "\n"),
			Want: &Client{
				Config: Config{
					AccountType:         ConfigType,
					Owner:               getOwner(configPayload),
					Bump_seed:           253,
					Local_asn:           65100,
					Remote_asn:          65001,
					TunnelTunnelBlock:   [5]byte{172, 16, 0, 0, 16},
					UserTunnelBlock:     [5]byte{169, 254, 0, 0, 16},
					MulticastGroupBlock: [5]byte{223, 0, 0, 0, 4},
					PubKey:              getPubKey(configPayload),
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Tunnels:         []Tunnel{},
				Users:           []User{},
				Exchanges:       []Exchange{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_exchange",
			Description: "parse and populate a valid exchange struct",
			Payload:     strings.TrimSuffix(exchangePayload, "\n"),
			Want: &Client{
				Exchanges: []Exchange{
					{
						AccountType: ExchangeType,
						Index:       Uint128{High: 12, Low: 0},
						Bump_seed:   255,
						Owner:       getOwner(exchangePayload),
						Lat:         50.1215356432098,
						Lng:         8.642047117175098,
						LocId:       0,
						Status:      1,
						Code:        "xfra",
						Name:        "Frankfurt",
						PubKey:      getPubKey(exchangePayload),
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Tunnels:         []Tunnel{},
				Users:           []User{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_device",
			Description: "parse and populate a valid device struct",
			Payload:     strings.TrimSuffix(devicePayload, "\n"),
			Want: &Client{
				Devices: []Device{
					{
						AccountType:    DeviceType,
						Index:          Uint128{High: 22, Low: 0},
						Bump_seed:      255,
						Owner:          getOwner(exchangePayload),
						LocationPubKey: getPubKeyOffset(devicePayload, 50, 82),
						ExchangePubKey: getPubKeyOffset(devicePayload, 82, 114),
						DeviceType:     0,
						PublicIp:       [4]byte{0xb4, 0x57, 0x9a, 0x70},
						Status:         1,
						Code:           "ty2-dz01",
						DzPrefixes:     [][5]byte{{0xb4, 0x57, 0x9a, 0x70, 0x1d}},
						PubKey:         getPubKey(devicePayload),
					},
				},
				Locations:       []Location{},
				Exchanges:       []Exchange{},
				Tunnels:         []Tunnel{},
				Users:           []User{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_location",
			Description: "parse and populate a valid location struct",
			Payload:     strings.TrimSuffix(locationPayload, "\n"),
			Want: &Client{
				Locations: []Location{
					{
						AccountType: LocationType,
						Index:       Uint128{High: 6, Low: 0},
						Bump_seed:   254,
						Owner:       getOwner(locationPayload),
						Lat:         35.66875144228767,
						Lng:         139.76565267564501,
						LocId:       0,
						Status:      1,
						Code:        "tyo",
						Name:        "Tokyo",
						Country:     "JP",
						PubKey:      getPubKey(locationPayload),
					},
				},
				Exchanges:       []Exchange{},
				Devices:         []Device{},
				Tunnels:         []Tunnel{},
				Users:           []User{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_user",
			Description: "parse and populate a valid user struct",
			Payload:     strings.TrimSuffix(userPayload, "\n"),
			Want: &Client{
				Users: []User{
					{
						AccountType:  UserType,
						Index:        Uint128{High: 31, Low: 0},
						Bump_seed:    252,
						Owner:        getOwner(userPayload),
						UserType:     UserTypeIBRL,
						TenantPubKey: getPubKeyOffset(userPayload, 51, 83),
						DevicePubKey: getPubKeyOffset(userPayload, 83, 115),
						CyoaType:     CyoaTypeGREOverDIA,
						ClientIp:     [4]byte{0x0a, 0x00, 0x00, 0x01},
						TunnelId:     500,
						TunnelNet:    [5]byte{0xa9, 0xfe, 0x00, 0x00, 0x1f},
						DzIp:         [4]byte{0x0a, 0x00, 0x00, 0x01},
						Status:       UserStatusActivated,
						PubKey:       getPubKey(userPayload),
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Tunnels:         []Tunnel{},
				Exchanges:       []Exchange{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_tunnel",
			Description: "parse and populate a valid tunnel struct",
			Payload:     strings.TrimSuffix(tunnelPayload, "\n"),
			Want: &Client{
				Tunnels: []Tunnel{
					{
						AccountType: TunnelType,
						Index:       Uint128{High: 30, Low: 0},
						Bump_seed:   251,
						Owner:       getOwner(tunnelPayload),
						SideAPubKey: getPubKeyOffset(tunnelPayload, 50, 82),
						SideZPubKey: getPubKeyOffset(tunnelPayload, 82, 114),
						TunnelType:  TunnelTunnelTypeMPLSoverGRE,
						Bandwidth:   10000000000,
						Mtu:         9000,
						DelayNs:     30000000,
						JitterNs:    10000000,
						TunnelId:    5,
						TunnelNet:   [5]byte{0xac, 0x10, 0x00, 0x0a, 0x1f},
						Status:      TunnelStatusActivated,
						Code:        "ty2-dz01:la2-dz01",
						PubKey:      getPubKey(tunnelPayload),
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Exchanges:       []Exchange{},
				Users:           []User{},
				MulticastGroups: []MulticastGroup{},
			},
		},
		{
			Name:        "parse_valid_multicastgroup",
			Description: "parse and populate a valid multicastgroup struct",
			Payload:     strings.TrimSuffix(multicastgroupPayload, "\n"),
			Want: &Client{
				Tunnels:   []Tunnel{},
				Locations: []Location{},
				Devices:   []Device{},
				Exchanges: []Exchange{},
				Users:     []User{},
				MulticastGroups: []MulticastGroup{
					{
						AccountType:  MulticastGroupType,
						Index:        Uint128{High: 35, Low: 0},
						Bump_seed:    255,
						Owner:        getOwner(multicastgroupPayload),
						TenantPubKey: [32]byte{},
						MulticastIp:  [4]byte{0xd0, 0x00, 0x00, 0x00},
						MaxBandwidth: 1000000000,
						Status:       MulticastGroupStatusActivated,
						Code:         "mg01",
						PubAllowList: [][32]uint8{{0xba, 0xae, 0x1c, 0xe3, 0xbc, 0xe5, 0x13, 0x0a, 0xe5, 0xf4, 0x6b, 0x6d, 0x47, 0x88, 0x4a, 0xb6, 0x0b, 0x6d, 0x22, 0xf5, 0x5b, 0x0c, 0x0c, 0xfa, 0xcf, 0x14, 0xab, 0xe7, 0xea, 0x31, 0x18, 0xae}},
						SubAllowList: [][32]uint8{{0xba, 0xae, 0x1c, 0xe3, 0xbc, 0xe5, 0x13, 0x0a, 0xe5, 0xf4, 0x6b, 0x6d, 0x47, 0x88, 0x4a, 0xb6, 0x0b, 0x6d, 0x22, 0xf5, 0x5b, 0x0c, 0x0c, 0xfa, 0xcf, 0x14, 0xab, 0xe7, 0xea, 0x31, 0x18, 0xae}},
						Publishers:   [][32]uint8{{0x59, 0xd1, 0x27, 0xe5, 0xab, 0xbd, 0x5c, 0xe8, 0x8c, 0x1d, 0xe4, 0xab, 0xe7, 0x0b, 0x13, 0x2b, 0x4c, 0x79, 0xd4, 0xa1, 0xff, 0xe7, 0x81, 0x95, 0x2a, 0x8b, 0xdf, 0x13, 0x80, 0x1d, 0x2c, 0xb6}, {0x3a, 0x31, 0x6a, 0x45, 0x05, 0xa3, 0x9d, 0x60, 0x26, 0xa5, 0x5b, 0xf2, 0x89, 0x4e, 0x30, 0xba, 0xd3, 0x3b, 0xc1, 0x63, 0x1c, 0xe1, 0xbd, 0x92, 0x5f, 0x02, 0xab, 0x4c, 0x79, 0x94, 0xe9, 0xd4}},
						Subscribers:  [][32]uint8{{0x41, 0xc6, 0x96, 0x40, 0x53, 0xcf, 0x55, 0xd2, 0x92, 0x54, 0x72, 0xdb, 0xe0, 0x1a, 0xfb, 0xc3, 0x27, 0xf5, 0xab, 0xfd, 0xb9, 0x17, 0xec, 0x23, 0x4e, 0xca, 0xbc, 0x09, 0xe5, 0x29, 0x0b, 0x2b}, {0x3a, 0x31, 0x6a, 0x45, 0x05, 0xa3, 0x9d, 0x60, 0x26, 0xa5, 0x5b, 0xf2, 0x89, 0x4e, 0x30, 0xba, 0xd3, 0x3b, 0xc1, 0x63, 0x1c, 0xe1, 0xbd, 0x92, 0x5f, 0x02, 0xab, 0x4c, 0x79, 0x94, 0xe9, 0xd4}},
						PubKey: [32]uint8{
							0xb7, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
							0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
					},
				},
			},
		},
	}

	t.Log("Start testing")
	for _, test := range tests {
		t.Log(test.Name)

		client := &Client{client: &mockSolanaClient{payload: test.Payload}}
		if err := client.Load(context.Background()); err != nil {
			t.Fatalf("error while loading data: %v", err)
		}
		t.Run(test.Name, func(t *testing.T) {
			if diff := cmp.Diff(test.Want, client, cmpopts.IgnoreUnexported(Client{})); diff != "" {
				t.Fatalf("Client diff found; -want, +got: %s", diff)
			}
		})

	}
}

func TestNewClient(t *testing.T) {
	t.Run("test_default_program_id", func(t *testing.T) {
		client := New("endpoint")
		want := solana.MustPublicKeyFromBase58(PROGRAM_ID_TESTNET)
		if client.pubkey != want {
			t.Fatalf("default client pubkey incorrect; got %s, wanted %s", client.pubkey, want)
		}
	})

	t.Run("test_override_program_id", func(t *testing.T) {
		programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
		client := New("endpoint", WithProgramId(programId))
		want := solana.MustPublicKeyFromBase58(programId)
		if client.pubkey != want {
			t.Fatalf("overridden client pubkey incorrect; got %s, wanted %s", client.pubkey, want)
		}
	})

}
