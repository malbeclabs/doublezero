package probing

import (
	"maps"
	"sync"
)

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
