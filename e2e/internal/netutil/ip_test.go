package netutil_test

import (
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/stretchr/testify/require"
)

func TestBuildIPInCIDR(t *testing.T) {
	ip, err := netutil.BuildIPInCIDR("192.168.1.0/24", 80)
	require.NoError(t, err)
	require.Equal(t, "192.168.1.80", ip.String())
}
