package probing

import (
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestProbing_RouteStore(t *testing.T) {
	t.Parallel()
	_ = t.Context()

	newRoute := func(table int, dstIP, nextHopIP string) *routing.Route {
		return &routing.Route{
			Table:   table,
			Dst:     &net.IPNet{IP: net.ParseIP(dstIP), Mask: net.CIDRMask(32, 32)},
			NextHop: net.ParseIP(nextHopIP),
			Src:     net.ParseIP("10.0.0.1"),
			// Protocol is intentionally omitted; tests don't rely on it
		}
	}

	t.Run("newRouteKey_IPv4", func(t *testing.T) {
		t.Parallel()
		r := newRoute(100, "192.0.2.1", "198.51.100.7")
		k := newRouteKey(r)

		expDst := netip.MustParseAddr("192.0.2.1")
		expNH := netip.MustParseAddr("198.51.100.7")

		require.Equal(t, 100, k.table)
		require.True(t, k.dst.IsValid())
		require.True(t, k.nextHop.IsValid())
		require.Equal(t, expDst, k.dst)
		require.Equal(t, expNH, k.nextHop)
	})

	t.Run("newRouteKey_IPv6_becomesZeroAddrs", func(t *testing.T) {
		t.Parallel()
		r := &routing.Route{
			Table:   300,
			Dst:     &net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(128, 128)},
			NextHop: net.ParseIP("2001:db8::2"),
			Src:     net.ParseIP("10.0.0.1"),
		}
		k := newRouteKey(r)
		require.Equal(t, 300, k.table)
		require.False(t, k.dst.IsValid())
		require.False(t, k.nextHop.IsValid())
	})

	t.Run("managedRoute_Key_and_String", func(t *testing.T) {
		t.Parallel()
		r := newRoute(42, "203.0.113.9", "203.0.113.10")
		mr := managedRoute{route: r, liveness: NewHysteresisLivenessPolicy(10, 10).NewTracker()}

		require.Equal(t, r.String(), mr.String())
		require.Equal(t, newRouteKey(r), mr.Key())
	})

	t.Run("routeStore_Set_Get_Len_Del", func(t *testing.T) {
		t.Parallel()
		s := newRouteStore()

		r1 := newRoute(1, "192.0.2.10", "192.0.2.1")
		r2 := newRoute(2, "192.0.2.20", "192.0.2.1")
		k1, k2 := newRouteKey(r1), newRouteKey(r2)

		require.Equal(t, 0, s.Len())

		s.Set(k1, managedRoute{route: r1})
		s.Set(k2, managedRoute{route: r2})
		require.Equal(t, 2, s.Len())

		v1, ok1 := s.Get(k1)
		require.True(t, ok1)
		require.Equal(t, r1, v1.route)

		s.Del(k1)
		_, ok1 = s.Get(k1)
		require.False(t, ok1)
		require.Equal(t, 1, s.Len())
	})

	t.Run("routeStore_Clone_returnsIndependentSnapshot", func(t *testing.T) {
		t.Parallel()
		s := newRouteStore()

		r1 := newRoute(10, "198.51.100.1", "198.51.100.254")
		r2 := newRoute(10, "198.51.100.2", "198.51.100.254")
		k1, k2 := newRouteKey(r1), newRouteKey(r2)

		s.Set(k1, managedRoute{route: r1})
		snap := s.Clone()
		require.Len(t, snap, 1)

		// mutate store after clone; snapshot should not change in size or keys
		s.Set(k2, managedRoute{route: r2})
		require.Len(t, s.Clone(), 2) // store changes
		require.Len(t, snap, 1)      // snapshot unchanged

		// sanity: snapshot holds the same value it had
		v1, ok := snap[k1]
		require.True(t, ok)
		require.Equal(t, r1, v1.route)
	})

	t.Run("routeStore_IsThreadSafe_UnderMixedLoad", func(t *testing.T) {
		t.Parallel()
		s := newRouteStore()

		const n = 200
		var wg sync.WaitGroup
		wg.Add(3)

		// writers
		go func() {
			defer wg.Done()
			for i := range n {
				r := newRoute(100+i, "203.0.113.1", "203.0.113.254")
				k := newRouteKey(r)
				s.Set(k, managedRoute{route: r})
			}
		}()

		// readers
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				_ = s.Len()
				_, _ = s.Get(routeKey{}) // likely miss; just exercise read lock
				_ = s.Clone()
				time.Sleep(time.Millisecond) // encourage interleaving
			}
		}()

		// deleter
		go func() {
			defer wg.Done()
			// delete some keys that may or may not exist yet
			for i := 0; i < n; i += 3 {
				r := newRoute(100+i, "203.0.113.1", "203.0.113.254")
				s.Del(newRouteKey(r))
			}
		}()

		wg.Wait()

		// Basic post-conditions: size is within [~2n/3, n] depending on race of deletes.
		ln := s.Len()
		require.GreaterOrEqual(t, ln, n/2) // be generous; depends on timing
		require.LessOrEqual(t, ln, n)
	})
}
