package qa

import (
	"math/rand"
	"testing"
)

func TestRandomNonAllocateAddrClient(t *testing.T) {
	plain := &Client{Host: "plain"}
	alloc := &Client{Host: "alloc", AllocateAddr: true}
	test := &Test{clients: map[string]*Client{"plain": plain, "alloc": alloc}, rand: rand.New(rand.NewSource(1))}
	for range 20 {
		if got := test.RandomNonAllocateAddrClient(); got != plain {
			t.Fatalf("expected plain client, got %v", got)
		}
	}

	test.clients = map[string]*Client{"alloc": alloc}
	if got := test.RandomNonAllocateAddrClient(); got != nil {
		t.Fatalf("expected nil when every client uses allocate-addr, got %v", got)
	}
}
