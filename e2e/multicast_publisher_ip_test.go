//go:build e2e

package e2e_test

import (
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/stretchr/testify/require"
)

// publisherUserInfo holds info for a multicast publisher user parsed from user list output.
type publisherUserInfo struct {
	ClientIP string
	DzIP     string
	UserType string
}

// parsePublisherUserInfo parses user list output and returns info for Multicast users.
func parsePublisherUserInfo(output []byte) []publisherUserInfo {
	rows := fixtures.ParseCLITable(output)
	var users []publisherUserInfo
	for _, row := range rows {
		if row["user_type"] == "Multicast" {
			users = append(users, publisherUserInfo{
				ClientIP: row["client_ip"],
				DzIP:     row["dz_ip"],
				UserType: row["user_type"],
			})
		}
	}
	return users
}

// TestE2E_MulticastPublisher_MultipleAllocations verifies that multiple publishers
// get unique sequential IPs from the global multicast_publisher_block (147.51.126.0/23).
func TestE2E_MulticastPublisher_MultipleAllocations(t *testing.T) {
	t.Parallel()

	dn, _, client1 := NewSingleDeviceSingleClientTestDevnet(t)

	// Add two more clients for additional publishers
	client2, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	client3, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 102,
	})
	require.NoError(t, err)

	// Set access passes for all clients
	for _, client := range []*devnet.Client{client1, client2, client3} {
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
		})
		require.NoError(t, err)
	}

	// Create three multicast groups
	for _, groupCode := range []string{"mg01", "mg02", "mg03"} {
		dn.CreateMulticastGroupOnchain(t, client1, groupCode)

		// Add all clients to publisher allowlist
		for _, client := range []*devnet.Client{client1, client2, client3} {
			_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
				"doublezero multicast group allowlist publisher add --code " + groupCode +
					" --user-payer " + client.Pubkey + " --client-ip " + client.CYOANetworkIP,
			})
			require.NoError(t, err)
		}
	}

	if !t.Run("connect_and_verify", func(t *testing.T) {
		// Connect each client as publisher to different multicast groups
		dn.ConnectMulticastPublisherSkipAccessPass(t, client1, "mg01")
		dn.ConnectMulticastPublisherSkipAccessPass(t, client2, "mg02")
		dn.ConnectMulticastPublisherSkipAccessPass(t, client3, "mg03")

		// Wait for all tunnels to come up
		for _, client := range []*devnet.Client{client1, client2, client3} {
			err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
			require.NoError(t, err)
		}

		// Query user list from manager to get dz_ip allocations
		userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err)

		publishers := parsePublisherUserInfo(userListOutput)
		require.Len(t, publishers, 3, "should have 3 multicast publisher users")

		// Verify all IPs are from global multicast_publisher_block (147.51.126.0/23)
		// and are sequential
		dzIPs := []string{}
		for _, pub := range publishers {
			require.True(t, netutil.IPInRange(pub.DzIP, "147.51.126.0/23"),
				"publisher dz_ip %s should be in global multicast_publisher_block 147.51.126.0/23", pub.DzIP)
			dzIPs = append(dzIPs, pub.DzIP)
		}

		// Verify all IPs are unique and sequential (contiguous)
		sort.Slice(dzIPs, func(i, j int) bool {
			a := net.ParseIP(dzIPs[i]).To4()
			b := net.ParseIP(dzIPs[j]).To4()
			for k := range a {
				if a[k] != b[k] {
					return a[k] < b[k]
				}
			}
			return false
		})
		for i, ip := range dzIPs {
			if i > 0 {
				prev := net.ParseIP(dzIPs[i-1]).To4()
				curr := net.ParseIP(ip).To4()
				prevInt := uint32(prev[0])<<24 | uint32(prev[1])<<16 | uint32(prev[2])<<8 | uint32(prev[3])
				currInt := uint32(curr[0])<<24 | uint32(curr[1])<<16 | uint32(curr[2])<<8 | uint32(curr[3])
				require.Equal(t, prevInt+1, currInt,
					"IPs should be sequential: %s followed by %s", dzIPs[i-1], ip)
			}
		}

		dn.log.Info("✓ All publishers allocated unique sequential IPs from global block",
			"dz_ips", dzIPs)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectMulticastPublisher(t, client1)
		dn.DisconnectMulticastPublisher(t, client2)
		dn.DisconnectMulticastPublisher(t, client3)
	}) {
		t.Fail()
	}
}

