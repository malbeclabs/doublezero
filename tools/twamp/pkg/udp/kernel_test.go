//go:build !linux
// +build !linux

package udp_test

import (
	"testing"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDPTimestampedDialer_KernelStub(t *testing.T) {
	t.Run("NewKernelDialer returns ErrPlatformNotSupported", func(t *testing.T) {
		kr, err := udp.NewKernelDialer()
		require.ErrorIs(t, err, udp.ErrPlatformNotSupported)
		require.Nil(t, kr)
	})

	t.Run("Dial returns ErrPlatformNotSupported", func(t *testing.T) {
		kd := &udp.KernelDialer{}
		_, err := kd.Dial(t.Context(), "", nil, nil)
		require.ErrorIs(t, err, udp.ErrPlatformNotSupported)
	})
}
