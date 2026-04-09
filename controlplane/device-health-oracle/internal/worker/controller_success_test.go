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
	"github.com/stretchr/testify/require"
)

type mockControllerCallChecker struct {
	minutesWithCalls int64
	err              error
}

func (m *mockControllerCallChecker) ControllerCallCoverage(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return m.minutesWithCalls, m.err
}

func (m *mockControllerCallChecker) Close() error { return nil }

func TestControllerSuccessCriterion_Name(t *testing.T) {
	c := NewControllerSuccessCriterion(nil, 0, 0, 0, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	assert.Equal(t, "controller_success", c.Name())
}

func TestControllerSuccessCriterion_Passes(t *testing.T) {
	// 5000 slots * 400ms = 2,000,000ms = 2000s ≈ 33.33 minutes → 33 expected minutes
	checker := &mockControllerCallChecker{minutesWithCalls: 33}
	c := NewControllerSuccessCriterion(checker, 200_000, 5_000, 400, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	device := serviceability.Device{
		Status: serviceability.DeviceStatusDrained,
	}
	passed, reason := c.Check(context.Background(), device)
	assert.True(t, passed)
	assert.Empty(t, reason)
}

func TestControllerSuccessCriterion_Fails_InsufficientCoverage(t *testing.T) {
	// 5000 slots * 400ms = 33 expected minutes, but only 20 with calls
	checker := &mockControllerCallChecker{minutesWithCalls: 20}
	c := NewControllerSuccessCriterion(checker, 200_000, 5_000, 400, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	device := serviceability.Device{
		Status: serviceability.DeviceStatusDrained,
	}
	passed, reason := c.Check(context.Background(), device)
	assert.False(t, passed)
	assert.Contains(t, reason, "controller calls cover 20/33 minutes")
}

func TestControllerSuccessCriterion_Fails_ClickHouseError(t *testing.T) {
	checker := &mockControllerCallChecker{err: errors.New("connection refused")}
	c := NewControllerSuccessCriterion(checker, 200_000, 5_000, 400, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	device := serviceability.Device{
		Status: serviceability.DeviceStatusDeviceProvisioning,
	}
	passed, reason := c.Check(context.Background(), device)
	assert.False(t, passed)
	assert.Contains(t, reason, "clickhouse query failed")
}

func TestControllerSuccessCriterion_BurnInSlotCount(t *testing.T) {
	c := NewControllerSuccessCriterion(nil, 200_000, 5_000, 400, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	tests := []struct {
		status   serviceability.DeviceStatus
		expected uint64
	}{
		{serviceability.DeviceStatusDeviceProvisioning, 200_000},
		{serviceability.DeviceStatusLinkProvisioning, 200_000},
		{serviceability.DeviceStatusDrained, 5_000},
		{serviceability.DeviceStatusPending, 200_000},
		{serviceability.DeviceStatusActivated, 200_000},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			result := c.burnInSlotCount(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestControllerSuccessCriterion_ProvisioningBurnIn(t *testing.T) {
	// 200,000 slots * 400ms = 80,000,000ms = 80,000s ≈ 1333.33 minutes → 1333 expected
	checker := &mockControllerCallChecker{minutesWithCalls: 1333}
	c := NewControllerSuccessCriterion(checker, 200_000, 5_000, 400, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	device := serviceability.Device{
		Status: serviceability.DeviceStatusDeviceProvisioning,
	}
	passed, _ := c.Check(context.Background(), device)
	require.True(t, passed, "should pass with full coverage for provisioning burn-in")
}

func TestControllerSuccessCriterion_DefaultSlotDuration(t *testing.T) {
	c := NewControllerSuccessCriterion(nil, 200_000, 5_000, 0, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	assert.Equal(t, uint64(defaultSlotDurationMs), c.slotDurationMs)
}
