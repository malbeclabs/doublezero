package twamplight_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"runtime"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignedSender_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	// Sender tests validate sender behavior, not reflector rate limiting.
	origInterval := twamplight.SignedReflectorVerifyInterval
	twamplight.SignedReflectorVerifyInterval = 0
	t.Cleanup(func() { twamplight.SignedReflectorVerifyInterval = origInterval })

	t.Run("successful signed RTT probe", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflectorPub, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := twamplight.NewLinuxSignedSender(t.Context(), "", nil, reflector.LocalAddr(), senderPriv, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		rtt, reply, err := sender.Probe(ctx)
		require.NoError(t, err)
		assert.True(t, rtt >= 0, "RTT should be non-negative")
		assert.NotNil(t, reply)
		assert.Equal(t, reflectorPubKey, reply.ReflectorPubkey)
		assert.True(t, twamplight.VerifyProbe(&reply.Probe))
		assert.True(t, twamplight.VerifyReply(reply))
	})

	t.Run("timeout returns error", func(t *testing.T) {
		t.Parallel()

		_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var remotePubkey [32]byte

		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19999}
		sender, err := twamplight.NewLinuxSignedSender(t.Context(), "", nil, addr, senderPriv, remotePubkey)
		require.NoError(t, err)
		defer sender.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, _, err = sender.Probe(ctx)
		assert.Error(t, err)
	})

	t.Run("reply contains original probe", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflectorPub, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := twamplight.NewLinuxSignedSender(t.Context(), "", nil, reflector.LocalAddr(), senderPriv, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		_, reply, err := sender.Probe(ctx)
		require.NoError(t, err)

		assert.Equal(t, senderPubKey, reply.Probe.SenderPubkey)
		assert.True(t, twamplight.VerifyProbe(&reply.Probe), "probe signature in reply should verify")
		assert.True(t, twamplight.VerifyReply(reply), "reply signature should verify")
	})

	t.Run("multiple sequential probes", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflectorPub, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		sender, err := twamplight.NewLinuxSignedSender(t.Context(), "", nil, reflector.LocalAddr(), senderPriv, reflectorPubKey)
		require.NoError(t, err)
		defer sender.Close()

		for i := 0; i < 5; i++ {
			rtt, reply, err := sender.Probe(ctx)
			require.NoError(t, err, "probe %d failed", i)
			assert.True(t, rtt >= 0, "probe %d: RTT should be non-negative", i)
			assert.NotNil(t, reply, "probe %d: reply should not be nil", i)
			assert.True(t, twamplight.VerifyReply(reply), "probe %d: reply signature should verify", i)
		}
	})
}
