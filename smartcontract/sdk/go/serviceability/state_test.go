package serviceability_test

import (
	"encoding/json"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomJSONMarshal(t *testing.T) {
	// Create a single dummy public key to be reused across tests.
	var dummyPubKey [32]byte
	for i := 0; i < 32; i++ {
		dummyPubKey[i] = '1'
	}
	// The expected base58 string for the dummy key.
	const dummyPubKeyB58 = "4K2V1kpVycZ6qSFsNdz2FtpNxnJs17eBNzf9rdCMcKoe"

	testCases := []struct {
		name      string
		input     any
		expected  string
		expectErr bool
	}{
		{
			name: "link struct with valid data",
			input: &serviceability.Link{
				AccountType:       serviceability.LinkType,
				Owner:             dummyPubKey,
				Index:             serviceability.Uint128{High: 0, Low: 1},
				Bump_seed:         255,
				SideAPubKey:       dummyPubKey,
				SideZPubKey:       dummyPubKey,
				LinkType:          serviceability.LinkLinkTypeWAN,
				Bandwidth:         1000000000, // 1 Gbps
				Mtu:               1500,
				DelayNs:           5000000, // 5ms
				JitterNs:          1000000, // 1ms
				TunnelId:          101,
				TunnelNet:         [5]uint8{192, 168, 1, 10, 24}, // IP: 192.168.1.10, Prefix: 24
				Status:            serviceability.LinkStatusActivated,
				Code:              "link-01",
				ContributorPubKey: dummyPubKey,
				SideAIfaceName:    "Switch1/1/1",
				SideZIfaceName:    "Switch1/1/1",
				DelayOverrideNs:   10,
				PubKey:            dummyPubKey,
			},
			expected: `{
				"AccountType": 6,
				"Owner": "` + dummyPubKeyB58 + `",
				"Index": {"High":0,"Low":1},
				"Bump_seed": 255,
				"SideAPubKey": "` + dummyPubKeyB58 + `",
				"SideZPubKey": "` + dummyPubKeyB58 + `",
				"LinkDesiredStatus":"pending",
				"LinkHealth":"pending",
				"LinkType": "WAN",
				"Bandwidth": 1000000000,
				"Mtu": 1500,
				"DelayNs": 5000000,
				"JitterNs": 1000000,
				"TunnelId": 101,
				"TunnelNet": "192.168.1.10/24",
				"Status": "activated",
				"Code": "link-01",
				"ContributorPubKey": "` + dummyPubKeyB58 + `",
				"SideAIfaceName": "Switch1/1/1",
				"SideZIfaceName": "Switch1/1/1",
				"DelayOverrideNs": 10,
				"PubKey": "` + dummyPubKeyB58 + `"
			}`,
			expectErr: false,
		},
		{
			name: "link struct with DZX type and soft drained status",
			input: &serviceability.Link{
				AccountType:       serviceability.LinkType,
				Owner:             dummyPubKey,
				Index:             serviceability.Uint128{High: 1, Low: 2},
				Bump_seed:         1,
				SideAPubKey:       dummyPubKey,
				SideZPubKey:       dummyPubKey,
				LinkType:          serviceability.LinkLinkTypeDZX,
				Bandwidth:         2000000000,
				Mtu:               9000,
				DelayNs:           1000000,
				JitterNs:          200000,
				TunnelId:          202,
				TunnelNet:         [5]uint8{10, 0, 0, 0, 16}, // 10.0.0.0/16
				Status:            serviceability.LinkStatusSoftDrained,
				Code:              "link-02",
				ContributorPubKey: dummyPubKey,
				SideAIfaceName:    "Edge1/0/0",
				SideZIfaceName:    "Edge2/0/0",
				DelayOverrideNs:   0,
				PubKey:            dummyPubKey,
			},
			expected: `{
				"AccountType": 6,
				"Owner": "` + dummyPubKeyB58 + `",
				"Index": {"High":1,"Low":2},
				"Bump_seed": 1,
				"SideAPubKey": "` + dummyPubKeyB58 + `",
				"SideZPubKey": "` + dummyPubKeyB58 + `",
				"LinkDesiredStatus":"pending",
				"LinkHealth":"pending",
				"LinkType": "DZX",
				"Bandwidth": 2000000000,
				"Mtu": 9000,
				"DelayNs": 1000000,
				"JitterNs": 200000,
				"TunnelId": 202,
				"TunnelNet": "10.0.0.0/16",
				"Status": "soft-drained",
				"Code": "link-02",
				"ContributorPubKey": "` + dummyPubKeyB58 + `",
				"SideAIfaceName": "Edge1/0/0",
				"SideZIfaceName": "Edge2/0/0",
				"DelayOverrideNs": 0,
				"PubKey": "` + dummyPubKeyB58 + `"
			}`,
			expectErr: false,
		},
		{
			name: "link struct with zero values and invalid TunnelNet",
			input: &serviceability.Link{
				TunnelNet: [5]uint8{10, 0, 0, 1, 0}, // Prefix 0 is invalid
			},
			expected: `{
				"AccountType": 0,
				"Owner": "11111111111111111111111111111111",
				"Index": {"High":0,"Low":0},
				"Bump_seed": 0,
				"SideAPubKey": "11111111111111111111111111111111",
				"SideZPubKey": "11111111111111111111111111111111",
				"LinkDesiredStatus":"pending",
				"LinkHealth":"pending",
				"LinkType": "",
				"Bandwidth": 0,
				"Mtu": 0,
				"DelayNs": 0,
				"JitterNs": 0,
				"TunnelId": 0,
				"TunnelNet": "",
				"Status": "pending",
				"Code": "",
				"ContributorPubKey": "11111111111111111111111111111111",
				"SideAIfaceName": "",
				"SideZIfaceName": "",
				"DelayOverrideNs": 0,
				"PubKey": "11111111111111111111111111111111"
			}`,
			expectErr: false,
		},
		{
			name: "device struct with valid data",
			input: &serviceability.Device{
				AccountType:            serviceability.DeviceType,
				Owner:                  dummyPubKey,
				Index:                  serviceability.Uint128{High: 0, Low: 2},
				Bump_seed:              254,
				LocationPubKey:         dummyPubKey,
				ExchangePubKey:         dummyPubKey,
				DeviceType:             1,
				PublicIp:               [4]uint8{8, 8, 8, 8},
				Status:                 serviceability.DeviceStatusActivated,
				Code:                   "device-01",
				DzPrefixes:             [][5]uint8{{10, 1, 0, 0, 16}, {10, 2, 0, 0, 16}},
				MetricsPublisherPubKey: dummyPubKey,
				ContributorPubKey:      dummyPubKey,
				MgmtVrf:                "mgmt-vrf",
				Interfaces: []serviceability.Interface{
					{
						Version:            0,
						Status:             serviceability.InterfaceStatusActivated,
						Name:               "Switch1/1/1",
						InterfaceType:      serviceability.InterfaceTypePhysical,
						LoopbackType:       serviceability.LoopbackTypeNone,
						VlanId:             100,
						IpNet:              [5]uint8{192, 168, 100, 1, 24},
						NodeSegmentIdx:     0,
						UserTunnelEndpoint: true,
					},
				},
				ReferenceCount: 5,
				UsersCount:     2,
				MaxUsers:       100,
				PubKey:         dummyPubKey,
			},
			expected: `{
                "AccountType": 5,
                "Owner": "` + dummyPubKeyB58 + `",
                "Index": {"High":0,"Low":2},
                "Bump_seed": 254,
                "LocationPubKey": "` + dummyPubKeyB58 + `",
                "ExchangePubKey": "` + dummyPubKeyB58 + `",
				"DeviceDesiredStatus": "pending",
				"DeviceHealth": "pending",
                "DeviceType": 1,
                "PublicIp": "8.8.8.8",
                "Status": "activated",
                "Code": "device-01",
                "DzPrefixes": ["10.1.0.0/16", "10.2.0.0/16"],
                "MetricsPublisherPubKey": "` + dummyPubKeyB58 + `",
                "ContributorPubKey": "` + dummyPubKeyB58 + `",
                "MgmtVrf": "mgmt-vrf",
                "Interfaces": [
                    {
                        "Version": 0,
                        "Status": "activated",
                        "Name": "Switch1/1/1",
                        "InterfaceType": "physical",
                        "LoopbackType": "none",
						"InterfaceCYOA": "none",
						"InterfaceDIA": "none",
						"Bandwidth": 0,
						"Cir": 0,
						"Mtu": 0,
						"RoutingMode": "static",
                        "VlanId": 100,
                        "IpNet": "192.168.100.1/24",
                        "NodeSegmentIdx": 0,
                        "UserTunnelEndpoint": true
                    }
                ],
                "ReferenceCount": 5,
                "UsersCount": 2,
                "MaxUsers": 100,
                "PubKey": "` + dummyPubKeyB58 + `"
            }`,
			expectErr: false,
		},
		{
			name: "device struct with valid data",
			input: &serviceability.Device{
				AccountType:            serviceability.DeviceType,
				Owner:                  dummyPubKey,
				Index:                  serviceability.Uint128{High: 0, Low: 2},
				Bump_seed:              254,
				LocationPubKey:         dummyPubKey,
				ExchangePubKey:         dummyPubKey,
				DeviceType:             1,
				PublicIp:               [4]uint8{8, 8, 8, 8},
				Status:                 serviceability.DeviceStatusActivated,
				Code:                   "device-01",
				DzPrefixes:             [][5]uint8{{10, 1, 0, 0, 16}, {10, 2, 0, 0, 16}},
				MetricsPublisherPubKey: dummyPubKey,
				ContributorPubKey:      dummyPubKey,
				MgmtVrf:                "mgmt-vrf",
				Interfaces: []serviceability.Interface{
					{
						Version:            serviceability.CurrentInterfaceVersion - 1,
						Status:             serviceability.InterfaceStatusActivated,
						Name:               "Switch1/1/1",
						InterfaceType:      serviceability.InterfaceTypePhysical,
						LoopbackType:       serviceability.LoopbackTypeNone,
						InterfaceCYOA:      serviceability.InterfaceCYOANone,
						InterfaceDIA:       serviceability.InterfaceDIANone,
						Bandwidth:          0,
						Cir:                0,
						Mtu:                0,
						RoutingMode:        serviceability.RoutingModeStatic,
						VlanId:             100,
						IpNet:              [5]uint8{192, 168, 100, 1, 24},
						NodeSegmentIdx:     0,
						UserTunnelEndpoint: true,
					},
				},
				ReferenceCount: 5,
				UsersCount:     2,
				MaxUsers:       100,
				PubKey:         dummyPubKey,
			},
			expected: `{
                "AccountType": 5,
                "Owner": "` + dummyPubKeyB58 + `",
                "Index": {"High":0,"Low":2},
                "Bump_seed": 254,
                "LocationPubKey": "` + dummyPubKeyB58 + `",
                "ExchangePubKey": "` + dummyPubKeyB58 + `",
				"DeviceDesiredStatus": "pending",
				"DeviceHealth": "pending",
                "DeviceType": 1,
                "PublicIp": "8.8.8.8",
                "Status": "activated",
                "Code": "device-01",
                "DzPrefixes": ["10.1.0.0/16", "10.2.0.0/16"],
                "MetricsPublisherPubKey": "` + dummyPubKeyB58 + `",
                "ContributorPubKey": "` + dummyPubKeyB58 + `",
                "MgmtVrf": "mgmt-vrf",
                "Interfaces": [
                    {
                        "Version": 1,
                        "Status": "activated",
                        "Name": "Switch1/1/1",
                        "InterfaceType": "physical",
                        "LoopbackType": "none",
						"InterfaceCYOA": "none",
						"InterfaceDIA": "none",
						"Bandwidth": 0,
						"Cir": 0,
						"Mtu": 0,
						"RoutingMode": "static",
                        "VlanId": 100,
                        "IpNet": "192.168.100.1/24",
                        "NodeSegmentIdx": 0,
                        "UserTunnelEndpoint": true
                    }
                ],
                "ReferenceCount": 5,
                "UsersCount": 2,
                "MaxUsers": 100,
                "PubKey": "` + dummyPubKeyB58 + `"
            }`,
			expectErr: false,
		},
		{
			name: "user struct with valid data",
			input: &serviceability.User{
				AccountType:     serviceability.UserType,
				Owner:           dummyPubKey,
				Index:           serviceability.Uint128{High: 0, Low: 3},
				Bump_seed:       253,
				UserType:        serviceability.UserTypeIBRL,
				TenantPubKey:    dummyPubKey,
				DevicePubKey:    dummyPubKey,
				CyoaType:        serviceability.CyoaTypeGREOverDIA,
				ClientIp:        [4]uint8{192, 168, 1, 20},
				DzIp:            [4]uint8{10, 10, 0, 2},
				TunnelId:        102,
				TunnelNet:       [5]uint8{172, 16, 1, 0, 30},
				Status:          serviceability.UserStatusActivated,
				Publishers:      [][32]uint8{dummyPubKey},
				Subscribers:     [][32]uint8{dummyPubKey},
				ValidatorPubKey: dummyPubKey,
				TunnelEndpoint:  [4]uint8{0, 0, 0, 0},
				PubKey:          dummyPubKey,
			},
			expected: `{
				"AccountType": 7,
				"Owner": "` + dummyPubKeyB58 + `",
				"Index": {"High":0,"Low":3},
				"Bump_seed": 253,
				"UserType": "ibrl",
				"TenantPubKey": "` + dummyPubKeyB58 + `",
				"DevicePubKey": "` + dummyPubKeyB58 + `",
				"CyoaType": "gre_over_dia",
				"ClientIp": "192.168.1.20",
				"DzIp": "10.10.0.2",
				"TunnelId": 102,
				"TunnelNet": "172.16.1.0/30",
				"Status": "activated",
				"Publishers": ["` + dummyPubKeyB58 + `"],
				"Subscribers": ["` + dummyPubKeyB58 + `"],
				"ValidatorPubKey": "` + dummyPubKeyB58 + `",
				"TunnelEndpoint": "0.0.0.0",
				"PubKey": "` + dummyPubKeyB58 + `"
			}`,
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualJSON, err := json.Marshal(tc.input)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.JSONEq(t, tc.expected, string(actualJSON), "The marshaled JSON should match the expected output.")
			}
		})
	}
}

