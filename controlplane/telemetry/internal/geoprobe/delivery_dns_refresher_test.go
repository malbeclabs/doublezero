package geoprobe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestDeliveryDNSRefresher_StartupRefresh(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewDeliveryDNSRefresher(log, 5*time.Minute)
	r.Cache().lookup = func(host string) ([]string, error) {
		if host == "results.example.com" {
			return []string{"93.184.216.34"}, nil
		}
		return nil, errors.New("unknown host")
	}

	r.SetDesiredHostPorts([]string{"results.example.com:9000"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Start(ctx, time.Hour)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timeout waiting for DNS refresh")
		default:
			addr, ok := r.Lookup("results.example.com:9000")
			if ok && addr.Port == 9000 && addr.IP.String() == "93.184.216.34" {
				cancel()
				<-done
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestDeliveryDNSRefresher_LookupIPWithoutWaitingForRefresh(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewDeliveryDNSRefresher(log, 5*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Start(ctx, time.Hour)

	addr, ok := r.Lookup("185.199.108.1:9000")
	if !ok {
		t.Fatal("expected literal public IP lookup without background refresh")
	}
	if addr.IP.String() != "185.199.108.1" || addr.Port != 9000 {
		t.Fatalf("unexpected addr: %v", addr)
	}
}

func TestDeliveryDNSRefresher_DomainMissBeforeRefresh(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewDeliveryDNSRefresher(log, 5*time.Minute)

	_, ok := r.Lookup("results.example.com:9000")
	if ok {
		t.Fatal("expected cache miss before refresh")
	}
}

func TestDeliveryDNSRefresher_SetDesiredTriggersRefresh(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewDeliveryDNSRefresher(log, 5*time.Minute)
	lookupCalls := 0
	r.Cache().lookup = func(host string) ([]string, error) {
		lookupCalls++
		return []string{"93.184.216.34"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Start(ctx, time.Hour)
		close(done)
	}()

	r.SetDesiredHostPorts([]string{"other.example.com:8080"})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("timeout; lookupCalls=%d", lookupCalls)
		default:
			if _, ok := r.Lookup("other.example.com:8080"); ok {
				if lookupCalls < 1 {
					t.Fatal("expected at least one DNS lookup")
				}
				cancel()
				<-done
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestLookupDeliveryUDPAddr_CachedDomain(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	cache.lookup = func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	_, ok := cache.LookupDeliveryUDPAddr("results.example.com:9000")
	if ok {
		t.Fatal("expected miss before Resolve")
	}

	_, err := cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	addr, ok := cache.LookupDeliveryUDPAddr("results.example.com:9000")
	if !ok {
		t.Fatal("expected hit after Resolve")
	}
	if addr.Port != 9000 || addr.IP.String() != "93.184.216.34" {
		t.Fatalf("addr %v", addr)
	}
}

func TestLookupDeliveryUDPAddr_LiteralIP(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	addr, ok := cache.LookupDeliveryUDPAddr("185.199.108.1:9000")
	if !ok {
		t.Fatal("expected literal IP without Resolve")
	}
	if addr.IP.String() != "185.199.108.1" || addr.Port != 9000 {
		t.Fatalf("addr %v", addr)
	}
}

func TestLookupDeliveryUDPAddr_RejectsPrivateIP(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	_, ok := cache.LookupDeliveryUDPAddr("10.0.0.1:9000")
	if ok {
		t.Fatal("expected rejection for private IP")
	}
}

func TestLookupDeliveryUDPAddr_ExpiredCache(t *testing.T) {
	now := time.Now()
	cache := NewDNSCache(5 * time.Minute)
	cache.now = func() time.Time { return now }
	cache.lookup = func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	_, err := cache.Resolve("results.example.com:9000")
	if err != nil {
		t.Fatal(err)
	}

	_, ok := cache.LookupDeliveryUDPAddr("results.example.com:9000")
	if !ok {
		t.Fatal("expected hit before expiry")
	}

	now = now.Add(6 * time.Minute)
	_, ok = cache.LookupDeliveryUDPAddr("results.example.com:9000")
	if ok {
		t.Fatal("expected miss after TTL")
	}
}

func TestLookupDeliveryUDPAddr_InvalidHostPort(t *testing.T) {
	cache := NewDNSCache(5 * time.Minute)
	_, ok := cache.LookupDeliveryUDPAddr("not-a-host-port")
	if ok {
		t.Fatal("expected invalid")
	}
}
