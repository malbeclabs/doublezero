package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
)

type mockControllerCallChecker struct {
	minutesWithCalls int64
	err              error
}

func (m *mockControllerCallChecker) ControllerCallCoverage(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return m.minutesWithCalls, m.err
}

func (m *mockControllerCallChecker) Close() error { return nil }

func testLoggerSlog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func TestControllerSuccessCriterion_Name(t *testing.T) {
	c := NewControllerSuccessCriterion(nil, testLoggerSlog())
	assert.Equal(t, "controller_success", c.Name())
}

func TestControllerSuccessCriterion_Passes(t *testing.T) {
	now := time.Now()
	start := now.Add(-33 * time.Minute)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: start,
		Now:          now,
	})

	checker := &mockControllerCallChecker{minutesWithCalls: 33}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, reason := c.Check(ctx, device)
	assert.True(t, passed)
	assert.Empty(t, reason)
}

func TestControllerSuccessCriterion_Fails_InsufficientCoverage(t *testing.T) {
	now := time.Now()
	start := now.Add(-33 * time.Minute)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: start,
		Now:          now,
	})

	checker := &mockControllerCallChecker{minutesWithCalls: 20}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, reason := c.Check(ctx, device)
	assert.False(t, passed)
	assert.Contains(t, reason, "controller calls cover 20/33 minutes")
}

func TestControllerSuccessCriterion_Fails_ClickHouseError(t *testing.T) {
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: start,
		Now:               now,
	})

	checker := &mockControllerCallChecker{err: errors.New("connection refused")}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(ctx, device)
	assert.False(t, passed)
	assert.Contains(t, reason, "clickhouse query failed")
}

func TestControllerSuccessCriterion_Fails_NoBurnInTimes(t *testing.T) {
	checker := &mockControllerCallChecker{minutesWithCalls: 100}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(context.Background(), device)
	assert.False(t, passed)
	assert.Contains(t, reason, "burn-in times not available")
}

func TestControllerSuccessCriterion_UsesProvisioningStart(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now.Add(-60 * time.Minute),
		DrainedStart:      now.Add(-10 * time.Minute),
		Now:               now,
	})

	// Provide enough coverage for provisioning (60 min) but check that
	// DeviceProvisioning status uses ProvisioningStart, not DrainedStart.
	checker := &mockControllerCallChecker{minutesWithCalls: 60}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, _ := c.Check(ctx, device)
	assert.True(t, passed)
}

func TestControllerSuccessCriterion_UsesDrainedStart(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now.Add(-60 * time.Minute),
		DrainedStart:      now.Add(-10 * time.Minute),
		Now:               now,
	})

	// Only 10 minutes of coverage — enough for drained but not provisioning.
	checker := &mockControllerCallChecker{minutesWithCalls: 10}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, _ := c.Check(ctx, device)
	assert.True(t, passed)
}

func TestControllerSuccessCriterion_ZeroBurnIn_Passes(t *testing.T) {
	// When burn-in start == now (zero-length window), the criterion should pass
	// without querying ClickHouse. This happens in newly created environments
	// where the burn-in slot is 0.
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now,
		Now:               now,
	})

	checker := &mockControllerCallChecker{err: errors.New("should not be called")}
	c := NewControllerSuccessCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(ctx, device)
	assert.True(t, passed)
	assert.Empty(t, reason)
}
