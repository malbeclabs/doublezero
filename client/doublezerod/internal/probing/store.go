package probing

import (
	"fmt"
	"maps"
	"net/netip"
	"sync"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteKey struct {
	table   int
	dst     netip.Addr
	nextHop netip.Addr
}

func newRouteKey(route *routing.Route) RouteKey {
	var dk, nk netip.Addr
	if a, ok := netip.AddrFromSlice(route.Dst.IP.To4()); ok {
		dk = a
	}
	if a, ok := netip.AddrFromSlice(route.NextHop.To4()); ok {
		nk = a
	}
	return RouteKey{table: route.Table, dst: dk, nextHop: nk}
}

func (k RouteKey) String() string {
	return fmt.Sprintf("RouteKey{table: %d, dst: %s, nextHop: %s}", k.table, k.dst, k.nextHop)
}

type managedRoute struct {
	route    *routing.Route
	liveness LivenessTracker
}

func (r *managedRoute) String() string {
	return r.route.String()
}

func (r *managedRoute) Key() RouteKey {
	return newRouteKey(r.route)
}

// Route store: threadsafe wrapper around the managed routes map.
type routeStore struct {
	mu sync.RWMutex
	m  map[RouteKey]managedRoute
}

func newRouteStore() *routeStore { return &routeStore{m: make(map[RouteKey]managedRoute)} }

func (s *routeStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[RouteKey]managedRoute)
}

func (s *routeStore) Clone() map[RouteKey]managedRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.m)
}

func (s *routeStore) Keys() []RouteKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]RouteKey, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	return keys
}

func (s *routeStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func (s *routeStore) Get(k RouteKey) (managedRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}

func (s *routeStore) Set(k RouteKey, v managedRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *routeStore) Del(k RouteKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
}
