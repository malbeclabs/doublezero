package signed_test

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSender_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	origInterval := signed.VerifyInterval
	signed.VerifyInterval = 0
	t.Cleanup(func() { signed.VerifyInterval = origInterval })

	t.Run("successful signed RTT probe", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		reflectorPub, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := signed.NewLinuxSender(t.Context(), "", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())}, senderSigner, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		rtt, reply, err := sender.Probe(ctx)
		require.NoError(t, err)
		assert.True(t, rtt >= 0, "RTT should be non-negative")
		assert.NotNil(t, reply)
		assert.Equal(t, reflectorPubKey, reply.ReflectorPubkey)
		assert.True(t, reply.Probe.Verify())
		assert.True(t, reply.Verify())
	})

	t.Run("timeout returns error", func(t *testing.T) {
		t.Parallel()

		_, senderSigner := newTestSigner(t)

		var remotePubkey [32]byte

		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19999}
		sender, err := signed.NewLinuxSender(t.Context(), "", nil, addr, senderSigner, remotePubkey)
		require.NoError(t, err)
		defer sender.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, _, err = sender.Probe(ctx)
		assert.Error(t, err)
	})

	t.Run("reply contains original probe", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		reflectorPub, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := signed.NewLinuxSender(t.Context(), "", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())}, senderSigner, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		_, reply, err := sender.Probe(ctx)
		require.NoError(t, err)

		assert.Equal(t, senderPubKey, reply.Probe.SenderPubkey)
		assert.True(t, reply.Probe.Verify(), "probe signature in reply should verify")
		assert.True(t, reply.Verify(), "reply signature should verify")
	})

	t.Run("multiple sequential probes", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		reflectorPub, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := signed.NewLinuxSender(t.Context(), "", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())}, senderSigner, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		for i := 0; i < 5; i++ {
			rtt, reply, err := sender.Probe(ctx)
			require.NoError(t, err, "probe %d failed", i)
			assert.True(t, rtt >= 0, "probe %d: RTT should be non-negative", i)
			assert.NotNil(t, reply, "probe %d: reply should not be nil", i)
			assert.True(t, reply.Verify(), "probe %d: reply signature should verify", i)
		}
	})
}
