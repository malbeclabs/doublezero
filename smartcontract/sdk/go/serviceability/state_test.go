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
				LinkType:          serviceability.LinkLinkTypeMPLSoverGRE,
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
				PubKey:            dummyPubKey,
			},
			expected: `{
				"AccountType": 6,
				"Owner": "` + dummyPubKeyB58 + `",
				"Index": {"High":0,"Low":1},
				"Bump_seed": 255,
				"SideAPubKey": "` + dummyPubKeyB58 + `",
				"SideZPubKey": "` + dummyPubKeyB58 + `",
				"LinkType": 1,
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
				"LinkType": 0,
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
						Version:            serviceability.CurrentInterfaceVersion,
						Status:             serviceability.InterfaceStatusActivated,
						Name:               "Switch1/1/1",
						InterfaceType:      serviceability.InterfaceTypePhysical,
						VlanId:             100,
						IpNet:              [5]uint8{192, 168, 100, 1, 24},
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
				AccountType:  serviceability.UserType,
				Owner:        dummyPubKey,
				Index:        serviceability.Uint128{High: 0, Low: 3},
				Bump_seed:    253,
				UserType:     serviceability.UserTypeIBRL,
				TenantPubKey: dummyPubKey,
				DevicePubKey: dummyPubKey,
				CyoaType:     serviceability.CyoaTypeGREOverDIA,
				ClientIp:     [4]uint8{192, 168, 1, 20},
				DzIp:         [4]uint8{10, 10, 0, 2},
				TunnelId:     102,
				TunnelNet:    [5]uint8{172, 16, 1, 0, 30},
				Status:       serviceability.UserStatusActivated,
				Publishers:   [][32]uint8{dummyPubKey},
				Subscribers:  [][32]uint8{dummyPubKey},
				PubKey:       dummyPubKey,
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
