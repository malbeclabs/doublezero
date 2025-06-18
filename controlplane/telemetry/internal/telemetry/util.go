package telemetry

import (
	"context"
	"math/rand"
	"time"
)

func randFloat64() float64 {
	return rand.Float64()
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
