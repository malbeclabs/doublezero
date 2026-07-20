package qa

import (
	"strings"
	"testing"
)

func TestClassifyEpochTailWindow(t *testing.T) {
	// Constants from the verified mainnet example (2026-07-20, epoch 1004→1005):
	// epoch 1004 starts at slot 433,728,000, epoch 1005 at 434,160,000, and the
	// onchain grace period was 2250 slots, giving a close target of 434,157,750.
	const (
		slotsInEpoch   = uint64(432_000)
		firstSlot      = uint64(433_728_000)
		nextEpochStart = uint64(434_160_000)
		grace          = uint32(2250)
		closeTarget    = uint64(434_157_750)
	)

	tests := []struct {
		name         string
		epoch        uint64
		currentSlot  uint64
		slotIndex    uint64
		slotsInEpoch uint64
		grace        uint32
		want         EpochTailWindow
		wantErr      string
	}{
		{
			name:         "inside window: slot where the oracle closed the program",
			epoch:        1004,
			currentSlot:  434_157_756,
			slotIndex:    434_157_756 - firstSlot,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           true,
				Epoch:              1004,
				CurrentSlot:        434_157_756,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
			},
		},
		{
			name:         "inside window: exactly at the close target slot",
			epoch:        1004,
			currentSlot:  closeTarget,
			slotIndex:    closeTarget - firstSlot,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           true,
				Epoch:              1004,
				CurrentSlot:        closeTarget,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
			},
		},
		{
			name:         "inside window: last slot before the epoch boundary",
			epoch:        1004,
			currentSlot:  nextEpochStart - 1,
			slotIndex:    slotsInEpoch - 1,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           true,
				Epoch:              1004,
				CurrentSlot:        nextEpochStart - 1,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
			},
		},
		{
			name:         "outside window: one slot before the close target",
			epoch:        1004,
			currentSlot:  closeTarget - 1,
			slotIndex:    closeTarget - 1 - firstSlot,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           false,
				Epoch:              1004,
				CurrentSlot:        closeTarget - 1,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
			},
		},
		{
			name:         "outside window: first slot of the epoch",
			epoch:        1004,
			currentSlot:  firstSlot,
			slotIndex:    0,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           false,
				Epoch:              1004,
				CurrentSlot:        firstSlot,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
			},
		},
		{
			// A run that crosses the boundary is classified against the NEW
			// epoch's window: just past the boundary is not in the tail window,
			// so a program still closed there fails loudly.
			name:         "outside window: first slot of the next epoch",
			epoch:        1005,
			currentSlot:  nextEpochStart,
			slotIndex:    0,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			want: EpochTailWindow{
				InWindow:           false,
				Epoch:              1005,
				CurrentSlot:        nextEpochStart,
				CloseTargetSlot:    nextEpochStart + slotsInEpoch - uint64(grace),
				NextEpochStartSlot: nextEpochStart + slotsInEpoch,
				GracePeriodSlots:   grace,
			},
		},
		{
			name:         "zero grace period: last slot of the epoch is not in window",
			epoch:        1004,
			currentSlot:  nextEpochStart - 1,
			slotIndex:    slotsInEpoch - 1,
			slotsInEpoch: slotsInEpoch,
			grace:        0,
			want: EpochTailWindow{
				InWindow:           false,
				Epoch:              1004,
				CurrentSlot:        nextEpochStart - 1,
				CloseTargetSlot:    nextEpochStart,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   0,
			},
		},
		{
			name:         "error: grace period covers the entire epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: slotsInEpoch,
			grace:        uint32(slotsInEpoch),
			wantErr:      "covers the entire epoch",
		},
		{
			name:         "error: grace period exceeds the epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: slotsInEpoch,
			grace:        500_000,
			wantErr:      "covers the entire epoch",
		},
		{
			name:         "error: zero slots in epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: 0,
			grace:        grace,
			wantErr:      "slots-in-epoch is zero",
		},
		{
			name:         "error: slot index not below slots in epoch",
			epoch:        1004,
			currentSlot:  nextEpochStart,
			slotIndex:    slotsInEpoch,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			wantErr:      "inconsistent epoch info",
		},
		{
			name:         "error: slot index larger than current slot",
			epoch:        1004,
			currentSlot:  100,
			slotIndex:    101,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			wantErr:      "inconsistent epoch info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyEpochTailWindow(tt.epoch, tt.currentSlot, tt.slotIndex, tt.slotsInEpoch, tt.grace)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("classifyEpochTailWindow() = %+v, want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("classifyEpochTailWindow() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyEpochTailWindow() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("classifyEpochTailWindow() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEpochTailWindowString(t *testing.T) {
	w := EpochTailWindow{
		InWindow:           true,
		Epoch:              1004,
		CurrentSlot:        434_157_756,
		CloseTargetSlot:    434_157_750,
		NextEpochStartSlot: 434_160_000,
		GracePeriodSlots:   2250,
	}
	want := "epoch 1004 boundary at slot 434160000, close target slot 434157750 (grace period 2250 slots), current slot 434157756"
	if got := w.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