// TestE2E_MulticastPublisher_MixedUsers verifies that publishers and IBRL users
// coexist with separate IP pools (publishers use global block, IBRL uses device block).
func TestE2E_MulticastPublisher_MixedUsers(t *testing.T) {
	t.Parallel()

	dn, device, publisherClient := NewSingleDeviceSingleClientTestDevnet(t)

	// Add an IBRL client
	ibrlClient, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	// Set access passes
	for _, client := range []*devnet.Client{publisherClient, ibrlClient} {
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
		})
		require.NoError(t, err)
	}

	// Create multicast group for publisher
	dn.CreateMulticastGroupOnchain(t, publisherClient, "mg01")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero multicast group allowlist publisher add --code mg01" +
			" --user-payer " + publisherClient.Pubkey + " --client-ip " + publisherClient.CYOANetworkIP,
	})
	require.NoError(t, err)

	if !t.Run("connect_and_verify", func(t *testing.T) {
		// Connect publisher
		dn.ConnectMulticastPublisherSkipAccessPass(t, publisherClient, "mg01")
		err := publisherClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Connect IBRL user with allocated IP
		dn.ConnectUserTunnelWithAllocatedIP(t, ibrlClient)
		err = ibrlClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Query user list
		userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err)

		rows := fixtures.ParseCLITable(userListOutput)
		require.Len(t, rows, 2, "should have 2 users (1 publisher + 1 IBRL)")

		var publisherDzIP, ibrlDzIP string
		for _, row := range rows {
			switch row["user_type"] {
			case "Multicast":
				publisherDzIP = row["dz_ip"]
			case "IBRLWithAllocatedIP":
				ibrlDzIP = row["dz_ip"]
			}
		}

		require.NotEmpty(t, publisherDzIP, "publisher should have dz_ip")
		require.NotEmpty(t, ibrlDzIP, "IBRL user should have dz_ip")

		// Verify publisher uses global multicast_publisher_block
		require.True(t, netutil.IPInRange(publisherDzIP, "147.51.126.0/23"),
			"publisher dz_ip %s should be from global block 147.51.126.0/23", publisherDzIP)

		// Verify IBRL user uses device DzPrefixBlock
		require.True(t, netutil.IPInRange(ibrlDzIP, device.DZPrefix),
			"IBRL dz_ip %s should be from device block %s", ibrlDzIP, device.DZPrefix)

		// Verify IPs don't conflict
		require.NotEqual(t, publisherDzIP, ibrlDzIP,
			"publisher and IBRL user should have different IPs")

		dn.log.Info("✓ Publisher and IBRL user coexist with separate IP pools",
			"publisher_dz_ip", publisherDzIP, "publisher_pool", "147.51.126.0/23",
			"ibrl_dz_ip", ibrlDzIP, "ibrl_pool", device.DZPrefix)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectMulticastPublisher(t, publisherClient)
		dn.DisconnectUserTunnel(t, ibrlClient)
	}) {
		t.Fail()
	}
}

// TestE2E_MulticastPublisher_IPDeallocation verifies that IPs are returned to the pool
// when publishers are deleted and can be reused by new publishers.
func TestE2E_MulticastPublisher_IPDeallocation(t *testing.T) {
	t.Parallel()

	dn, _, client1 := NewSingleDeviceSingleClientTestDevnet(t)

	// Add second client
	client2, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	// Set access passes
	for _, client := range []*devnet.Client{client1, client2} {
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
		})
		require.NoError(t, err)
	}

	// Create multicast groups
	for _, groupCode := range []string{"mg01", "mg02"} {
		dn.CreateMulticastGroupOnchain(t, client1, groupCode)
		for _, client := range []*devnet.Client{client1, client2} {
			_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
				"doublezero multicast group allowlist publisher add --code " + groupCode +
					" --user-payer " + client.Pubkey + " --client-ip " + client.CYOANetworkIP,
			})
			require.NoError(t, err)
		}
	}

	var firstPublisherIP string

	if !t.Run("connect_first_publisher", func(t *testing.T) {
		// Connect first publisher
		dn.ConnectMulticastPublisherSkipAccessPass(t, client1, "mg01")
		err := client1.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Query and record the IP
		userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err)

		publishers := parsePublisherUserInfo(userListOutput)
		require.Len(t, publishers, 1, "should have 1 publisher")
		firstPublisherIP = publishers[0].DzIP

		require.True(t, netutil.IPInRange(firstPublisherIP, "147.51.126.0/23"),
			"first publisher dz_ip %s should be from global block", firstPublisherIP)

		dn.log.Info("✓ First publisher connected", "dz_ip", firstPublisherIP)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect_and_delete_first_publisher", func(t *testing.T) {
		// Disconnect first publisher
		dn.DisconnectMulticastPublisher(t, client1)

		// Wait for user removal
		require.Eventually(t, func() bool {
			userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
			if err != nil {
				return false
			}
			return len(parsePublisherUserInfo(userListOutput)) == 0
		}, 30*time.Second, 1*time.Second, "publisher user should be removed after disconnect")

		dn.log.Info("✓ First publisher disconnected and deleted", "freed_ip", firstPublisherIP)

	}) {
		t.Fail()
		return
	}

	if !t.Run("connect_second_publisher_reuses_ip", func(t *testing.T) {
		// Connect second publisher - should reuse the first IP
		dn.ConnectMulticastPublisherSkipAccessPass(t, client2, "mg02")
		err := client2.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Query and verify IP is reused
		userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err)

		publishers := parsePublisherUserInfo(userListOutput)
		require.Len(t, publishers, 1, "should have 1 publisher")
		secondPublisherIP := publishers[0].DzIP

		// The IP should be reused (bitmap allocates from lowest available)
		require.Equal(t, firstPublisherIP, secondPublisherIP,
			"second publisher should reuse the first publisher's IP after deallocation")

		dn.log.Info("✓ Second publisher reused deallocated IP",
			"reused_ip", secondPublisherIP, "original_ip", firstPublisherIP)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectMulticastPublisher(t, client2)
	}) {
		t.Fail()
	}
}

