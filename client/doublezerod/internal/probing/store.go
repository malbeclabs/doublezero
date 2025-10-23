package probing

import (
	"maps"
	"net/netip"
	"sync"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type routeKey struct {
	table   int
	dst     netip.Addr
	nextHop netip.Addr
}

func newRouteKey(route *routing.Route) routeKey {
	var dk, nk netip.Addr
	if a, ok := netip.AddrFromSlice(route.Dst.IP.To4()); ok {
		dk = a
	}
	if a, ok := netip.AddrFromSlice(route.NextHop.To4()); ok {
		nk = a
	}
	return routeKey{table: route.Table, dst: dk, nextHop: nk}
}

type managedRoute struct {
	route    *routing.Route
	liveness LivenessState
}

func (r *managedRoute) String() string {
	return r.route.String()
}

func (r *managedRoute) Key() routeKey {
	return newRouteKey(r.route)
}

// Route store: threadsafe wrapper around the managed routes map.
type routeStore struct {
	mu sync.RWMutex
	m  map[routeKey]managedRoute
}

func newRouteStore() *routeStore { return &routeStore{m: make(map[routeKey]managedRoute)} }

func (s *routeStore) Clone() map[routeKey]managedRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.m)
}
func (s *routeStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}
func (s *routeStore) Get(k routeKey) (managedRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[k]
	return v, ok
}
func (s *routeStore) Set(k routeKey, v managedRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}
func (s *routeStore) Del(k routeKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
}
