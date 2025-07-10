//go:build linux
// +build linux

package udp_test

import (
	"log/slog"
	"testing"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_NewDialer_UsesKernelDialerOnLinux(t *testing.T) {
	log := slog.New(slog.NewTextHandler(nil, nil))
	d := udp.NewDialer(log)

	require.IsType(t, &udp.KernelDialer{}, d)
}
