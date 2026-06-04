package analyze

import (
	"fmt"
	"time"
)

// fmtInt formats with thousand-separators for readability in tables.
func fmtInt(n int) string {
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmtInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}

// fmtDur renders a duration with units that fit the magnitude. Zero
// durations render as "—" so empty cells are visually obvious in tables.
func fmtDur(d time.Duration) string {
	if d == 0 {
		return "—"
	}
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