// TestUserDeserializationOrder verifies that TunnelEndpoint is read just before PubKey.
//
// CRITICAL: The Rust User struct serializes fields in order via Borsh, with tunnel_endpoint
// as the LAST Borsh field. After deserialization, we append the account's public key (PubKey).
//
// The deserialization order in deserialize.go MUST be:
//  1. ... all other fields ...
//  2. ValidatorPubKey (32 bytes)
//  3. TunnelEndpoint (4 bytes) <- MUST be last Borsh field
//  4. PubKey (32 bytes) <- appended from account address, not part of Borsh
//
// If this test fails, either:
//   - Someone reordered fields in deserialize.go (fix the order)
//   - Someone added a field after tunnel_endpoint in Rust (add it BEFORE tunnel_endpoint instead)
func TestUserDeserializationOrder(t *testing.T) {
	// Create a minimal User payload with known values for TunnelEndpoint and PubKey
	// This is a hand-crafted byte sequence to verify deserialization order

	// Known values we'll verify after deserialization
	expectedTunnelEndpoint := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	expectedPubKey := [32]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20,
	}

	// Build the serialized User bytes
	// This matches the Rust struct field order exactly
	var buf []byte

	// account_type: u8 = 7 (UserType)
	buf = append(buf, 0x07)
	// owner: Pubkey (32 bytes)
	buf = append(buf, make([]byte, 32)...)
	// index: u128 (16 bytes)
	buf = append(buf, make([]byte, 16)...)
	// bump_seed: u8
	buf = append(buf, 0x01)
	// user_type: u8
	buf = append(buf, 0x00) // IBRL
	// tenant_pk: Pubkey (32 bytes)
	buf = append(buf, make([]byte, 32)...)
	// device_pk: Pubkey (32 bytes)
	buf = append(buf, make([]byte, 32)...)
	// cyoa_type: u8
	buf = append(buf, 0x01) // GREOverDIA
	// client_ip: [4]u8
	buf = append(buf, 0x0A, 0x00, 0x00, 0x01)
	// dz_ip: [4]u8
	buf = append(buf, 0x0A, 0x00, 0x00, 0x02)
	// tunnel_id: u16
	buf = append(buf, 0xF4, 0x01) // 500 in little-endian
	// tunnel_net: NetworkV4 (5 bytes: 4 for IP + 1 for prefix)
	buf = append(buf, 0xA9, 0xFE, 0x00, 0x00, 0x1F) // 169.254.0.0/31
	// status: u8
	buf = append(buf, 0x01) // Activated
	// publishers: Vec<Pubkey> (4 bytes length + data)
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // empty vec
	// subscribers: Vec<Pubkey> (4 bytes length + data)
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // empty vec
	// validator_pubkey: Pubkey (32 bytes)
	buf = append(buf, make([]byte, 32)...)
	// tunnel_endpoint: [4]u8 - THIS MUST BE THE LAST BORSH FIELD
	buf = append(buf, expectedTunnelEndpoint[:]...)
	// PubKey: [32]u8 - appended AFTER all Borsh fields
	buf = append(buf, expectedPubKey[:]...)

	// Deserialize
	reader := serviceability.NewByteReader(buf)
	var user serviceability.User
	serviceability.DeserializeUser(reader, &user)

	// Verify TunnelEndpoint was deserialized correctly
	assert.Equal(t, expectedTunnelEndpoint, user.TunnelEndpoint,
		"TunnelEndpoint should be deserialized from the 4 bytes before PubKey. "+
			"If this fails, check that tunnel_endpoint is the last Borsh field in Rust "+
			"and TunnelEndpoint is read just before PubKey in deserialize.go")

	// Verify PubKey was deserialized correctly (it should be the last 32 bytes)
	assert.Equal(t, expectedPubKey, user.PubKey,
		"PubKey should be the last 32 bytes after all Borsh fields. "+
			"If this fails, a field may have been added after tunnel_endpoint in Rust.")
}
