package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
)

type mockInterfaceCountersChecker struct {
	minutesWithRecords int64
	err                error
}

func (m *mockInterfaceCountersChecker) InterfaceCountersCoverage(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return m.minutesWithRecords, m.err
}

func TestInterfaceCountersCriterion_Name(t *testing.T) {
	c := NewInterfaceCountersCriterion(nil, testLoggerSlog())
	assert.Equal(t, "interface_counters", c.Name())
}

func TestInterfaceCountersCriterion_Passes(t *testing.T) {
	now := time.Now()
	start := now.Add(-33 * time.Minute)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: start,
		Now:          now,
	})

	checker := &mockInterfaceCountersChecker{minutesWithRecords: 33}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, reason := c.Check(ctx, device)
	assert.True(t, passed)
	assert.Empty(t, reason)
}

func TestInterfaceCountersCriterion_Fails_InsufficientCoverage(t *testing.T) {
	now := time.Now()
	start := now.Add(-33 * time.Minute)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: start,
		Now:          now,
	})

	checker := &mockInterfaceCountersChecker{minutesWithRecords: 20}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, reason := c.Check(ctx, device)
	assert.False(t, passed)
	assert.Contains(t, reason, "interface counters cover 20/33 minutes")
}

func TestInterfaceCountersCriterion_Fails_ClickHouseError(t *testing.T) {
	now := time.Now()
	start := now.Add(-1 * time.Hour)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: start,
		Now:               now,
	})

	checker := &mockInterfaceCountersChecker{err: errors.New("connection refused")}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(ctx, device)
	assert.False(t, passed)
	assert.Contains(t, reason, "clickhouse query failed")
}

func TestInterfaceCountersCriterion_Fails_NoBurnInTimes(t *testing.T) {
	checker := &mockInterfaceCountersChecker{minutesWithRecords: 100}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(context.Background(), device)
	assert.False(t, passed)
	assert.Contains(t, reason, "burn-in times not available")
}

func TestInterfaceCountersCriterion_UsesProvisioningStart(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now.Add(-60 * time.Minute),
		DrainedStart:      now.Add(-10 * time.Minute),
		Now:               now,
	})

	checker := &mockInterfaceCountersChecker{minutesWithRecords: 60}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, _ := c.Check(ctx, device)
	assert.True(t, passed)
}

func TestInterfaceCountersCriterion_UsesDrainedStart(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now.Add(-60 * time.Minute),
		DrainedStart:      now.Add(-10 * time.Minute),
		Now:               now,
	})

	checker := &mockInterfaceCountersChecker{minutesWithRecords: 10}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDrained}
	passed, _ := c.Check(ctx, device)
	assert.True(t, passed)
}

func TestInterfaceCountersCriterion_ZeroBurnIn_Passes(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now,
		Now:               now,
	})

	checker := &mockInterfaceCountersChecker{err: errors.New("should not be called")}
	c := NewInterfaceCountersCriterion(checker, testLoggerSlog())

	device := serviceability.Device{Status: serviceability.DeviceStatusDeviceProvisioning}
	passed, reason := c.Check(ctx, device)
	assert.True(t, passed)
	assert.Empty(t, reason)
}
