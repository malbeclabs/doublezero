package signed_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReflector_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	t.Run("echo with valid signature", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		reflectorPub, reflectorSigner := newTestSigner(t)
		geoprobePub, _ := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)
		var geoprobePubKey [32]byte
		copy(geoprobePubKey[:], geoprobePub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, geoprobePubKey, [][32]byte{senderPubKey}, 0)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		probe := signed.NewProbePacket(1, senderSigner)
		var buf [signed.ProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))

		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		n, err := conn.Read(replyBuf)
		require.NoError(t, err)
		assert.Equal(t, signed.MinReplyPacketSize, n)

		reply, err := signed.UnmarshalReplyPacket(replyBuf[:n])
		require.NoError(t, err)

		assert.Equal(t, probe.Seq, reply.Probe.Seq)
		assert.Equal(t, probe.Sec, reply.Probe.Sec)
		assert.Equal(t, probe.Frac, reply.Probe.Frac)
		assert.Equal(t, reflectorPubKey, reply.AuthorityPubkey)
		assert.Equal(t, geoprobePubKey, reply.GeoprobePubkey)
		assert.Empty(t, reply.Offsets)
		assert.True(t, reply.Probe.Verify())
		assert.True(t, reply.Verify())
	})

	t.Run("reject unauthorized pubkey", func(t *testing.T) {
		t.Parallel()

		_, unauthorizedSigner := newTestSigner(t)
		_, reflectorSigner := newTestSigner(t)

		authorizedPub, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		var authorizedPubKey [32]byte
		copy(authorizedPubKey[:], authorizedPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, [][32]byte{authorizedPubKey}, 0)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		probe := signed.NewProbePacket(1, unauthorizedSigner)
		var buf [signed.ProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for unauthorized pubkey")
	})

	t.Run("reject invalid signature", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		_, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, [][32]byte{senderPubKey}, 0)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		probe := signed.NewProbePacket(1, senderSigner)
		probe.Signature[0] ^= 0xff // Corrupt signature.

		var buf [signed.ProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for invalid signature")
	})

	t.Run("reject wrong-size packets", func(t *testing.T) {
		t.Parallel()

		_, reflectorSigner := newTestSigner(t)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, nil, 0)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		_, err = conn.Write(make([]byte, 50))
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply for wrong-size packet")
	})

	t.Run("concurrent clients", func(t *testing.T) {
		t.Parallel()

		_, reflectorSigner := newTestSigner(t)

		reflectorPub := reflectorSigner.Public()
		var reflectorPubKey [32]byte
		copy(reflectorPubKey[:], reflectorPub)

		const numClients = 3
		var senderKeys [numClients]struct {
			pub    [32]byte
			signer signed.Signer
		}
		authorizedKeys := make([][32]byte, numClients)
		for i := range numClients {
			pub, signer := newTestSigner(t)
			copy(senderKeys[i].pub[:], pub)
			senderKeys[i].signer = signer
			authorizedKeys[i] = senderKeys[i].pub
		}

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, reflectorPubKey, authorizedKeys, 0)
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

				conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
				if err != nil {
					t.Errorf("client %d: dial failed: %v", idx, err)
					return
				}
				defer conn.Close()

				probe := signed.NewProbePacket(1, senderKeys[idx].signer)
				var buf [signed.ProbePacketSize]byte
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
				replyBuf := make([]byte, signed.MaxReplyPacketSize)
				n, err := conn.Read(replyBuf)
				if err != nil {
					t.Errorf("client %d: read failed: %v", idx, err)
					return
				}

				reply, err := signed.UnmarshalReplyPacket(replyBuf[:n])
				if err != nil {
					t.Errorf("client %d: unmarshal failed: %v", idx, err)
					return
				}

				if !reply.Verify() {
					t.Errorf("client %d: reply signature verification failed", idx)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		t.Parallel()

		_, reflectorSigner := newTestSigner(t)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, nil, 0)
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

		senderPub, senderSigner := newTestSigner(t)
		_, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, nil, 0)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		probe := signed.NewProbePacket(1, senderSigner)
		var buf [signed.ProbePacketSize]byte
		require.NoError(t, probe.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "should not receive reply before authorization")

		reflector.SetAuthorizedKeys([][32]byte{senderPubKey})

		probe2 := signed.NewProbePacket(2, senderSigner)
		require.NoError(t, probe2.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		n, err := conn.Read(replyBuf)
		require.NoError(t, err, "should receive reply after authorization")
		assert.Equal(t, signed.MinReplyPacketSize, n)

		reply, err := signed.UnmarshalReplyPacket(replyBuf[:n])
		require.NoError(t, err)
		assert.True(t, reply.Verify())
	})

	t.Run("rate limit drops repeated probes from same pubkey", func(t *testing.T) {
		t.Parallel()

		senderPub, senderSigner := newTestSigner(t)
		_, reflectorSigner := newTestSigner(t)

		var senderPubKey [32]byte
		copy(senderPubKey[:], senderPub)

		reflector, err := signed.NewLinuxReflector("127.0.0.1:0", 100*time.Millisecond, reflectorSigner, [32]byte{}, [][32]byte{senderPubKey}, 10*time.Second)
		require.NoError(t, err)
		defer reflector.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			_ = reflector.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)

		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(reflector.Port())})
		require.NoError(t, err)
		defer conn.Close()

		probe1 := signed.NewProbePacket(1, senderSigner)
		var buf [signed.ProbePacketSize]byte
		require.NoError(t, probe1.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		replyBuf := make([]byte, signed.MaxReplyPacketSize)
		_, err = conn.Read(replyBuf)
		require.NoError(t, err, "first probe should receive reply")

		probe2 := signed.NewProbePacket(2, senderSigner)
		require.NoError(t, probe2.Marshal(buf[:]))
		_, err = conn.Write(buf[:])
		require.NoError(t, err)

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
		_, err = conn.Read(replyBuf)
		assert.Error(t, err, "second probe should be rate-limited")
	})
}
