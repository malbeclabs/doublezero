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
020a3b74b3535cdeb34fd5e4cd7ea11
33e55abc521c8850f6d08166d11e482
89784cfe0000e9fd0000ac10000010a
9fe0000100be181db23854a6598b9a4
75753f210bc4f6e0bfeaf65125ab154
9aa9a132d27
`

var locationPayload = `
030a3b74b3535cdeb34fd5e4cd7ea11
33e55abc521c8850f6d08166d11e482
8978070000000000000000000000000
000002c10d8adc0394440784baca051
0054c000000000010200000070690a0
0000050697474736275726768020000
0055535c6804f55866ea7efca9377af
0e077fd8849826ffd91c22cf8b42917
9b9be0b7
`

var exchangePayload = `
040a3b74b3535cdeb34fd5e4cd7ea11
33e55abc521c8850f6d08166d11e482
89780a0000000000000000000000000
00000f808debecac14940e4bdb30cff
c1bebf000000000103000000786c640
60000004c6f6e646f6e35328ae45eec
91a7e0e53030b9fc5c4a480d51d9b87
c274b256290d40fdf914c
`

var devicePayload = `
050a3b74b3535cdeb34fd5e4cd7ea11
33e55abc521c8850f6d08166d11e482
89780f0000000000000000000000000
0000028ae0a8eea9b48bff4a072e277
9ab5f6c7025f71c22e20cd763f109c2
7de0021895a3281cdcdf5d46ffb7a58
c2af8f72fc20b6b06ef75e756171138
c0f66e13a00cf2dd888cf2dd8881d01
080000006c61322d647a30317830543
f55840f9fa6f04948c0b2708d022b51
c46f40772f345369cbb6e71889
`

var tunnelPayload = `
060a3b74b3535cdeb34fd5e4cd7ea11
33e55abc521c8850f6d08166d11e482
8978180000000000000000000000000
000003caf7d29208fd201a140df6e9c
463448e310b050ce66f55e9d290a472
09627034b5f47695c4f6e3f087d7c44
29eef8cd2cfaea3e823e43c40a49ae5
bf0759aff0100e40b54020000002823
000040787d010000000080969800000
000000200ac1000021f01110000006c
64342d647a30313a66726b2d647a303
19469c73fef7f054a311597628e2a82
646315badda4f4af612421cd24dff15
671
`

var userPayload = `
070a3b74b3535cdeb34fd5e4cd7ea1
133e55abc521c8850f6d08166d11e4
8289781c0000000000000000000000
00000000014e87bc43fa1a87570633
1a9365bcece0273faca423ea084280
aba8289426385a0191284e70cc10f3
f3f401a9fe00001f01fd7b849cb615
09e9f63cb3081ab75c836c800c5bfb
52bfcf3f4801719751291a
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
			Pubkey: solana.MustPublicKeyFromBase58(PROGRAM_ID),
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
					AccountType:       ConfigType,
					Owner:             getOwner(configPayload),
					Local_asn:         65100,
					Remote_asn:        65001,
					TunnelTunnelBlock: [5]byte{172, 16, 0, 0, 16},
					UserTunnelBlock:   [5]byte{169, 254, 0, 0, 16},
					PubKey:            getPubKey(configPayload),
				},
				Locations: []Location{},
				Devices:   []Device{},
				Tunnels:   []Tunnel{},
				Users:     []User{},
				Exchanges: []Exchange{},
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
						Index:       Uint128{High: 10, Low: 0},
						Owner:       getOwner(exchangePayload),
						Lat:         51.513999803939384,
						Lng:         -0.12014764843092213,
						LocId:       0,
						Status:      1,
						Code:        "xld",
						Name:        "London",
						PubKey:      getPubKey(exchangePayload),
					},
				},
				Locations: []Location{},
				Devices:   []Device{},
				Tunnels:   []Tunnel{},
				Users:     []User{},
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
						Index:          Uint128{High: 15, Low: 0},
						Owner:          getOwner(exchangePayload),
						TenantPubKey:   getPubKeyOffset(devicePayload, 49, 81),
						LocationPubKey: getPubKeyOffset(devicePayload, 49, 81),
						ExchangePubKey: getPubKeyOffset(devicePayload, 81, 113),
						DeviceType:     0,
						PublicIp:       [4]byte{0xcf, 0x2d, 0xd8, 0x88},
						DzPrefixes:     [][5]byte{{0xcf, 0x2d, 0xd8, 0x88, 0x1d}},
						Status:         1,
						Code:           "la2-dz01",
						PubKey:         getPubKey(devicePayload),
					},
				},
				Locations: []Location{},
				Exchanges: []Exchange{},
				Tunnels:   []Tunnel{},
				Users:     []User{},
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
						Index:       Uint128{High: 7, Low: 0},
						Owner:       getOwner(locationPayload),
						Lat:         40.45119259881935,
						Lng:         -80.00498215509094,
						LocId:       0,
						Status:      1,
						Code:        "pi",
						Name:        "Pittsburgh",
						Country:     "US",
						PubKey:      getPubKey(locationPayload),
					},
				},
				Exchanges: []Exchange{},
				Devices:   []Device{},
				Tunnels:   []Tunnel{},
				Users:     []User{},
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
						Index:        Uint128{High: 28, Low: 0},
						Owner:        getOwner(userPayload),
						UserType:     UserTypeServer,
						DevicePubKey: getPubKeyOffset(userPayload, 50, 82),
						CyoaType:     CyoaTypeGREOverDIA,
						ClientIp:     [4]byte{0x91, 0x28, 0x4e, 0x70},
						TunnelId:     500,
						TunnelNet:    [5]byte{0xa9, 0xfe, 0x00, 0x00, 0x1f},
						DzIp:         [4]byte{0xcc, 0x10, 0xf3, 0xf3},
						Status:       UserStatusActivated,
						PubKey:       getPubKey(userPayload),
					},
				},
				Locations: []Location{},
				Devices:   []Device{},
				Tunnels:   []Tunnel{},
				Exchanges: []Exchange{},
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
						Index:       Uint128{High: 24, Low: 0},
						Owner:       getOwner(tunnelPayload),
						SideAPubKey: getPubKeyOffset(tunnelPayload, 49, 81),
						SideZPubKey: getPubKeyOffset(tunnelPayload, 81, 113),
						TunnelType:  TunnelTunnelTypeMPLSoverGRE,
						Bandwidth:   10000000000,
						Mtu:         9000,
						DelayNs:     25000000,
						JitterNs:    10000000,
						TunnelId:    2,
						TunnelNet:   [5]byte{0xac, 0x10, 0x00, 0x02, 0x1f},
						Status:      TunnelStatusActivated,
						Code:        "ld4-dz01:frk-dz01",
						PubKey:      getPubKey(tunnelPayload),
					},
				},
				Locations: []Location{},
				Devices:   []Device{},
				Exchanges: []Exchange{},
				Users:     []User{},
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
		want := solana.MustPublicKeyFromBase58(PROGRAM_ID)
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
