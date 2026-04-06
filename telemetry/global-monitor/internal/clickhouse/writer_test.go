package clickhouse

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriter_Append_BuffersRows(t *testing.T) {
	w := &Writer{}

	ts := time.Unix(1700000000, 0)

	w.AppendSolanaValidatorICMPProbe(SolanaValidatorICMPProbeRow{
		Timestamp:       ts,
		ProbeType:       "icmp",
		ValidatorPubkey: "val1",
	})
	w.AppendSolanaValidatorICMPProbe(SolanaValidatorICMPProbeRow{
		Timestamp:       ts,
		ProbeType:       "icmp",
		ValidatorPubkey: "val2",
	})

	w.AppendSolanaValidatorTPUQUICProbe(SolanaValidatorTPUQUICProbeRow{
		Timestamp:       ts,
		ProbeType:       "tpuquic",
		ValidatorPubkey: "val3",
	})

	w.AppendDoubleZeroUserICMPProbe(DoubleZeroUserICMPProbeRow{
		Timestamp:  ts,
		ProbeType:  "icmp",
		UserPubkey: "user1",
	})
	w.AppendDoubleZeroUserICMPProbe(DoubleZeroUserICMPProbeRow{
		Timestamp:  ts,
		ProbeType:  "icmp",
		UserPubkey: "user2",
	})
	w.AppendDoubleZeroUserICMPProbe(DoubleZeroUserICMPProbeRow{
		Timestamp:  ts,
		ProbeType:  "icmp",
		UserPubkey: "user3",
	})

	w.mu.Lock()
	require.Len(t, w.solICMPRows, 2)
	require.Len(t, w.solTPUQUICRows, 1)
	require.Len(t, w.dzUserICMPRows, 3)
	require.Equal(t, "val1", w.solICMPRows[0].ValidatorPubkey)
	require.Equal(t, "val2", w.solICMPRows[1].ValidatorPubkey)
	require.Equal(t, "val3", w.solTPUQUICRows[0].ValidatorPubkey)
	require.Equal(t, "user1", w.dzUserICMPRows[0].UserPubkey)
	require.Equal(t, "user3", w.dzUserICMPRows[2].UserPubkey)
	w.mu.Unlock()
}

func TestWriter_Append_ConcurrentSafety(t *testing.T) {
	w := &Writer{}

	ts := time.Unix(1700000000, 0)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			w.AppendSolanaValidatorICMPProbe(SolanaValidatorICMPProbeRow{Timestamp: ts})
		}()
		go func() {
			defer wg.Done()
			w.AppendSolanaValidatorTPUQUICProbe(SolanaValidatorTPUQUICProbeRow{Timestamp: ts})
		}()
		go func() {
			defer wg.Done()
			w.AppendDoubleZeroUserICMPProbe(DoubleZeroUserICMPProbeRow{Timestamp: ts})
		}()
	}

	wg.Wait()

	w.mu.Lock()
	require.Len(t, w.solICMPRows, 100)
	require.Len(t, w.solTPUQUICRows, 100)
	require.Len(t, w.dzUserICMPRows, 100)
	w.mu.Unlock()
}

func TestWriter_ImplementsProbeWriter(t *testing.T) {
	var _ ProbeWriter = (*Writer)(nil)
}
