package twamplight_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignedReflector_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	t.Run("echo with valid signature", func(t *testing.T) {
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

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		probe := twamplight.NewSignedProbePacket(1, senderPriv)
		var buf [twamplight.SignedProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))

		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		n, err := conn.Read(replyBuf)
		require.NoError(t, err)
		assert.Equal(t, twamplight.SignedReplyPacketSize, n)

		reply, err := twamplight.UnmarshalSignedReplyPacket(replyBuf[:n])
		require.NoError(t, err)

		assert.Equal(t, probe.Seq, reply.Probe.Seq)
		assert.Equal(t, probe.Sec, reply.Probe.Sec)
		assert.Equal(t, probe.Frac, reply.Probe.Frac)
		assert.Equal(t, reflectorPubKey, reply.ReflectorPubkey)
		assert.True(t, twamplight.VerifyProbe(&reply.Probe))
		assert.True(t, twamplight.VerifyReply(reply))
	})

	t.Run("reject unauthorized pubkey", func(t *testing.T) {
		t.Parallel()

		_, unauthorizedPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		authorizedPub, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		var authorizedPubKey [32]byte
		copy(authorizedPubKey[:], authorizedPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{authorizedPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		probe := twamplight.NewSignedProbePacket(1, unauthorizedPriv)
		var buf [twamplight.SignedProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for unauthorized pubkey")
	})

	t.Run("reject invalid signature", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		probe := twamplight.NewSignedProbePacket(1, senderPriv)
		probe.Signature[0] ^= 0xff // Corrupt signature.

		var buf [twamplight.SignedProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for invalid signature")
	})

	t.Run("reject wrong-size packets", func(t *testing.T) {
		t.Parallel()

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, nil)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.Write(make([]byte, 50))
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for wrong-size packet")
	})

	t.Run("concurrent clients", func(t *testing.T) {
		t.Parallel()

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflectorPub := reflectorPriv.Public().(ed25519.PublicKey)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		const numClients = 3
		var senderKeys [numClients]struct {
			pub  [32]byte
			priv ed25519.PrivateKey
		}
		authorizedKeys := make([][32]byte, numClients)
		for i := range numClients {
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			copy(senderKeys[i].pub[:], pub)
			senderKeys[i].priv = priv
			authorizedKeys[i] = senderKeys[i].pub
		}

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, authorizedKeys)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		var wg sync.WaitGroup
		for i := range numClients {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
				if err != nil {
					t.Errorf("client %d: dial failed: %v", idx, err)
					return
				}
				defer conn.Close()

				// One probe per pubkey — rate limiter allows one verification per interval.
				probe := twamplight.NewSignedProbePacket(1, senderKeys[idx].priv)
				var buf [twamplight.SignedProbePacketSize]byte
				if err := probe.Marshal(buf[:]); err != nil {
					t.Errorf("client %d: marshal failed: %v", idx, err)
					return
				}
				if _, err := conn.Write(buf[:]); err != nil {
					t.Errorf("client %d: write failed: %v", idx, err)
					return
				}

				if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Errorf("client %d: set deadline failed: %v", idx, err)
					return
				}
				replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
				n, err := conn.Read(replyBuf)
				if err != nil {
					t.Errorf("client %d: read failed: %v", idx, err)
					return
				}

				reply, err := twamplight.UnmarshalSignedReplyPacket(replyBuf[:n])
				if err != nil {
					t.Errorf("client %d: unmarshal failed: %v", idx, err)
					return
				}

				if !twamplight.VerifyReply(reply) {
					t.Errorf("client %d: reply signature verification failed", idx)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		t.Parallel()

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err, "Run should return nil on graceful shutdown")
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after context cancellation")
		}
	})

	t.Run("pubkey allowlist update at runtime", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		// Start reflector with NO authorized keys.
		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, nil)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// Send probe — rejected at allowlist (no authorized keys).
		probe := twamplight.NewSignedProbePacket(1, senderPriv)
		var buf [twamplight.SignedProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply before authorization")

		// Update authorized keys to include sender.
		reflector.SetAuthorizedKeys([][32]byte{senderPubKey})

		// Send again — now passes allowlist, first time through rate limiter.
		probe2 := twamplight.NewSignedProbePacket(2, senderPriv)
		require.NoError(t, probe2.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		n, err := conn.Read(replyBuf)
		require.NoError(t, err, "should receive reply after authorization")
		assert.Equal(t, twamplight.SignedReplyPacketSize, n)

		reply, err := twamplight.UnmarshalSignedReplyPacket(replyBuf[:n])
		require.NoError(t, err)
		assert.True(t, twamplight.VerifyReply(reply))
	})

	t.Run("rate limit drops repeated probes from same pubkey", func(t *testing.T) {
		t.Parallel()

		senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		reflector, err := twamplight.NewLinuxSignedReflector("127.0.0.1:0", 100*time.Millisecond, reflectorPriv, [][32]byte{senderPubKey})
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, reflector.LocalAddr())
		require.NoError(t, err)
		defer conn.Close()

		// First probe should get a reply.
		probe1 := twamplight.NewSignedProbePacket(1, senderPriv)
		var buf [twamplight.SignedProbePacketSize]byte
		require.NoError(t, probe1.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		replyBuf := make([]byte, twamplight.SignedReplyPacketSize)
		_, err = conn.Read(replyBuf)
		require.NoError(t, err, "first probe should receive reply")

		// Second probe immediately after should be rate-limited.
		probe2 := twamplight.NewSignedProbePacket(2, senderPriv)
		require.NoError(t, probe2.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "second probe should be rate-limited")
	})
}
