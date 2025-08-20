package twamplight

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ntpEpochOffset is the number of seconds between Jan 1, 1900 (NTP epoch) and Jan 1, 1970 (Unix epoch).
// This constant is duplicated here for testing purposes to ensure the test file is self-contained.
// Make it explicitly uint32 to match the calculated value's type.
const ntpEpochOffset uint32 = 2208988800

func TestTWAMP_NTPTimestamp(t *testing.T) {
	type testCase struct {
		name         string
		inputTime    time.Time
		expectedSec  uint32
		expectedFrac uint32
		// deltaFrac allows for a small acceptable error range for fractional part due to integer division
		deltaFrac uint32
	}

	t1900 := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	t1970 := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	calculatedEpochOffset := uint32(t1970.Sub(t1900).Seconds())

	tests := []testCase{
		{
			name:         "Unix epoch (1970-01-01 00:00:00 UTC)",
			inputTime:    time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedSec:  ntpEpochOffset,
			expectedFrac: 0,
			deltaFrac:    0,
		},
		{
			name:         "NTP epoch (1900-01-01 00:00:00 UTC)",
			inputTime:    time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedSec:  0,
			expectedFrac: 0,
			deltaFrac:    0,
		},
		{
			name:         "Specific time with 0.5 seconds (500,000,000 nanoseconds)",
			inputTime:    time.Date(2023, 1, 1, 12, 30, 45, 500*1e6, time.UTC),
			expectedSec:  uint32(time.Date(2023, 1, 1, 12, 30, 45, 0, time.UTC).Unix()) + ntpEpochOffset,
			expectedFrac: uint32(math.Pow(2, 32) / 2), // 2^32 / 2 = 2147483648
			deltaFrac:    0,
		},
		{
			name:      "Max nanoseconds (999,999,999)",
			inputTime: time.Date(2023, 1, 1, 0, 0, 0, 999999999, time.UTC),
			// Expected frac: (MAX9999 * 2^32) / 1e9 = 4294967291.705032704 -> 4294967291
			expectedSec:  uint32(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Unix()) + ntpEpochOffset,
			expectedFrac: uint32((uint64(999999999) * (1 << 32)) / 1e9),
			deltaFrac:    0, // No delta needed as it's exact calculation
		},
		{
			name:         "Zero nanoseconds",
			inputTime:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedSec:  uint32(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Unix()) + ntpEpochOffset,
			expectedFrac: 0,
			deltaFrac:    0,
		},
		{
			name:        "Specific microsecond value (250 microseconds)",
			inputTime:   time.Date(2023, 1, 1, 0, 0, 0, 250*1000, time.UTC), // 250,000 nanoseconds
			expectedSec: uint32(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Unix()) + ntpEpochOffset,
			// Expected frac: (250000 * 2^32) / 1e9 = 107374182.4 -> 107374182
			expectedFrac: uint32((uint64(250000) * (1 << 32)) / 1e9),
			deltaFrac:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sec, frac := ntpTimestamp(tc.inputTime)

			assert.Equal(t, tc.expectedSec, sec, "Seconds mismatch for %s", tc.name)

			if tc.deltaFrac == 0 {
				assert.Equal(t, tc.expectedFrac, frac, "Fractional part mismatch for %s", tc.name)
			} else {
				// Use assert.InDelta for floating-point comparisons
				assert.InDelta(t, tc.expectedFrac, frac, float64(tc.deltaFrac), "Fractional part mismatch (with delta) for %s", tc.name)
			}
		})
	}

	t.Run("Monotonicity Check", func(t *testing.T) {
		t1 := time.Date(2023, 1, 1, 10, 0, 0, 1000000, time.UTC) // 1ms
		t2 := time.Date(2023, 1, 1, 10, 0, 0, 2000000, time.UTC) // 2ms
		t3 := time.Date(2023, 1, 1, 10, 0, 1, 0, time.UTC)       // 1s later

		sec1, frac1 := ntpTimestamp(t1)
		sec2, frac2 := ntpTimestamp(t2)
		sec3, frac3 := ntpTimestamp(t3)

		// Assert that (sec2, frac2) is greater than or equal to (sec1, frac1)
		if sec2 < sec1 || (sec2 == sec1 && frac2 < frac1) {
			assert.Fail(t, "Monotonicity check failed: t2 should be >= t1", "t2 (%v) NTP (%d,%d) should be >= t1 (%v) NTP (%d,%d)",
				t2, sec2, frac2, t1, sec1, frac1)
		}
		// Assert that (sec3, frac3) is greater than or equal to (sec2, frac2)
		if sec3 < sec2 || (sec3 == sec2 && frac3 < frac2) {
			assert.Fail(t, "Monotonicity check failed: t3 should be >= t2", "t3 (%v) NTP (%d,%d) should be >= t2 (%v) NTP (%d,%d)",
				t3, sec3, frac3, t2, sec2, frac2)
		}
	})

	t.Run("NTP Epoch Offset Constant Verification", func(t *testing.T) {
		// Explicitly cast both to uint32 to ensure consistent comparison and display.
		assert.Equal(t, ntpEpochOffset, calculatedEpochOffset, "ntpEpochOffset constant is incorrect")
	})

	t.Run("Leap Second Scenario (Go's perspective)", func(t *testing.T) {
		tBeforeLeap := time.Date(2015, 6, 30, 23, 59, 59, 0, time.UTC)
		tAfterLeap := time.Date(2015, 7, 1, 0, 0, 0, 0, time.UTC)

		secBefore, _ := ntpTimestamp(tBeforeLeap)
		secAfter, fracAfter := ntpTimestamp(tAfterLeap)

		assert.Equal(t, secBefore+1, secAfter, "Seconds part mismatch for leap second scenario")
		assert.Equal(t, uint32(0), fracAfter, "Fractional part mismatch for leap second scenario after jump")
	})
}

