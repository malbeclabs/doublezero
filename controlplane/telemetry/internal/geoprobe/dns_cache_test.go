package geoprobe

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDNSCache_ResolveIPAddress(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)

	addr, err := cache.Resolve("185.199.108.1:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr.IP.String() != "185.199.108.1" {
		t.Errorf("expected IP 185.199.108.1, got %s", addr.IP)
	}
	if addr.Port != 9000 {
		t.Errorf("expected port 9000, got %d", addr.Port)
	}
}

func TestDNSCache_ResolveIPAddress_NoCaching(t *testing.T) {
	lookupCalls := 0
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		lookupCalls++
		return []string{"1.2.3.4"}, nil
	}

	// IP addresses should not trigger DNS lookup.
	for i := 0; i < 3; i++ {
		_, err := cache.Resolve("10.0.0.1:8080")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if lookupCalls != 0 {
		t.Errorf("expected 0 DNS lookups for IP address, got %d", lookupCalls)
	}
}

func TestDNSCache_ResolveDomain(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		if host == "results.example.com" {
			return []string{"93.184.216.34"}, nil
		}
		return nil, fmt.Errorf("unknown host: %s", host)
	}

	addr, err := cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr.IP.String() != "93.184.216.34" {
		t.Errorf("expected IP 93.184.216.34, got %s", addr.IP)
	}
	if addr.Port != 9000 {
		t.Errorf("expected port 9000, got %d", addr.Port)
	}
}

func TestDNSCache_CachesDNSLookup(t *testing.T) {
	lookupCalls := 0
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		lookupCalls++
		return []string{"93.184.216.34"}, nil
	}

	// First call triggers lookup.
	_, err := cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("expected 1 lookup call, got %d", lookupCalls)
	}

	// Second call should use cache.
	_, err = cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookupCalls != 1 {
		t.Errorf("expected 1 lookup call (cached), got %d", lookupCalls)
	}
}

func TestDNSCache_TTLExpiry(t *testing.T) {
	lookupCalls := 0
	now := time.Now()

	cache := NewDNSCache(5 * time.Minute)
	cache.now = func() time.Time { return now }
	cache.lookup = func(host string) ([]string, error) {
		lookupCalls++
		return []string{"93.184.216.34"}, nil
	}

	// First lookup.
	_, err := cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("expected 1 lookup, got %d", lookupCalls)
	}

	// Advance past TTL.
	now = now.Add(6 * time.Minute)

	// Should trigger new lookup.
	_, err = cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookupCalls != 2 {
		t.Errorf("expected 2 lookups after TTL expiry, got %d", lookupCalls)
	}
}

func TestDNSCache_LookupFailure(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		return nil, fmt.Errorf("dns: NXDOMAIN")
	}

	_, err := cache.Resolve("nonexistent.example.com:9000")
	if err == nil {
		t.Fatal("expected error for failed DNS lookup")
	}
}

func TestDNSCache_InvalidAddress(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)

	tests := []struct {
		name    string
		address string
	}{
		{"no port", "example.com"},
		{"empty", ""},
		{"invalid port", "example.com:abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cache.Resolve(tt.address)
			if err == nil {
				t.Error("expected error for invalid address")
			}
		})
	}
}

func TestDNSCache_ConcurrentAccess(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.Resolve("results.example.com:9000")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestDNSCache_DifferentDomains(t *testing.T) {
	lookupCalls := make(map[string]int)
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		lookupCalls[host]++
		return []string{"1.2.3.4"}, nil
	}

	_, _ = cache.Resolve("a.example.com:9000")
	_, _ = cache.Resolve("b.example.com:9000")
	_, _ = cache.Resolve("a.example.com:9000") // cached

	if lookupCalls["a.example.com"] != 1 {
		t.Errorf("expected 1 lookup for a.example.com, got %d", lookupCalls["a.example.com"])
	}
	if lookupCalls["b.example.com"] != 1 {
		t.Errorf("expected 1 lookup for b.example.com, got %d", lookupCalls["b.example.com"])
	}
}

func TestDNSCache_DifferentPortsSameDomain(t *testing.T) {
	lookupCalls := 0
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		lookupCalls++
		return []string{"93.184.216.34"}, nil
	}

	addr1, _ := cache.Resolve("results.example.com:9000")
	addr2, _ := cache.Resolve("results.example.com:8080")

	// Same domain should only be resolved once.
	if lookupCalls != 1 {
		t.Errorf("expected 1 lookup for same domain different ports, got %d", lookupCalls)
	}
	if addr1.Port != 9000 {
		t.Errorf("expected port 9000, got %d", addr1.Port)
	}
	if addr2.Port != 8080 {
		t.Errorf("expected port 8080, got %d", addr2.Port)
	}
}
