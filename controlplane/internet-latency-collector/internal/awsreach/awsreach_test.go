package awsreach

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testPrefixes = `[
  {"us-west-2": {"35.95.0.0/16": "35.95.2.254", "34.208.0.0/12": "34.208.0.0", "35.100.0.0/16": "35.100.0.0"}},
  {"us-west-2-lax-1": {"70.224.192.0/18": "70.224.192.0"}},
  {"eu-central-1": {"3.64.0.0/12": "3.64.0.0", "18.153.0.0/16": "not-an-address"}}
]`

type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("mock not configured")
}

func TestInternetLatency_AWSReach_ParsePrefixes(t *testing.T) {
	t.Parallel()

	targets, err := ParsePrefixes(strings.NewReader(testPrefixes))
	require.NoError(t, err)

	require.Len(t, targets, 3, "every region key stands on its own")
	require.Equal(t, []string{"34.208.0.0", "35.95.2.254", "35.100.0.0"}, targets["us-west-2"],
		"addresses must be sorted numerically so output is stable")
	require.Equal(t, []string{"70.224.192.0"}, targets["us-west-2-lax-1"],
		"a Local Zone must not fold into its parent region")
	require.Equal(t, []string{"3.64.0.0"}, targets["eu-central-1"],
		"values that are not IPv4 addresses are dropped")
}

func TestInternetLatency_AWSReach_ParsePrefixes_Empty(t *testing.T) {
	t.Parallel()

	_, err := ParsePrefixes(strings.NewReader(`[]`))
	require.Error(t, err)
	require.ErrorContains(t, err, "no regions")
}

func TestInternetLatency_AWSReach_ParsePrefixes_NoIPv4(t *testing.T) {
	t.Parallel()

	_, err := ParsePrefixes(strings.NewReader(`[{"eu-west-1": {"18.153.0.0/16": "not-an-address"}}]`))
	require.Error(t, err)
	require.ErrorContains(t, err, "no regions with IPv4 addresses")
}

func TestInternetLatency_AWSReach_FetchPrefixes(t *testing.T) {
	t.Parallel()

	var requestedURL string
	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(testPrefixes)),
			}, nil
		},
	}

	targets, err := FetchPrefixes(t.Context(), client, PrefixesURL)
	require.NoError(t, err)
	require.Equal(t, "http://ec2-reachability.amazonaws.com/prefixes-ipv4.json", requestedURL,
		"the list is served over plain HTTP only")
	require.Contains(t, targets, "eu-central-1")
}

func TestInternetLatency_AWSReach_FetchPrefixes_BadStatus(t *testing.T) {
	t.Parallel()

	client := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(bytes.NewBufferString("")),
			}, nil
		},
	}

	_, err := FetchPrefixes(t.Context(), client, PrefixesURL)
	require.Error(t, err)
	require.ErrorContains(t, err, "status: 503")
}

func TestInternetLatency_AWSReach_PreferFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []string
		preferred  string
		want       []string
	}{
		{
			name:       "Preferred moves to the front",
			candidates: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			preferred:  "3.3.3.3",
			want:       []string{"3.3.3.3", "1.1.1.1", "2.2.2.2"},
		},
		{
			name:       "Preferred already first",
			candidates: []string{"1.1.1.1", "2.2.2.2"},
			preferred:  "1.1.1.1",
			want:       []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			name:       "Preferred absent",
			candidates: []string{"1.1.1.1", "2.2.2.2"},
			preferred:  "9.9.9.9",
			want:       []string{"1.1.1.1", "2.2.2.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, PreferFirst(tt.candidates, tt.preferred))
		})
	}
}

func TestInternetLatency_AWSReach_FirstAnswering(t *testing.T) {
	t.Parallel()

	var tried []string
	ping := func(ctx context.Context, address string) bool {
		tried = append(tried, address)
		return address == "3.3.3.3"
	}

	got, err := FirstAnswering(t.Context(), []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}, ping, 8)
	require.NoError(t, err)
	require.Equal(t, "3.3.3.3", got)
	require.Equal(t, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, tried,
		"it stops at the first address that answers")
}

func TestInternetLatency_AWSReach_FirstAnswering_AttemptCap(t *testing.T) {
	t.Parallel()

	attempts := 0
	ping := func(ctx context.Context, address string) bool {
		attempts++
		return false
	}

	_, err := FirstAnswering(t.Context(), []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, ping, 2)
	require.Error(t, err)
	require.ErrorContains(t, err, "no address answered after 2 attempts")
	require.Equal(t, 2, attempts)
}
