package serviceability

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToLineProtocol(t *testing.T) {
	t.Parallel()

	var pubKey1 [32]byte
	copy(pubKey1[:], "11111111111111111111111111111111")
	pubKey1B58 := "6JjJS3bJS2s2p2Y9x2y2q2y2q2y2q2y2q2y2q2y2q"

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
			expected:  `devices,code=dev-01,device_type=1,env=testnet,owner=` + pubKey1B58 + `,public_ip=192.168.1.1,status=activated dz_prefixes="10.0.0.0/16,10.1.0.0/16",max_users=100,users_count=5 ` + time.Unix(0, tsNano).Format("1582230000000000000"),
			expectErr: false,
		},
		{
			name:        "empty struct",
			measurement: "devices",
			input:       testDevice{},
			ts:          ts,
			expected:    `devices,code=,device_type=0,owner=11111111111111111111111111111111,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0 ` + time.Unix(0, tsNano).Format("1582230000000000000"),
			expectErr:   false,
		},
		{
			name:        "additional tags override struct tags",
			measurement: "devices",
			input: testDevice{
				Owner: pubKey1,
				Code:  "dev-01",
			},
			ts: ts,
			additionalTags: map[string]string{
				"owner": pubKey2B58,
				"env":   "mainnet",
			},
			expected:  `devices,code=dev-01,device_type=0,env=mainnet,owner=` + pubKey2B58 + `,public_ip=0.0.0.0,status=pending dz_prefixes="",max_users=0,users_count=0 ` + time.Unix(0, tsNano).Format("1582230000000000000"),
			expectErr: false,
		},
		{
			name:        "no tags",
			measurement: "devices",
			input: struct {
				Field1 int `influx:"field,field1"`
			}{Field1: 42},
			ts:       ts,
			expected: `devices field1=42 ` + time.Unix(0, tsNano).Format("1582230000000000000"),
		},
		{
			name:        "no fields",
			measurement: "devices",
			input: struct {
				Tag1 string `influx:"tag,tag1"`
			}{Tag1: "value1"},
			ts:       ts,
			expected: `devices,tag1=value1  ` + time.Unix(0, tsNano).Format("1582230000000000000"),
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

			expected := tc.expected
			if !tc.expectErr {
				expected = strings.Replace(expected, "1582230000000000000", fmt.Sprintf("%d", tsNano), 1)
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
