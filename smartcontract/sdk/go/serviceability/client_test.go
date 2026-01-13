package serviceability

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/go-cmp/cmp"
)

var configPayload = `
02baae1ce3bce5130ae5f46b6d47884ab60b6d22f55b0c0cfac
f14abe7ea3118aefd4cfe0000e9fd0000ac10000010a9fe0000
10df00000004a2aa7d81b23bd270048af6aae3813dea
`

var locationPayload = `
030a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e482897806000000000000000000000000000000fea2
e3b2a599d54140b03f0a3a80786140000000000103000000747
96f05000000546f6b796f020000004a5065483c031c496dd52f
fd841907413a92
`

var exchangePayload = `
040a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e48289780c000000000000000000000000000000ff35
71de7a8e0f494029845566ba482140000000000104000000786
67261090000004672616e6b6675727405050505111111111111
111111111111111111111111111111111111111111111111111
122222222222222222222222222222222222222222222222222
22222222222222
`

var devicePayload = `
050a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d08
166d11e482897816000000000000000000000000000000ff0000
0000000000080000000000000000000000000000000000000000
0000000000000000000000090000000000000000000000000000
0000000000000000000000b4579a7001080000007479322d647a
303101000000b4579a701d000000000000001a00000000000000
0000000000000000000000000000000000000000000000000300
0000000000000000000000000000000000000000000000070000
0064656661756c740200000000020b000000737769746368312f
312f3102002a000a0102031d7b00000002030000006c6f300101
0f000a0203041d2a0001d20400006e008000
`

var tunnelPayload = `
060a3b74b3535cdeb34fd5e4cd7ea1133e55abc521c8850f6d0
8166d11e48289781e000000000000000000000000000000fb90
3a23e92446591b0bb98794f3e278aeafc84fd20ad064acb8cc2
f8198607689246e25c9403fba46e89122ff5d0fcc1febb51d4b
4ce64f17ad56c47b3d1d7f3f0100e40b5402000000282300008
0c3c9010000000080969800000000000500ac10000a1f011100
00007479322d647a30313a6c61322d647a30310001020304050
60708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
0b000000737769746368312f312f31030000006c6f30ffc99a3
b00000000ad2570a0cf27761cab55a3f26d85fb20
`

var userPayload = `
07baae1ce3bce5130ae5f46b6d47884ab60b6d22f55b0c0cfac
f14abe7ea3118ae1f000000000000000000000000000000fc00
000000000000000000000000000000000000000000000000000
0000000000000d2b30c6593b3dd99bbdde9c8e29eb9291adefb
c11544a47f17d9472cae13fdfc010a0000010a000001f401a9f
e00001f010000000000000000fcef68d5d9eae991fd7d6284da
d2f2d7
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
4c7994e9d4b745f92183e1b409bb7006560f858cf3
`

var programconfigPayload = `
09ff010000000200000003000000
`

type mockSolanaClient struct {
	payload     string
	pubkey      solana.PublicKey
	returnEmpty bool
}

func (m *mockSolanaClient) GetProgramAccounts(context.Context, solana.PublicKey) (rpc.GetProgramAccountsResult, error) {
	if m.returnEmpty {
		return []*rpc.KeyedAccount{}, nil
	}
	data, err := hex.DecodeString(strings.ReplaceAll(m.payload, "\n", ""))
	if err != nil {
		return nil, err
	}
	return []*rpc.KeyedAccount{
		{
			Pubkey: m.pubkey,
			Account: &rpc.Account{
				Data: rpc.DataBytesOrJSONFromBytes(data),
			},
		},
	}, nil
}

func getOwner(payload string) [32]byte {
	return getPubKeyOffset(payload, 1, 33)
}

func getPubKeyOffset(payload string, start, end int) [32]byte {
	var d [32]byte
	p, _ := hex.DecodeString(strings.ReplaceAll(payload, "\n", ""))
	copy(d[:], p[start:end])
	return d
}

