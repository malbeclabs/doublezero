package serviceability

import (
	"fmt"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

func TestToLineProtocol(t *testing.T) {
	t.Parallel()

	var pubKey1 [32]byte
	copy(pubKey1[:], "11111111111111111111111111111111")
	pubKey1B58 := base58.Encode(pubKey1[:])
	var pubKey2 [32]byte
	copy(pubKey2[:], "22222222222222222222222222222222")
	pubKey2B58 := "C2n2b2n2b2n2b2n2b2n2b2n2b2n2b2n2b2n2b2n2b"

	var pubKey1Uint8 [32]uint8
	copy(pubKey1Uint8[:], pubKey1[:])

	ts := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	tsNano := ts.UnixNano()

	type testDevice struct {
		Owner      [32]byte     `influx:"tag,owner,pubkey"`
		DeviceType uint8        `influx:"tag,device_type"`
		PublicIp   [4]uint8     `influx:"tag,public_ip,ip"`
		Status     DeviceStatus `influx:"tag,status"`
		Code       string       `influx:"tag,code"`
		DzPrefixes [][5]uint8   `influx:"field,dz_prefixes,cidr"`
		UsersCount uint16       `influx:"field,users_count"`
		MaxUsers   uint16       `influx:"field,max_users"`
		Ignored    string       `influx:"-"`
		NoTag      string
	}

	type testContributor struct {
		Owner   [32]byte          `influx:"tag,owner,pubkey"`
		Status  ContributorStatus `influx:"tag,status"`
		Code    string            `influx:"tag,code"`
		Name    string            `influx:"tag,name"`
		Ignored string            `influx:"-"`
		NoTag   string
	}

	type testExchange struct {
		Owner   [32]byte       `influx:"tag,owner,pubkey"`
		Lat     float64        `influx:"field,lat"`
		Lng     float64        `influx:"field,lng"`
		Status  ExchangeStatus `influx:"tag,status"`
		Code    string         `influx:"tag,code"`
		Name    string         `influx:"tag,name"`
		Ignored string         `influx:"-"`
		NoTag   string
	}

	testCases := []struct {
		name           string
		measurement    string
		input          any
		ts             time.Time
		additionalTags map[string]string
		expected       string
		expectErr      bool
	}{
		{
			name:        "full device struct",
			measurement: "devices",
			input: testDevice{
				Owner:      pubKey1,
				DeviceType: 1,
				PublicIp:   [4]uint8{192, 168, 1, 1},
				Status:     DeviceStatusActivated,
				Code:       "dev-01",
				DzPrefixes: [][5]uint8{{10, 0, 0, 0, 16}, {10, 1, 0, 0, 16}},
				UsersCount: 5,
				MaxUsers:   100,
				Ignored:    "should be ignored",
				NoTag:      "should be ignored",
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected:  `devices,code=dev-01,device_type=1,env=testnet,owner=` + pubKey1B58 + `,public_ip=192.168.1.1,status=activated dz_prefixes="10.0.0.0/16,10.1.0.0/16",max_users=100,users_count=5`,
			expectErr: false,
		},
		{
			name:        "full contributor struct",
			measurement: "contributors",
			input: testContributor{
				Owner:   pubKey1,
				Status:  ContributorStatusActivated,
				Code:    "dev-01",
				Name:    "test-contributor",
				Ignored: "should be ignored",
				NoTag:   "should be ignored",
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected:  `contributors,code=dev-01,env=testnet,name=test-contributor,owner=` + pubKey1B58 + `,status=activated `,
			expectErr: false,
		},
		{
			name:        "full exchange struct",
			measurement: "exchanges",
			input: testExchange{
				Owner:   pubKey1,
				Lat:     10.0,
				Lng:     20.0,
				Status:  ExchangeStatusActivated,
				Code:    "dev-01",
				Name:    "test-exchange",
				Ignored: "should be ignored",
				NoTag:   "should be ignored",
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected:  `exchanges,code=dev-01,env=testnet,name=test-exchange,owner=` + pubKey1B58 + `,status=activated lat=10,lng=20`,
			expectErr: false,
		},
		{
			name:        "full link struct",
			measurement: "links",
			input: Link{
				Owner:             pubKey1Uint8,
				SideAPubKey:       pubKey1Uint8,
				SideZPubKey:       pubKey1Uint8,
				LinkType:          LinkLinkTypeWAN,
				Bandwidth:         1000,
				Mtu:               1500,
				DelayNs:           1000000,
				JitterNs:          500000,
				TunnelId:          42,
				TunnelNet:         [5]uint8{172, 16, 0, 0, 24},
				Status:            LinkStatusActivated,
				Code:              "link-01",
				ContributorPubKey: pubKey1Uint8,
				SideAIfaceName:    "xe-0/0/0",
				SideZIfaceName:    "xe-0/0/1",
				DelayOverrideNs:   2000000,
				LinkHealth:        LinkHealthPending,
				PubKey:            pubKey1,
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected: `links,code=link-01,contributor_pubkey=` + pubKey1B58 +
				`,env=testnet,link_desired_status=pending,link_type=WAN,owner=` + pubKey1B58 +
				`,pubkey=` + pubKey1B58 +
				`,side_a_iface_name=xe-0/0/0,side_a_pubkey=` + pubKey1B58 +
				`,side_z_iface_name=xe-0/0/1,side_z_pubkey=` + pubKey1B58 +
				`,status=activated,tunnel_id=42,tunnel_net=172.16.0.0/24 bandwidth=1000,delay_ns=1e+06,delay_override_ns=2e+06,jitter_ns=500000,link_health="pending",mtu=1500`,
			expectErr: false,
		},
		{
			name:        "link with empty optional tags",
			measurement: "links",
			input: Link{
				Owner:             pubKey1Uint8,
				SideAPubKey:       pubKey1Uint8,
				SideZPubKey:       pubKey1Uint8,
				LinkType:          LinkLinkTypeDZX,
				Bandwidth:         0,
				Mtu:               0,
				DelayNs:           0,
				JitterNs:          0,
				TunnelId:          7,
				Status:            LinkStatusPending,
				Code:              "link-empty",
				ContributorPubKey: pubKey1Uint8,
				DelayOverrideNs:   0,
				LinkHealth:        LinkHealthPending,
				PubKey:            pubKey1,
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected: `links,code=link-empty,contributor_pubkey=` + pubKey1B58 +
				`,env=testnet,link_desired_status=pending,link_type=DZX,owner=` + pubKey1B58 +
				`,pubkey=` + pubKey1B58 +
				`,side_a_pubkey=` + pubKey1B58 +
				`,side_z_pubkey=` + pubKey1B58 +
				`,status=pending,tunnel_id=7 bandwidth=0,delay_ns=0,delay_override_ns=0,jitter_ns=0,link_health="pending",mtu=0`,
			expectErr: false,
		},
		{
			name:        "empty struct",
			measurement: "devices",
			input:       testDevice{},
			ts:          ts,
			expected:    `devices,device_type=0,owner=11111111111111111111111111111111,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0`,
			expectErr:   false,
		},
		{
			name:        "additional tags override struct tags",
			measurement: "devices",
			input: testDevice{
				Owner:      pubKey1,
				Code:       "dev-01",
				DeviceType: 1,
			},
			ts: ts,
			additionalTags: map[string]string{
				"owner":       pubKey2B58,
				"env":         "mainnet",
				"device_type": "2", // override a numeric tag
			},
			expected:  `devices,code=dev-01,device_type=2,env=mainnet,owner=` + pubKey2B58 + `,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0`,
			expectErr: false,
		},
		{
			name:        "no tags",
			measurement: "devices",
			input: struct {
				Field1 int `influx:"field,field1"`
			}{Field1: 42},
			ts:       ts,
			expected: `devices, field1=42`,
		},
		{
			name:        "no fields",
			measurement: "devices",
			input: struct {
				Tag1 string `influx:"tag,tag1"`
			}{Tag1: "value1"},
			ts:       ts,
			expected: `devices,tag1=value1 `,
		},
		{
			name:        "input is not a struct",
			measurement: "test",
			input:       "not a struct",
			ts:          ts,
			expectErr:   true,
		},
		{
			name:        "empty measurement",
			measurement: "",
			input:       testDevice{},
			ts:          ts,
			expectErr:   true,
		},
		{
			name:        "empty tag",
			measurement: "contributors",
			input: testContributor{
				Owner:   pubKey1,
				Status:  ContributorStatusActivated,
				Code:    "dev-01",
				Name:    "",
				Ignored: "should be ignored",
				NoTag:   "should be ignored",
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected:  `contributors,code=dev-01,env=testnet,owner=` + pubKey1B58 + `,status=activated `,
			expectErr: false,
		},
		{
			name:        "field with spaces",
			measurement: "exchanges",
			input: testExchange{
				Owner:   pubKey1,
				Lat:     10.0,
				Lng:     20.0,
				Status:  ExchangeStatusActivated,
				Code:    "dev-01",
				Name:    "test exchange",
				Ignored: "should be ignored",
				NoTag:   "should be ignored",
			},
			ts: ts,
			additionalTags: map[string]string{
				"env": "testnet",
			},
			expected:  `exchanges,code=dev-01,env=testnet,name=test\ exchange,owner=` + pubKey1B58 + `,status=activated lat=10,lng=20`,
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var expected string
			if !tc.expectErr {
				expected = tc.expected + " " + fmt.Sprintf("%d", tsNano)
			}

			line, err := ToLineProtocol(tc.measurement, tc.input, tc.ts, tc.additionalTags)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, expected, line)
			}
		})
	}
}
