//go:build !linux
// +build !linux

package udp_test

import (
	"log/slog"
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_NewTimestampedReader_FallbackToWallclock(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, _ := net.ListenUDP("udp", addr)
	defer conn.Close()

	log := slog.New(slog.NewTextHandler(nil, nil))
	r := udp.NewTimestampedReader(log, conn)

	require.IsType(t, &udp.WallclockTimestampedReader{}, r)
}