func TestSDK_Serviceability_GetProgramData(t *testing.T) {
	pubkeys := [][32]uint8{
		{0xb0, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb1, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb2, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb3, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb4, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb5, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb6, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
		{0xb7, 0x45, 0xf9, 0x21, 0x83, 0xe1, 0xb4, 0x09, 0xbb, 0x70, 0x06, 0x56, 0x0f, 0x85, 0x8c, 0xf3,
			0xbf, 0xa5, 0x57, 0xc7, 0x5c, 0xd9, 0x67, 0x18, 0x2a, 0x00, 0x39, 0x22, 0x00, 0xb5, 0xde, 0x78},
	}
	tests := []struct {
		Name        string
		Description string
		Payload     string
		Want        *ProgramData
	}{

		{
			Name:        "parse_valid_config",
			Description: "parse and populate a valid config struct",
			Payload:     strings.TrimSuffix(configPayload, "\n"),
			Want: &ProgramData{
				Config: Config{
					AccountType:         ConfigType,
					Owner:               getOwner(configPayload),
					Bump_seed:           253,
					Local_asn:           65100,
					Remote_asn:          65001,
					TunnelTunnelBlock:   [5]byte{172, 16, 0, 0, 16},
					UserTunnelBlock:     [5]byte{169, 254, 0, 0, 16},
					MulticastGroupBlock: [5]byte{223, 0, 0, 0, 4},
					PubKey:              pubkeys[0],
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Links:           []Link{},
				Users:           []User{},
				Exchanges:       []Exchange{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_exchange",
			Description: "parse and populate a valid exchange struct",
			Payload:     strings.TrimSuffix(exchangePayload, "\n"),
			Want: &ProgramData{
				Exchanges: []Exchange{
					{
						AccountType:  ExchangeType,
						Index:        Uint128{High: 12, Low: 0},
						Bump_seed:    255,
						Owner:        getOwner(exchangePayload),
						Lat:          50.1215356432098,
						Lng:          8.642047117175098,
						BgpCommunity: 0,
						Status:       1,
						Code:         "xfra",
						Name:         "Frankfurt",
						PubKey:       pubkeys[1],
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Links:           []Link{},
				Users:           []User{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_device",
			Description: "parse and populate a valid device struct",
			Payload:     strings.TrimSuffix(devicePayload, "\n"),
			Want: &ProgramData{
				Devices: []Device{
					{
						AccountType:            DeviceType,
						Index:                  Uint128{High: 22, Low: 0},
						Bump_seed:              255,
						Owner:                  getOwner(exchangePayload),
						LocationPubKey:         getPubKeyOffset(devicePayload, 50, 82),
						ExchangePubKey:         getPubKeyOffset(devicePayload, 82, 114),
						DeviceType:             0,
						PublicIp:               [4]byte{0xb4, 0x57, 0x9a, 0x70},
						Status:                 1,
						Code:                   "ty2-dz01",
						DzPrefixes:             [][5]byte{{0xb4, 0x57, 0x9a, 0x70, 0x1d}},
						MetricsPublisherPubKey: getPubKeyOffset(devicePayload, 141, 173),
						ContributorPubKey:      getPubKeyOffset(devicePayload, 173, 205),
						MgmtVrf:                "default",
						Interfaces: []Interface{
							{
								Version:            0,
								Status:             InterfaceStatusPending,
								Name:               "switch1/1/1",
								InterfaceType:      InterfaceTypePhysical,
								LoopbackType:       LoopbackTypeNone,
								VlanId:             42,
								IpNet:              [5]byte{0x0a, 0x01, 0x02, 0x03, 0x1d},
								NodeSegmentIdx:     123,
								UserTunnelEndpoint: false,
							},
							{
								Version:            0,
								Status:             InterfaceStatusPending,
								Name:               "lo0",
								InterfaceType:      InterfaceTypeLoopback,
								LoopbackType:       LoopbackTypeVpnv4,
								VlanId:             15,
								IpNet:              [5]byte{0x0a, 0x02, 0x03, 0x04, 0x1d},
								NodeSegmentIdx:     42,
								UserTunnelEndpoint: true,
							},
						},
						ReferenceCount: 1234,
						UsersCount:     110,
						MaxUsers:       128,
						PubKey:         pubkeys[2],
					},
				},
				Locations:       []Location{},
				Exchanges:       []Exchange{},
				Links:           []Link{},
				Users:           []User{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_location",
			Description: "parse and populate a valid location struct",
			Payload:     strings.TrimSuffix(locationPayload, "\n"),
			Want: &ProgramData{
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
						PubKey:      pubkeys[3],
					},
				},
				Exchanges:       []Exchange{},
				Devices:         []Device{},
				Links:           []Link{},
				Users:           []User{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_user",
			Description: "parse and populate a valid user struct",
			Payload:     strings.TrimSuffix(userPayload, "\n"),
			Want: &ProgramData{
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
						PubKey:       pubkeys[4],
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Links:           []Link{},
				Exchanges:       []Exchange{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_link",
			Description: "parse and populate a valid link struct",
			Payload:     strings.TrimSuffix(tunnelPayload, "\n"),
			Want: &ProgramData{
				Links: []Link{
					{
						AccountType:       LinkType,
						Index:             Uint128{High: 30, Low: 0},
						Bump_seed:         251,
						Owner:             getOwner(tunnelPayload),
						SideAPubKey:       getPubKeyOffset(tunnelPayload, 50, 82),
						SideZPubKey:       getPubKeyOffset(tunnelPayload, 82, 114),
						LinkType:          LinkLinkTypeWAN,
						Bandwidth:         10000000000,
						Mtu:               9000,
						DelayNs:           30000000,
						JitterNs:          10000000,
						DelayOverrideNs:   999999999,
						TunnelId:          5,
						TunnelNet:         [5]byte{0xac, 0x10, 0x00, 0x0a, 0x1f},
						Status:            LinkStatusActivated,
						Code:              "ty2-dz01:la2-dz01",
						ContributorPubKey: [32]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f},
						SideAIfaceName:    "switch1/1/1",
						SideZIfaceName:    "lo0",
						PubKey:            pubkeys[5],
					},
				},
				Locations:       []Location{},
				Devices:         []Device{},
				Exchanges:       []Exchange{},
				Users:           []User{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig:   ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_multicastgroup",
			Description: "parse and populate a valid multicastgroup struct",
			Payload:     strings.TrimSuffix(multicastgroupPayload, "\n"),
			Want: &ProgramData{
				Links:        []Link{},
				Locations:    []Location{},
				Devices:      []Device{},
				Exchanges:    []Exchange{},
				Users:        []User{},
				Contributors: []Contributor{},
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
						Code:         "jito",
						PubKey:       pubkeys[6],
					},
				},
				ProgramConfig: ProgramConfig{},
			},
		},
		{
			Name:        "parse_valid_programconfig",
			Description: "parse and populate a valid programconfig struct",
			Payload:     strings.TrimSuffix(programconfigPayload, "\n"),
			Want: &ProgramData{
				Links:           []Link{},
				Locations:       []Location{},
				Devices:         []Device{},
				Exchanges:       []Exchange{},
				Users:           []User{},
				Contributors:    []Contributor{},
				MulticastGroups: []MulticastGroup{},
				ProgramConfig: ProgramConfig{
					AccountType: ProgramConfigType,
					BumpSeed:    255,
					Version: ProgramVersion{
						Major: 1,
						Minor: 2,
						Patch: 3,
					},
				},
			},
		},
	}

	for idx, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			client := &Client{rpc: &mockSolanaClient{payload: test.Payload, pubkey: pubkeys[idx]}}
			got, err := client.GetProgramData(t.Context())
			if err != nil {
				t.Fatalf("error while loading data: %v", err)
			}
			if diff := cmp.Diff(test.Want, got); diff != "" {
				t.Fatalf("Client diff found; -want, +got: %s", diff)
			}
		})

	}
}

func TestSDK_Serviceability_GetProgramData_EmptyResult(t *testing.T) {
	programID := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
	client := &Client{
		rpc:       &mockSolanaClient{returnEmpty: true},
		programID: programID,
	}

	_, err := client.GetProgramData(t.Context())
	if err == nil {
		t.Fatal("expected error for empty GetProgramAccounts result, got nil")
	}

	expectedErrSubstring := "GetProgramAccounts returned empty result"
	if !strings.Contains(err.Error(), expectedErrSubstring) {
		t.Fatalf("expected error to contain %q, got: %v", expectedErrSubstring, err)
	}
}
