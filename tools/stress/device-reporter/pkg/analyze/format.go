package analyze

import (
	"fmt"
	"time"
)

// FormatInt formats with thousand-separators for readability in tables.
// Exported so the format package's markdown writer can render the same
// way the analyze-internal `compare` rows do.
func FormatInt(n int) string {
	if n < 0 {
		return "-" + FormatInt(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return FormatInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}

// FormatDuration renders a duration with units that fit the magnitude.
// Zero durations render as "—" so empty cells are visually obvious in
// tables. For fields where 0 is a deliberate operator choice (e.g. the
// Hold knob), use FormatDurationOrZero instead.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	return formatNonZeroDuration(d)
}

// FormatDurationOrZero renders 0 as "0s" rather than "—". Use this for
// fields where the operator explicitly chose zero (e.g. `--hold 0`),
// since the em-dash reads as "data missing" rather than "no hold".
func FormatDurationOrZero(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	return formatNonZeroDuration(d)
}

// FormatBytes renders a byte count in IEC units (KiB, MiB, GiB). Used
// for memory readouts where the operator wants something easier to
// eyeball than a 10-digit kilobyte total. Zero renders as "—" so the
// "missing data" convention is consistent with FormatDuration.
func FormatBytes(b uint64) string {
	if b == 0 {
		return "—"
	}
	const unit = 1024.0
	f := float64(b)
	switch {
	case f < unit:
		return fmt.Sprintf("%d B", b)
	case f < unit*unit:
		return fmt.Sprintf("%.1f KiB", f/unit)
	case f < unit*unit*unit:
		return fmt.Sprintf("%.1f MiB", f/(unit*unit))
	default:
		return fmt.Sprintf("%.2f GiB", f/(unit*unit*unit))
	}
}

func formatNonZeroDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return d.Truncate(time.Second).String()
}
