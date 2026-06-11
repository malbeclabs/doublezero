//go:build e2e

package e2e_test

import (
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// TestE2E_Multicast_CounterLifecycle verifies that the device's
// multicast_publishers_count and multicast_subscribers_count are correctly
// decremented when users disconnect.
//
// This is a regression test for a bug where both publisher and subscriber
// multicast users decremented multicast_subscribers_count on disconnect.
// The root cause: the decrement path checked !user.publishers.is_empty(), but
// the ReferenceCountNotZero guard (earlier in the same instruction) requires
// publishers to be empty before deletion — so the publishers check was always
// false at delete time, and every departing user took the subscribers branch.
//
// The fix adds a durable multicast_publisher bool to the User struct, set at
// activation time when publishers is non-empty, and uses that flag in the
// delete and closeaccount instructions instead.
func TestE2E_Multicast_CounterLifecycle(t *testing.T) {
	t.Parallel()

	dn, device, publisherClient := NewSingleDeviceSingleClientTestDevnet(t)

	// Add a second client that will connect as subscriber.
	subscriberClient, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	// CreateMulticastGroupOnchain adds publisherClient to both allowlists.
	// Add subscriberClient to the subscriber allowlist separately.
	dn.CreateMulticastGroupOnchain(t, publisherClient, "counter-mc01")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero multicast group allowlist subscriber add --code counter-mc01" +
			" --user-payer " + subscriberClient.Pubkey +
			" --client-ip " + subscriberClient.CYOANetworkIP,
	})
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	// getDeviceCounters returns (publishers_count, subscribers_count) for the
	// device under test, queried directly from onchain program data.
	getDeviceCounters := func(t *testing.T) (publishers, subscribers uint16) {
		t.Helper()
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		for _, d := range data.Devices {
			if d.Code == device.Spec.Code {
				return d.MulticastPublishersCount, d.MulticastSubscribersCount
			}
		}
		t.Fatalf("device %s not found in program data", device.Spec.Code)
		return 0, 0
	}

	// clientIPBytes converts a dotted-decimal IP string to the [4]uint8 format
	// used by the serviceability SDK user struct.
	clientIPBytes := func(ip string) [4]uint8 {
		parsed := net.ParseIP(ip).To4()
		require.NotNil(t, parsed, "could not parse client IP: %s", ip)
		return [4]uint8{parsed[0], parsed[1], parsed[2], parsed[3]}
	}

	// waitForUserGone polls until no user with the given client IP exists onchain.
	waitForUserGone := func(t *testing.T, clientIP string, timeout time.Duration, msg string) {
		t.Helper()
		ipBytes := clientIPBytes(clientIP)
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, u := range data.Users {
				if u.ClientIp == ipBytes {
					return false
				}
			}
			return true
		}, timeout, 1*time.Second, msg)
	}

	// -----------------------------------------------------------------------
	// Phase 1: connect publisher and subscriber, verify counters go up
	// -----------------------------------------------------------------------
	if !t.Run("connect_and_verify", func(t *testing.T) {
		// Connect publisher (sets access pass + runs doublezero connect).
		dn.ConnectMulticastPublisher(t, publisherClient, "counter-mc01")

		// Wait for publisher to be activated onchain.
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, u := range data.Users {
				if u.Status == serviceability.UserStatusActivated &&
					u.UserType == serviceability.UserTypeMulticast &&
					len(u.Publishers) > 0 {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "multicast publisher was not activated within timeout")

		pub, sub := getDeviceCounters(t)
		require.Equal(t, uint16(1), pub, "publishers_count should be 1 after publisher connects")
		require.Equal(t, uint16(0), sub, "subscribers_count should be 0 before subscriber connects")

		// Connect subscriber.
		dn.ConnectMulticastSubscriber(t, subscriberClient, "counter-mc01")

		// Wait for subscriber to be activated onchain.
		subIPBytes := clientIPBytes(subscriberClient.CYOANetworkIP)
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, u := range data.Users {
				if u.Status == serviceability.UserStatusActivated &&
					u.UserType == serviceability.UserTypeMulticast &&
					u.ClientIp == subIPBytes {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "multicast subscriber was not activated within timeout")

		pub, sub = getDeviceCounters(t)
		require.Equal(t, uint16(1), pub, "publishers_count should remain 1 after subscriber connects")
		require.Equal(t, uint16(1), sub, "subscribers_count should be 1 after subscriber connects")
	}) {
		t.FailNow()
	}

	// -----------------------------------------------------------------------
	// Phase 2: disconnect publisher — publishers_count must go to 0;
	// subscribers_count must stay at 1 (the regression check).
	// -----------------------------------------------------------------------
	if !t.Run("publisher_disconnect_decrements_publishers_count", func(t *testing.T) {
		dn.DisconnectMulticastPublisher(t, publisherClient)

		waitForUserGone(t, publisherClient.CYOANetworkIP, 30*time.Second,
			"publisher user was not deleted onchain after disconnect")

		pub, sub := getDeviceCounters(t)
		require.Equal(t, uint16(0), pub,
			"publishers_count must decrement to 0 after publisher disconnects")
		require.Equal(t, uint16(1), sub,
			"subscribers_count must remain 1 after publisher disconnects "+
				"(regression: bug decremented subscribers_count for publishers)")
	}) {
		t.FailNow()
	}

	// -----------------------------------------------------------------------
	// Phase 3: disconnect subscriber — subscribers_count must go to 0.
	// -----------------------------------------------------------------------
	t.Run("subscriber_disconnect_decrements_subscribers_count", func(t *testing.T) {
		dn.DisconnectMulticastSubscriber(t, subscriberClient)

		waitForUserGone(t, subscriberClient.CYOANetworkIP, 30*time.Second,
			"subscriber user was not deleted onchain after disconnect")

		pub, sub := getDeviceCounters(t)
		require.Equal(t, uint16(0), pub, "publishers_count must remain 0")
		require.Equal(t, uint16(0), sub,
			"subscribers_count must decrement to 0 after subscriber disconnects")
	})
}
