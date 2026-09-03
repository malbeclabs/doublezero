package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestLinkHealthCriterion_Impairment_QueryError_HoldsHealth(t *testing.T) {
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{}, false, errors.New("connection reset")
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger())

	before := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeLinkHealthQuery))
	passed, reason := c.Check(context.Background(), serviceability.Link{})
	assert.True(t, passed, "a ClickHouse outage must not demote a healthy link")
	assert.Empty(t, reason)
	assert.Equal(t, before+1, testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeLinkHealthQuery)),
		"outages must be counted separately from criterion failures")
}

// The unit assertion above only proves the criterion passes; this proves the
// demotion path is actually closed end to end.
func TestLinkHealthEvaluator_ReadyForService_QueryError_StaysReadyForService(t *testing.T) {
	checker := &mockLinkHealthChecker{
		recentFunc: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
			return LinkHealthRecentResult{}, false, errors.New("connection reset")
		},
	}
	eval := &LinkHealthEvaluator{
		ImpairmentCriteria: []LinkCriterion{
			NewLinkHealthCriterion(LinkHealthModeImpairment, checker, 5.0, testLogger()),
		},
		Log: testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthReadyForService}
	assert.Equal(t, serviceability.LinkHealthReadyForService, eval.Evaluate(context.Background(), link))
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
			return LinkHealthWindowResult{Bad: 0, Total: 6, MaxBucketTs: freshBucket(), AllClean: true}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.True(t, passed, reason)
}

func TestLinkHealthCriterion_Recovery_SparseWindow_Fails(t *testing.T) {
	// One clean bucket satisfies "all buckets clean" but is not the 30 minutes
	// of clean telemetry the recovery dwell is supposed to require.
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{Bad: 0, Total: 1, MaxBucketTs: freshBucket(), AllClean: true}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "expected buckets")
}

func TestLinkHealthCriterion_Recovery_WindowCoverageTolerance(t *testing.T) {
	// One missing bucket at the edge of the window is tolerated; two is not.
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	tests := []struct {
		name     string
		total    uint64
		expected bool
	}{
		{"full coverage", 6, true},
		{"one bucket missing", 5, true},
		{"two buckets missing", 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &mockLinkHealthChecker{
				windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
					return LinkHealthWindowResult{Total: tt.total, MaxBucketTs: freshBucket(), AllClean: true}, true, nil
				},
			}
			c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())
			passed, _ := c.Check(ctx, serviceability.Link{})
			assert.Equal(t, tt.expected, passed)
		})
	}
}

func TestLinkHealthCriterion_Recovery_StaleWindow_Fails(t *testing.T) {
	// Frozen pipeline: the dirty buckets aged out of the trailing window and
	// only old clean ones remain, so "all clean" holds on stale data. Without
	// the recency floor the link recovers and then stays RFS, because
	// checkImpairment reads the same stale latest bucket as no-data.
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now.Add(-30 * time.Minute),
		Now:          now,
	})
	checker := &mockLinkHealthChecker{
		windowFunc: func(_ context.Context, _ string, _, _ time.Time, _ float64) (LinkHealthWindowResult, bool, error) {
			return LinkHealthWindowResult{
				Bad:         0,
				Total:       6,
				MaxBucketTs: now.Add(-45 * time.Minute),
				AllClean:    true,
			}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, 5.0, testLogger())

	passed, reason := c.Check(ctx, serviceability.Link{})
	assert.False(t, passed)
	assert.Contains(t, reason, "stale")
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
			return LinkHealthWindowResult{AllClean: true, Total: 6, MaxBucketTs: freshBucket()}, true, nil
		},
	}
	c := NewLinkHealthCriterion(LinkHealthModeRecovery, checker, threshold, testLogger())

	_, _ = c.Check(ctx, serviceability.Link{})
	assert.Equal(t, threshold, observed)
}

// Finding 4: ReadyForServiceCriteria is wired to the impairment criterion, so a
// link whose latest bucket already reads impaired must not promote to RFS only
// to be demoted on the next tick. Links with no telemetry still promote.
func TestLinkHealthEvaluator_Pending_PromotionGatedOnImpairment(t *testing.T) {
	tests := []struct {
		name     string
		recent   func(ctx context.Context, linkPubkey string) (LinkHealthRecentResult, bool, error)
		expected serviceability.LinkHealth
	}{
		{
			name: "latest bucket impaired blocks promotion",
			recent: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
				return LinkHealthRecentResult{BucketTs: freshBucket(), IsisDown: true}, true, nil
			},
			expected: serviceability.LinkHealthPending,
		},
		{
			name: "no telemetry still promotes",
			recent: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
				return LinkHealthRecentResult{}, false, nil
			},
			expected: serviceability.LinkHealthReadyForService,
		},
		{
			name: "latest bucket clean promotes",
			recent: func(_ context.Context, _ string) (LinkHealthRecentResult, bool, error) {
				return LinkHealthRecentResult{BucketTs: freshBucket(), ALossPct: 1, ZLossPct: 1}, true, nil
			},
			expected: serviceability.LinkHealthReadyForService,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criterion := NewLinkHealthCriterion(LinkHealthModeImpairment,
				&mockLinkHealthChecker{recentFunc: tt.recent}, 5.0, testLogger())
			eval := &LinkHealthEvaluator{
				ReadyForServiceCriteria: []LinkCriterion{criterion},
				ImpairmentCriteria:      []LinkCriterion{criterion},
				Log:                     testLogger(),
			}

			link := serviceability.Link{LinkHealth: serviceability.LinkHealthPending}
			assert.Equal(t, tt.expected, eval.Evaluate(context.Background(), link))
		})
	}
}
