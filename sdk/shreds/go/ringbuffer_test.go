package shreds

import "testing"

// writeMetroPrices fills a ring buffer as the onchain program would: epochs are
// written in ascending order starting at slot 0, wrapping at MaxHistoryCount.
func writeMetroPrices(epochs ...uint64) MetroPriceRingBuffer {
	var buf MetroPriceRingBuffer
	for i, epoch := range epochs {
		index := i % MaxHistoryCount
		buf.Entries[index] = MetroPriceEntry{
			Epoch: epoch,
			Price: MetroPrice{USDCPriceDollars: uint16(epoch)},
		}
		buf.CurrentIndex = uint8(index)
		if buf.TotalCount < MaxHistoryCount {
			buf.TotalCount++
		}
	}
	return buf
}

func TestMetroPriceRingBufferFind(t *testing.T) {
	t.Run("exact hit on the current entry", func(t *testing.T) {
		buf := writeMetroPrices(100, 101, 102)
		entry, ok := buf.Find(102)
		if !ok {
			t.Fatal("Find(102) = not found, want found")
		}
		if entry.Price.USDCPriceDollars != 102 {
			t.Errorf("price = %d, want 102", entry.Price.USDCPriceDollars)
		}
	})

	t.Run("hit on an older entry", func(t *testing.T) {
		buf := writeMetroPrices(100, 101, 102)
		entry, ok := buf.Find(100)
		if !ok {
			t.Fatal("Find(100) = not found, want found")
		}
		if entry.Price.USDCPriceDollars != 100 {
			t.Errorf("price = %d, want 100", entry.Price.USDCPriceDollars)
		}
	})

	t.Run("wraps past index 0", func(t *testing.T) {
		// 33 writes: the ring wrapped, so CurrentIndex is 0 and epoch 132 sits at
		// slot 0 while epoch 131 sits at slot 31. Finding 131 requires the scan to
		// wrap backwards past index 0.
		buf := writeMetroPrices(seq(100, 33)...)
		if buf.CurrentIndex != 0 {
			t.Fatalf("CurrentIndex = %d, want 0", buf.CurrentIndex)
		}
		entry, ok := buf.Find(131)
		if !ok {
			t.Fatal("Find(131) = not found, want found")
		}
		if entry.Price.USDCPriceDollars != 131 {
			t.Errorf("price = %d, want 131", entry.Price.USDCPriceDollars)
		}
		// Epoch 100 was overwritten by 132 at slot 0.
		if _, ok := buf.Find(100); ok {
			t.Error("Find(100) = found, want not found (evicted by wraparound)")
		}
	})

	t.Run("does not scan beyond TotalCount", func(t *testing.T) {
		// Slot 5 holds a stale epoch outside the active window. A scan bounded by
		// the ring capacity rather than TotalCount would wrongly match it.
		buf := writeMetroPrices(100, 101)
		buf.Entries[5] = MetroPriceEntry{Epoch: 99, Price: MetroPrice{USDCPriceDollars: 99}}
		if _, ok := buf.Find(99); ok {
			t.Error("Find(99) = found, want not found (slot is beyond TotalCount)")
		}
	})

	t.Run("epoch zero misses a zero-initialized buffer", func(t *testing.T) {
		var buf MetroPriceRingBuffer
		if _, ok := buf.Find(0); ok {
			t.Error("Find(0) = found, want not found on an empty buffer")
		}
	})

	t.Run("miss", func(t *testing.T) {
		buf := writeMetroPrices(100, 101, 102)
		if _, ok := buf.Find(103); ok {
			t.Error("Find(103) = found, want not found")
		}
	})
}

func TestDeviceSubscriptionRingBufferFind(t *testing.T) {
	writeSubscriptions := func(epochs ...uint64) DeviceSubscriptionRingBuffer {
		var buf DeviceSubscriptionRingBuffer
		for i, epoch := range epochs {
			index := i % MaxHistoryCount
			buf.Entries[index] = DeviceSubscriptionEntry{
				Epoch:        epoch,
				Subscription: DeviceSubscription{GrantedSeatCount: uint16(epoch)},
			}
			buf.CurrentIndex = uint8(index)
			if buf.TotalCount < MaxHistoryCount {
				buf.TotalCount++
			}
		}
		return buf
	}

	t.Run("exact hit", func(t *testing.T) {
		buf := writeSubscriptions(100, 101, 102)
		entry, ok := buf.Find(101)
		if !ok {
			t.Fatal("Find(101) = not found, want found")
		}
		if entry.Subscription.GrantedSeatCount != 101 {
			t.Errorf("granted seats = %d, want 101", entry.Subscription.GrantedSeatCount)
		}
	})

	t.Run("wraps past index 0", func(t *testing.T) {
		buf := writeSubscriptions(seq(100, 33)...)
		entry, ok := buf.Find(131)
		if !ok {
			t.Fatal("Find(131) = not found, want found")
		}
		if entry.Subscription.GrantedSeatCount != 131 {
			t.Errorf("granted seats = %d, want 131", entry.Subscription.GrantedSeatCount)
		}
	})

	t.Run("does not scan beyond TotalCount", func(t *testing.T) {
		buf := writeSubscriptions(100, 101)
		buf.Entries[5] = DeviceSubscriptionEntry{Epoch: 99}
		if _, ok := buf.Find(99); ok {
			t.Error("Find(99) = found, want not found (slot is beyond TotalCount)")
		}
	})

	t.Run("epoch zero misses a zero-initialized buffer", func(t *testing.T) {
		var buf DeviceSubscriptionRingBuffer
		if _, ok := buf.Find(0); ok {
			t.Error("Find(0) = found, want not found on an empty buffer")
		}
	})

	t.Run("miss", func(t *testing.T) {
		buf := writeSubscriptions(100, 101, 102)
		if _, ok := buf.Find(103); ok {
			t.Error("Find(103) = found, want not found")
		}
	})
}

func seq(start uint64, count int) []uint64 {
	epochs := make([]uint64, count)
	for i := range epochs {
		epochs[i] = start + uint64(i)
	}
	return epochs
}

func TestDeviceSubscriptionUSDCPriceDollars(t *testing.T) {
	tests := []struct {
		name       string
		metroPrice uint16
		premium    int16
		want       uint16
	}{
		{"no premium", 43, 0, 43},
		{"positive premium", 43, 7, 50},
		{"negative premium is a discount", 43, -13, 30},
		{"discount saturates at zero", 10, -20, 0},
		{"most negative premium saturates at zero", 10, -32768, 0},
		{"premium saturates at u16 max", 65_000, 1_000, 65_535},
		{"max metro price with max premium saturates", 65_535, 32_767, 65_535},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := DeviceSubscription{USDCMetroPremiumDollars: tt.premium}
			metro := MetroPrice{USDCPriceDollars: tt.metroPrice}
			if got := sub.USDCPriceDollars(&metro); got != tt.want {
				t.Errorf("USDCPriceDollars() = %d, want %d", got, tt.want)
			}
		})
	}
}