// FuzzTWAMP_NTPTimestamp is a fuzz test to continuously test ntpTimestamp with random inputs.
// This helps catch edge cases that specific tests might miss.
func FuzzTWAMP_NTPTimestamp(f *testing.F) {
	// Add some seed values, including boundary conditions and specific times.
	f.Add(int64(0)) // Unix epoch
	f.Add(int64(1))
	f.Add(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()) // NTP epoch (negative UnixNano if before 1970)
	f.Add(time.Date(2023, 1, 1, 12, 30, 45, 123456789, time.UTC).UnixNano())
	f.Add(time.Date(2023, 1, 1, 12, 30, 45, 999999999, time.UTC).UnixNano()) // Max nanoseconds
	f.Add(time.Date(2023, 1, 1, 12, 30, 45, 0, time.UTC).UnixNano())         // Zero nanoseconds
	f.Add(time.Now().UnixNano())
	f.Add(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()) // Future date

	f.Fuzz(func(t *testing.T, unixNano int64) {
		const (
			minUnixNano = -2208988800 * 1e9 // Approx. NTP epoch in UnixNano
			maxUnixNano = 3000000000 * 1e9  // Approx. Year 2065 in UnixNano
		)

		if unixNano < minUnixNano || unixNano > maxUnixNano {
			t.Skip() // Skip values outside the practical range to avoid time.Unix errors
		}

		testTime := time.Unix(unixNano/int64(time.Second), unixNano%int64(time.Second))

		sec, frac := ntpTimestamp(testTime)

		assert.LessOrEqual(t, frac, uint32(math.MaxUint32), "Fractional part out of range")

		reconstructedNanosFloat := float64(frac) * 1e9 / math.Pow(2, 32)
		reconstructedNanos := uint64(reconstructedNanosFloat)

		originalNanos := uint64(testTime.Nanosecond())

		const allowedNanosDelta = 1000 // 1 microsecond

		assert.InDelta(t, float64(originalNanos), float64(reconstructedNanos), float64(allowedNanosDelta),
			"Large discrepancy in fractional part reconstruction for time %v (UnixNano: %d)", testTime, unixNano)

		expectedSecFuzz := uint32(testTime.Unix()) + ntpEpochOffset
		assert.Equal(t, expectedSecFuzz, sec, "Seconds part mismatch for time %v", testTime)
	})
}
