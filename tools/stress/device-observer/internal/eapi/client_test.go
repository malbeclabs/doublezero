package eapi

import "testing"

// TestNewClientReturnsNonNil verifies NewClient does not panic and
// returns a usable Client when goeapi.Connect succeeds. Real network
// behavior is exercised via the sampler tests through a fake.
func TestNewClientReturnsNonNil(t *testing.T) {
	// Use port 0 so even if a dial were attempted it would fail
	// immediately; the existing goeapi version-probe is non-fatal and
	// returns a valid Node on connection failure.
	c, err := NewClient("127.0.0.1", "admin", "admin", 0)
	if err != nil {
		// Some goeapi versions surface dial errors here; that is OK.
		t.Logf("NewClient returned err (acceptable): %v", err)
		return
	}
	if c == nil {
		t.Fatal("NewClient returned nil client without error")
	}
	if c.node == nil {
		t.Fatal("NewClient returned Client with nil node")
	}
}
