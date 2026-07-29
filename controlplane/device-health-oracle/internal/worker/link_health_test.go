package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
)

type mockLinkHealthChecker struct {
	recentFunc func(ctx context.Context, linkPubkey string) (LinkHealthRecentResult, bool, error)
	windowFunc func(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (LinkHealthWindowResult, bool, error)
}

func (m *mockLinkHealthChecker) LinkHealthRecent(ctx context.Context, linkPubkey string) (LinkHealthRecentResult, bool, error) {
	return m.recentFunc(ctx, linkPubkey)
}

func (m *mockLinkHealthChecker) LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (LinkHealthWindowResult, bool, error) {
	return m.windowFunc(ctx, linkPubkey, start, end, lossThreshold)
}

// freshBucket returns a bucket timestamp that's recent enough to pass the
// stale-data floor in checkImpairment.
func freshBucket() time.Time {
	return time.Now().Add(-1 * time.Minute)
}

func TestLinkHealthCriterion_Name(t *testing.T) {
	imp := NewLinkHealthCriterion(LinkHealthModeImpairment, &mockLinkHealthChecker{}, 5.0, testLogger())
	rec := NewLinkHealthCriterion(LinkHealthModeRecovery, &mockLinkHealthChecker{}, 5.0, testLogger())
	assert.Equal(t, "link_health_impairment", imp.Name())
	assert.Equal(t, "link_health_recovery", rec.Name())
}

func TestLinkHealthCriterion_Impairment_NoData_Passes(t *testing.T) {
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{}, false, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())
	link := serviceability.Link{LinkHealth: serviceability.LinkHealthReadyForService}

	passed, _ := c.Check(context.Background(), link)
	assert.True(t, passed, "no data must not flag a link as impaired")
}

func TestLinkHealthCriterion_Impairment_StaleBucket_Passes(t *testing.T) {
	// A latest bucket older than the recency floor signals a broken telemetry
	// pipeline. Don't act on it — neither demote nor recover.
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{
				BucketTs: time.Now().Add(-1 * time.Hour),
				IsisDown: true,
				ALossPct: 100,
				ZLossPct: 100,
			}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())

	passed, _ := c.Check(context.Background(), serviceability.Link{})
	assert.True(t, passed, "stale bucket should be treated as no data even when it indicates impairment")
}

func TestLinkHealthCriterion_Impairment_IsisDown_Fails(t *testing.T) {
	bucket := freshBucket()
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{BucketTs: bucket, IsisDown: true}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())

	passed, reason := c.Check(context.Background(), serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "isis")
	assert.Contains(t, reason, "bucket=")
}

func TestLinkHealthCriterion_Impairment_LossExceedsThreshold(t *testing.T) {
	tests := []struct {
		name     string
		aLoss    float64
		zLoss    float64
		expected bool
	}{
		{"both clean", 1.0, 1.0, true},
		{"a above threshold", 6.0, 1.0, false},
		{"z above threshold", 1.0, 6.0, false},
		{"a exactly at threshold", 5.0, 0, true},
		{"z exactly at threshold", 0, 5.0, true},
		{"both far above", 80.0, 90.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &mockLinkHealthChecker{
				recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
					return LinkHealthRecentResult{
						BucketTs: freshBucket(),
						ALossPct: tt.aLoss,
						ZLossPct: tt.zLoss,
					}, true, nil
				},
			}
			c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())
			passed, _ := c.Check(context.Background(), serviceability.Link{})
			assert.Equal(t, tt.expected, passed)
		})
	}
}

func TestLinkHealthCriterion_Impairment_QueryError_Fails(t *testing.T) {
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{}, false, errors.New("connection reset")
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())

	passed, reason := c.Check(context.Background(), serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "clickhouse query failed")
}

func TestLinkHealthCriterion_Recovery_NoBurnInContext_Fails(t *testing.T) {
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, &mockLinkHealthChecker{}, 5.0, testLogger())
	passed, reason := c.Check(context.Background(), serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "burn-in times not available")
}

func TestLinkHealthCriterion_Recovery_ZeroWindow_Fails(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now,
		Now:          now,
	})
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, &mockLinkHealthChecker{}, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "recovery window not yet established")
}

func TestLinkHealthCriterion_Recovery_AllClean_Passes(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{Bad: 0, Total: 6, AllClean: true}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, _ := c.Check(ctx, serviceability.Link{})
	assert.True(t, passed)
}

func TestLinkHealthCriterion_Recovery_NotAllClean_Fails(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{Bad: 2, Total: 6, AllClean: false}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "2/6")
}

func TestLinkHealthCriterion_Recovery_NoData_Fails(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{}, false, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "no rollup data")
}

func TestLinkHealthCriterion_Recovery_QueryError_Fails(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{}, false, errors.New("boom")
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "clickhouse query failed")
}

func TestLinkHealthCriterion_Recovery_PassesThresholdToChecker(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	const threshold = 7.5
	var observed float64
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, lossThreshold float64) (LinkHealthWindowResult, bool, error) {
			observed = lossThreshold
			return LinkHealthWindowResult{AllClean: true, Total: 6}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, threshold, testLogger())

	_, _ = c.Check(ctx, serviceability.Link{})
	assert.Equal(t, threshold, observed)
}
