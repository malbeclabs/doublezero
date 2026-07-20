package qa

import (
	"strings"
	"testing"

	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
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
		name          string
		epoch         uint64
		currentSlot   uint64
		slotIndex     uint64
		slotsInEpoch  uint64
		grace         uint32
		phase         shreds.ExecutionPhase
		waitStartSlot uint64
		want          EpochTailWindow
		wantErr       string
	}{
		{
			name:          "benign: slot where the oracle closed the program, wait started in window",
			epoch:         1004,
			currentSlot:   434_157_756,
			slotIndex:     434_157_756 - firstSlot,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseUpdatingPrices,
			waitStartSlot: closeTarget + 1,
			want: EpochTailWindow{
				Benign:             true,
				Epoch:              1004,
				CurrentSlot:        434_157_756,
				WaitStartSlot:      closeTarget + 1,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:          "benign: exactly at the close target slot, wait start unknown",
			epoch:         1004,
			currentSlot:   closeTarget,
			slotIndex:     closeTarget - firstSlot,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseClosedForRequests,
			waitStartSlot: 0,
			want: EpochTailWindow{
				Benign:             true,
				Epoch:              1004,
				CurrentSlot:        closeTarget,
				WaitStartSlot:      0,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			name:          "benign: last slot before the epoch boundary, wait started exactly at close target",
			epoch:         1004,
			currentSlot:   nextEpochStart - 1,
			slotIndex:     slotsInEpoch - 1,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseUpdatingPrices,
			waitStartSlot: closeTarget,
			want: EpochTailWindow{
				Benign:             true,
				Epoch:              1004,
				CurrentSlot:        nextEpochStart - 1,
				WaitStartSlot:      closeTarget,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:          "not benign: one slot before the close target",
			epoch:         1004,
			currentSlot:   closeTarget - 1,
			slotIndex:     closeTarget - 1 - firstSlot,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseUpdatingPrices,
			waitStartSlot: 0,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1004,
				CurrentSlot:        closeTarget - 1,
				WaitStartSlot:      0,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:          "not benign: first slot of the epoch",
			epoch:         1004,
			currentSlot:   firstSlot,
			slotIndex:     0,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseClosedForRequests,
			waitStartSlot: 0,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1004,
				CurrentSlot:        firstSlot,
				WaitStartSlot:      0,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			// A run that crosses the boundary is classified against the NEW
			// epoch's window: just past the boundary is not in the tail window,
			// so a program still closed there fails loudly.
			name:          "not benign: first slot of the next epoch",
			epoch:         1005,
			currentSlot:   nextEpochStart,
			slotIndex:     0,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseUpdatingPrices,
			waitStartSlot: closeTarget + 10,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1005,
				CurrentSlot:        nextEpochStart,
				WaitStartSlot:      closeTarget + 10,
				CloseTargetSlot:    nextEpochStart + slotsInEpoch - uint64(grace),
				NextEpochStartSlot: nextEpochStart + slotsInEpoch,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// A timeout means the program was closed for the entire wait; a
			// wait that began before the close target observed a premature
			// close, which must fail loudly even though the timeout-time slot
			// is inside the window.
			name:          "not benign: wait started before the close target",
			epoch:         1004,
			currentSlot:   closeTarget + 100,
			slotIndex:     closeTarget + 100 - firstSlot,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseClosedForRequests,
			waitStartSlot: closeTarget - 1,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1004,
				CurrentSlot:        closeTarget + 100,
				WaitStartSlot:      closeTarget - 1,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			// A timeout with the program observably open now was RPC read
			// breakage, not the closed window; it must fail loudly.
			name:          "not benign: in window but program is open for requests",
			epoch:         1004,
			currentSlot:   434_157_756,
			slotIndex:     434_157_756 - firstSlot,
			slotsInEpoch:  slotsInEpoch,
			grace:         grace,
			phase:         shreds.ExecutionPhaseOpenForRequests,
			waitStartSlot: closeTarget + 1,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1004,
				CurrentSlot:        434_157_756,
				WaitStartSlot:      closeTarget + 1,
				CloseTargetSlot:    closeTarget,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   grace,
				Phase:              shreds.ExecutionPhaseOpenForRequests,
			},
		},
		{
			name:          "zero grace period: last slot of the epoch is not benign",
			epoch:         1004,
			currentSlot:   nextEpochStart - 1,
			slotIndex:     slotsInEpoch - 1,
			slotsInEpoch:  slotsInEpoch,
			grace:         0,
			phase:         shreds.ExecutionPhaseUpdatingPrices,
			waitStartSlot: 0,
			want: EpochTailWindow{
				Benign:             false,
				Epoch:              1004,
				CurrentSlot:        nextEpochStart - 1,
				WaitStartSlot:      0,
				CloseTargetSlot:    nextEpochStart,
				NextEpochStartSlot: nextEpochStart,
				GracePeriodSlots:   0,
				Phase:              shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:         "error: grace period covers the entire epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: slotsInEpoch,
			grace:        uint32(slotsInEpoch),
			phase:        shreds.ExecutionPhaseUpdatingPrices,
			wantErr:      "covers the entire epoch",
		},
		{
			name:         "error: grace period exceeds the epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: slotsInEpoch,
			grace:        500_000,
			phase:        shreds.ExecutionPhaseUpdatingPrices,
			wantErr:      "covers the entire epoch",
		},
		{
			name:         "error: zero slots in epoch",
			epoch:        1004,
			currentSlot:  firstSlot + 10,
			slotIndex:    10,
			slotsInEpoch: 0,
			grace:        grace,
			phase:        shreds.ExecutionPhaseUpdatingPrices,
			wantErr:      "slots-in-epoch is zero",
		},
		{
			name:         "error: slot index not below slots in epoch",
			epoch:        1004,
			currentSlot:  nextEpochStart,
			slotIndex:    slotsInEpoch,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			phase:        shreds.ExecutionPhaseUpdatingPrices,
			wantErr:      ">= slots in epoch",
		},
		{
			name:         "error: slot index larger than current slot",
			epoch:        1004,
			currentSlot:  100,
			slotIndex:    101,
			slotsInEpoch: slotsInEpoch,
			grace:        grace,
			phase:        shreds.ExecutionPhaseUpdatingPrices,
			wantErr:      "> current slot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyEpochTailWindow(tt.epoch, tt.currentSlot, tt.slotIndex, tt.slotsInEpoch, tt.grace, tt.phase, tt.waitStartSlot)
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
		Benign:             true,
		Epoch:              1004,
		CurrentSlot:        434_157_756,
		WaitStartSlot:      434_157_400,
		CloseTargetSlot:    434_157_750,
		NextEpochStartSlot: 434_160_000,
		GracePeriodSlots:   2250,
		Phase:              shreds.ExecutionPhaseUpdatingPrices,
	}
	want := `epoch 1004 boundary at slot 434160000, close target slot 434157750 (grace period 2250 slots), current slot 434157756, wait started at slot 434157400, program phase "updating prices"`
	if got := w.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	w.WaitStartSlot = 0
	want = `epoch 1004 boundary at slot 434160000, close target slot 434157750 (grace period 2250 slots), current slot 434157756, wait started at slot unknown, program phase "updating prices"`
	if got := w.String(); got != want {
		t.Errorf("String() with unknown wait start = %q, want %q", got, want)
	}
}