// TestE2E_MulticastPublisher_BothAllocationPaths verifies that offchain publisher IP
// allocations are synced to the onchain ResourceExtension bitmap, and deallocations
// are also synced back.
func TestE2E_MulticastPublisher_BothAllocationPaths(t *testing.T) {
	t.Parallel()

	dn, _, client1 := NewSingleDeviceSingleClientTestDevnetWithOnchainAllocation(t)

	// Add one more client
	client2, err := dn.Devnet.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	// Set access passes for all clients
	for _, client := range []*devnet.Client{client1, client2} {
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
		})
		require.NoError(t, err)
	}

	// Create two multicast groups
	for _, groupCode := range []string{"mg01", "mg02"} {
		dn.CreateMulticastGroupOnchain(t, client1, groupCode)

		// Add all clients to publisher allowlist
		for _, client := range []*devnet.Client{client1, client2} {
			_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
				"doublezero multicast group allowlist publisher add --code " + groupCode +
					" --user-payer " + client.Pubkey + " --client-ip " + client.CYOANetworkIP,
			})
			require.NoError(t, err)
		}
	}

	if !t.Run("connect_and_verify", func(t *testing.T) {
		// Connect both clients as publishers to different multicast groups
		dn.ConnectMulticastPublisherSkipAccessPass(t, client1, "mg01")
		dn.ConnectMulticastPublisherSkipAccessPass(t, client2, "mg02")

		// Wait for all tunnels to come up
		for _, client := range []*devnet.Client{client1, client2} {
			err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
			require.NoError(t, err)
		}

		// Query user list to get allocated IPs
		userListOutput, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err)

		publishers := parsePublisherUserInfo(userListOutput)
		require.Len(t, publishers, 2, "should have 2 multicast publisher users")

		// Collect allocated IPs
		dzIPs := []string{}
		for _, pub := range publishers {
			require.True(t, netutil.IPInRange(pub.DzIP, "147.51.126.0/23"),
				"publisher dz_ip %s should be in global multicast_publisher_block", pub.DzIP)
			dzIPs = append(dzIPs, pub.DzIP)
		}

		// Verify off-chain allocations are synced to on-chain resource bitmap
		resourceOutput, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "resource", "get",
			"--resource-type", "multicast-publisher-block",
		})
		require.NoError(t, err)
		resourceStr := string(resourceOutput)

		// Both IPs should appear in the on-chain bitmap
		for _, ip := range dzIPs {
			require.Contains(t, resourceStr, ip+"/32",
				"off-chain allocated IP %s should be synced to on-chain bitmap", ip)
		}

		dn.log.Info("✓ Off-chain allocations synced to on-chain bitmap",
			"dz_ips", dzIPs)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect_and_verify_deallocation", func(t *testing.T) {
		// Disconnect both publishers
		dn.DisconnectMulticastPublisher(t, client1)
		dn.DisconnectMulticastPublisher(t, client2)

		// Wait for deallocation to sync to on-chain
		require.Eventually(t, func() bool {
			resourceOutput, err := dn.Manager.Exec(t.Context(), []string{
				"doublezero", "resource", "get",
				"--resource-type", "multicast-publisher-block",
			})
			if err != nil {
				return false
			}
			// Bitmap should be empty after deallocation
			return !strings.Contains(string(resourceOutput), "147.51.126")
		}, 30*time.Second, 2*time.Second, "IPs should be deallocated from on-chain bitmap")

		dn.log.Info("✓ Off-chain deallocations synced to on-chain bitmap")
	}) {
		t.Fail()
	}
}
