//go:build !linux
// +build !linux

package udp_test

import (
	"context"
	"log/slog"
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_TimestampedDialer_KernelStub(t *testing.T) {
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

func TestUDP_TimestampedReader_KernelStub(t *testing.T) {
	t.Run("NewKernelReader returns ErrPlatformNotSupported", func(t *testing.T) {
		conn := &net.UDPConn{}
		kr, err := udp.NewKernelTimestampedReader(slog.Default(), conn)
		require.ErrorIs(t, err, udp.ErrPlatformNotSupported)
		require.Nil(t, kr)
	})

	t.Run("Read returns ErrPlatformNotSupported", func(t *testing.T) {
		kr := &udp.KernelTimestampedReader{}
		buf := make([]byte, 128)
		_, ts, err := kr.Read(context.Background(), buf)
		require.ErrorIs(t, err, udp.ErrPlatformNotSupported)
		require.True(t, ts.IsZero())
	})
}
