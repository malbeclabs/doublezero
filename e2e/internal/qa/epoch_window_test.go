package qa

import (
	"strings"
	"testing"

	"github.com/gagliardetto/solana-go/rpc"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

func TestClassifyEpochTailWindow(t *testing.T) {
	// Constants from the verified mainnet example (2026-07-20, epoch 1004→1005):
	// epoch 1004 starts at slot 433,728,000, epoch 1005 at 434,160,000, and the
	// onchain grace period was 2250 slots, giving a close target of 434,157,750.
	// The oracle stamped LastClosedForRequestsSlot at 434,157,756, six slots
	// past the target.
	const (
		slotsInEpoch   = uint64(432_000)
		firstSlot      = uint64(433_728_000)
		nextEpochStart = uint64(434_160_000)
		grace          = uint32(2250)
		closeTarget    = uint64(434_157_750)
		closedSlot     = uint64(434_157_756)
	)

	tests := []struct {
		name           string
		epoch          uint64
		currentSlot    uint64
		slotIndex      uint64
		slotsInEpoch   uint64
		grace          uint32
		phase          shreds.ExecutionPhase
		lastClosedSlot uint64
		waitStartSlot  uint64
		want           EpochTailWindow
		wantErr        string
	}{
		{
			name:           "benign: slot where the oracle closed the program, wait started in window",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget + 1,
			want: EpochTailWindow{
				Benign:                    true,
				Epoch:                     1004,
				CurrentSlot:               closedSlot,
				WaitStartSlot:             closeTarget + 1,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closedSlot,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:           "benign: exactly at the close target slot, wait start unknown",
			epoch:          1004,
			currentSlot:    closeTarget,
			slotIndex:      closeTarget - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseClosedForRequests,
			lastClosedSlot: closeTarget,
			waitStartSlot:  0,
			want: EpochTailWindow{
				Benign:                    true,
				Epoch:                     1004,
				CurrentSlot:               closeTarget,
				WaitStartSlot:             0,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closeTarget,
				Phase:                     shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			name:           "benign: last slot before the epoch boundary, wait started exactly at close target",
			epoch:          1004,
			currentSlot:    nextEpochStart - 1,
			slotIndex:      slotsInEpoch - 1,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget,
			want: EpochTailWindow{
				Benign:                    true,
				Epoch:                     1004,
				CurrentSlot:               nextEpochStart - 1,
				WaitStartSlot:             closeTarget,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closedSlot,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// lastClosedSlot is placed in-window so only the current-slot gate
			// fails; the other cases below isolate the remaining gates the same
			// way.
			name:           "not benign: one slot before the close target",
			epoch:          1004,
			currentSlot:    closeTarget - 1,
			slotIndex:      closeTarget - 1 - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closeTarget,
			waitStartSlot:  0,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               closeTarget - 1,
				WaitStartSlot:             0,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closeTarget,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			name:           "not benign: first slot of the epoch",
			epoch:          1004,
			currentSlot:    firstSlot,
			slotIndex:      0,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseClosedForRequests,
			lastClosedSlot: closeTarget,
			waitStartSlot:  0,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               firstSlot,
				WaitStartSlot:             0,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closeTarget,
				Phase:                     shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			// A run that crosses the boundary is classified against the NEW
			// epoch's window: just past the boundary is not in the tail window
			// (and the last close is the previous epoch's), so a program still
			// closed there fails loudly.
			name:           "not benign: first slot of the next epoch",
			epoch:          1005,
			currentSlot:    nextEpochStart,
			slotIndex:      0,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget + 10,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1005,
				CurrentSlot:               nextEpochStart,
				WaitStartSlot:             closeTarget + 10,
				CloseTargetSlot:           nextEpochStart + slotsInEpoch - uint64(grace),
				NextEpochStartSlot:        nextEpochStart + slotsInEpoch,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closedSlot,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// A timeout means the program was closed for the entire wait; a
			// wait that began before the close target observed a premature
			// close, which must fail loudly even though the timeout-time slot
			// is inside the window.
			name:           "not benign: wait started before the close target",
			epoch:          1004,
			currentSlot:    closeTarget + 100,
			slotIndex:      closeTarget + 100 - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseClosedForRequests,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget - 1,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               closeTarget + 100,
				WaitStartSlot:             closeTarget - 1,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closedSlot,
				Phase:                     shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			// An oracle that died mid-epoch leaves the program closed with a
			// stale LastClosedForRequestsSlot; a wait landing entirely inside
			// the tail window must still fail loudly — this is the gate that
			// distinguishes "closed while in the window" from "closed BY this
			// epoch's scheduled close". Wait start unknown, so this also pins
			// the gate in the degraded classification mode.
			name:           "not benign: last close long before the target (oracle died mid-epoch)",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseClosedForRequests,
			lastClosedSlot: firstSlot + 100_000,
			waitStartSlot:  0,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               closedSlot,
				WaitStartSlot:             0,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: firstSlot + 100_000,
				Phase:                     shreds.ExecutionPhaseClosedForRequests,
			},
		},
		{
			// A close stamped even one slot before the target is early — an
			// anomaly, not the scheduled close.
			name:           "not benign: last close one slot before the target",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closeTarget - 1,
			waitStartSlot:  closeTarget + 1,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               closedSlot,
				WaitStartSlot:             closeTarget + 1,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closeTarget - 1,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// A close stamped at or past the epoch boundary belongs to a later
			// epoch than the one being classified — an inconsistent view.
			name:           "not benign: last close at the epoch boundary",
			epoch:          1004,
			currentSlot:    nextEpochStart - 1,
			slotIndex:      slotsInEpoch - 1,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: nextEpochStart,
			waitStartSlot:  closeTarget,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               nextEpochStart - 1,
				WaitStartSlot:             closeTarget,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: nextEpochStart,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// A timeout with the program observably open now was RPC read
			// breakage, not the closed window; it must fail loudly.
			name:           "not benign: in window but program is open for requests",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseOpenForRequests,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget + 1,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               closedSlot,
				WaitStartSlot:             closeTarget + 1,
				CloseTargetSlot:           closeTarget,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          grace,
				LastClosedForRequestsSlot: closedSlot,
				Phase:                     shreds.ExecutionPhaseOpenForRequests,
			},
		},
		{
			name:           "zero grace period: last slot of the epoch is not benign",
			epoch:          1004,
			currentSlot:    nextEpochStart - 1,
			slotIndex:      slotsInEpoch - 1,
			slotsInEpoch:   slotsInEpoch,
			grace:          0,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: 0,
			waitStartSlot:  0,
			want: EpochTailWindow{
				Benign:                    false,
				Epoch:                     1004,
				CurrentSlot:               nextEpochStart - 1,
				WaitStartSlot:             0,
				CloseTargetSlot:           nextEpochStart,
				NextEpochStartSlot:        nextEpochStart,
				GracePeriodSlots:          0,
				LastClosedForRequestsSlot: 0,
				Phase:                     shreds.ExecutionPhaseUpdatingPrices,
			},
		},
		{
			// An unknown phase byte (program upgrade, deserialization drift)
			// must not gate a benign skip.
			name:           "error: unknown execution phase",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhase(3),
			lastClosedSlot: closedSlot,
			waitStartSlot:  closeTarget + 1,
			wantErr:        "unknown execution phase 3",
		},
		{
			// The wait ran for minutes before classification, so its start slot
			// ahead of the classification-time slot means the slot view is
			// provably stale — exactly when the window position can't be
			// trusted.
			name:           "error: wait start slot ahead of current slot",
			epoch:          1004,
			currentSlot:    closedSlot,
			slotIndex:      closedSlot - firstSlot,
			slotsInEpoch:   slotsInEpoch,
			grace:          grace,
			phase:          shreds.ExecutionPhaseUpdatingPrices,
			lastClosedSlot: closedSlot,
			waitStartSlot:  closedSlot + 1,
			wantErr:        "wait start slot",
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
			info := &rpc.GetEpochInfoResult{
				Epoch:        tt.epoch,
				AbsoluteSlot: tt.currentSlot,
				SlotIndex:    tt.slotIndex,
				SlotsInEpoch: tt.slotsInEpoch,
			}
			ec := &shreds.ExecutionController{
				Phase:                     uint8(tt.phase),
				LastClosedForRequestsSlot: tt.lastClosedSlot,
			}
			got, err := classifyEpochTailWindow(info, tt.grace, ec, tt.waitStartSlot)
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

func TestClassifyEpochTailWindowNilInputs(t *testing.T) {
	info := &rpc.GetEpochInfoResult{Epoch: 1004, AbsoluteSlot: 100, SlotIndex: 100, SlotsInEpoch: 432_000}
	ec := &shreds.ExecutionController{Phase: uint8(shreds.ExecutionPhaseUpdatingPrices)}

	if _, err := classifyEpochTailWindow(nil, 2250, ec, 0); err == nil || !strings.Contains(err.Error(), "nil epoch info") {
		t.Errorf("classifyEpochTailWindow(nil info) error = %v, want nil epoch info error", err)
	}
	if _, err := classifyEpochTailWindow(info, 2250, nil, 0); err == nil || !strings.Contains(err.Error(), "nil execution controller") {
		t.Errorf("classifyEpochTailWindow(nil controller) error = %v, want nil execution controller error", err)
	}
}

func TestEpochTailWindowString(t *testing.T) {
	w := EpochTailWindow{
		Benign:                    true,
		Epoch:                     1004,
		CurrentSlot:               434_157_756,
		WaitStartSlot:             434_157_400,
		CloseTargetSlot:           434_157_750,
		NextEpochStartSlot:        434_160_000,
		GracePeriodSlots:          2250,
		LastClosedForRequestsSlot: 434_157_756,
		Phase:                     shreds.ExecutionPhaseUpdatingPrices,
	}
	want := `epoch 1004 boundary at slot 434160000, close target slot 434157750 (grace period 2250 slots), current slot 434157756, wait started at slot 434157400, last closed at slot 434157756, program phase "updating prices"`
	if got := w.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	w.WaitStartSlot = 0
	want = `epoch 1004 boundary at slot 434160000, close target slot 434157750 (grace period 2250 slots), current slot 434157756, wait started at slot unknown, last closed at slot 434157756, program phase "updating prices"`
	if got := w.String(); got != want {
		t.Errorf("String() with unknown wait start = %q, want %q", got, want)
	}
}
