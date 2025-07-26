package poll

import (
	"context"
	"fmt"
	"time"
)

func Until(ctx context.Context, condition func() (bool, error), timeout, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ok, err := condition()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("polling cancelled or timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
