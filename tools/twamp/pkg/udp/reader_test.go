package udp_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/udp"
	"github.com/stretchr/testify/require"
)

func TestUDP_NewTimestampedReader_ReadsMessage(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, _ := net.ListenUDP("udp", addr)
	defer conn.Close()

	r := udp.NewTimestampedReader(slog.Default(), conn)

	sender, _ := net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
	defer sender.Close()
	_, err := sender.Write([]byte("ping"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	buf := make([]byte, 512)
	n, ts, err := r.Read(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf[:n]))
	require.False(t, ts.IsZero())
}
