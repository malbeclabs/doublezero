package serviceability

import (
	"fmt"
	"strings"
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
			expected:  `devices,code=dev-01,device_type=1,env=testnet,owner=` + pubKey1B58 + `,public_ip=192.168.1.1,status=activated dz_prefixes="10.0.0.0/16,10.1.0.0/16",max_users=100,users_count=5 `,
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
			expected:  `contributors,code=dev-01,env=testnet,name=test-contributor,owner=` + pubKey1B58 + `,status=activated`,
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
			name:        "empty struct",
			measurement: "devices",
			input:       testDevice{},
			ts:          ts,
			expected:    `devices,code=,device_type=0,owner=11111111111111111111111111111111,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0 `,
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
			expected:  `devices,code=dev-01,device_type=2,env=mainnet,owner=` + pubKey2B58 + `,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0 `,
			expectErr: false,
		},
		{
			name:        "no tags",
			measurement: "devices",
			input: struct {
				Field1 int `influx:"field,field1"`
			}{Field1: 42},
			ts:       ts,
			expected: `devices field1=42 `,
		},
		{
			name:        "no fields",
			measurement: "devices",
			input: struct {
				Tag1 string `influx:"tag,tag1"`
			}{Tag1: "value1"},
			ts:       ts,
			expected: `devices,tag1=value1  `,
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var expected string
			if !tc.expectErr {
				expected = strings.TrimSpace(tc.expected) + " " + fmt.Sprintf("%d", tsNano)
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
