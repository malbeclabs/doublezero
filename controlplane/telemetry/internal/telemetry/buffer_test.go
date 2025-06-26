package telemetry_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
)

func TestAgentTelemetry_Buffer(t *testing.T) {
	t.Parallel()

	t.Run("Add and Read returns expected sample", func(t *testing.T) {
		t.Parallel()

		buf := telemetry.NewSampleBuffer(10)
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			Link:      "link1",
			Device:    "device1",
			RTT:       42 * time.Millisecond,
			Loss:      false,
		}
		buf.Add(sample)

		samples := buf.Read()
		if len(samples) != 1 {
			t.Fatalf("expected 1 sample, got %d", len(samples))
		}
		if samples[0] != sample {
			t.Errorf("expected sample %+v, got %+v", sample, samples[0])
		}
	})

	t.Run("Read returns copy not shared with buffer", func(t *testing.T) {
		t.Parallel()

		buf := telemetry.NewSampleBuffer(10)
		s1 := telemetry.Sample{Device: "a"}
		buf.Add(s1)

		s := buf.Read()
		s[0].Device = "mutated"

		s2 := buf.Read()
		if s2[0].Device != "a" {
			t.Errorf("expected original sample to remain unchanged, got %+v", s2[0])
		}
	})
}
