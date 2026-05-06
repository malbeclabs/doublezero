package worker

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
)

type mockDeviceCriterion struct {
	name   string
	result bool
	reason string
}

func (m *mockDeviceCriterion) Name() string { return m.name }
func (m *mockDeviceCriterion) Check(_ context.Context, _ serviceability.Device) (bool, string) {
	return m.result, m.reason
}

type mockLinkCriterion struct {
	name   string
	result bool
	reason string
}

func (m *mockLinkCriterion) Name() string { return m.name }
func (m *mockLinkCriterion) Check(_ context.Context, _ serviceability.Link) (bool, string) {
	return m.result, m.reason
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestDeviceHealthEvaluator_NoCriteria_AdvancesToReadyForUsers(t *testing.T) {
	eval := &DeviceHealthEvaluator{Log: testLogger()}

	tests := []struct {
		name           string
		currentHealth  serviceability.DeviceHealth
		expectedHealth serviceability.DeviceHealth
	}{
		{"unknown advances to ready-for-links", serviceability.DeviceHealthUnknown, serviceability.DeviceHealthReadyForLinks},
		{"pending advances to ready-for-links", serviceability.DeviceHealthPending, serviceability.DeviceHealthReadyForLinks},
		{"ready-for-links advances to ready-for-users", serviceability.DeviceHealthReadyForLinks, serviceability.DeviceHealthReadyForUsers},
		{"ready-for-users stays at ready-for-users", serviceability.DeviceHealthReadyForUsers, serviceability.DeviceHealthReadyForUsers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := serviceability.Device{DeviceHealth: tt.currentHealth}
			result := eval.Evaluate(context.Background(), device)
			assert.Equal(t, tt.expectedHealth, result)
		})
	}
}

func TestDeviceHealthEvaluator_CriterionFails_BlocksAdvancement(t *testing.T) {
	failingCriterion := &mockDeviceCriterion{name: "test_fail", result: false, reason: "test failure"}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{failingCriterion},
		Log:                   testLogger(),
	}

	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthPending}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthPending, result, "should not advance when criterion fails")
}

func TestDeviceHealthEvaluator_CriterionPasses_Advances(t *testing.T) {
	passingCriterion := &mockDeviceCriterion{name: "test_pass", result: true}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{passingCriterion},
		Log:                   testLogger(),
	}

	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthPending}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthReadyForLinks, result)
}

func TestDeviceHealthEvaluator_StagesNotSkipped(t *testing.T) {
	passingCriterion := &mockDeviceCriterion{name: "test_pass", result: true}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{passingCriterion},
		ReadyForUsersCriteria: []DeviceCriterion{passingCriterion},
		Log:                   testLogger(),
	}

	// A device at Pending should advance to ReadyForLinks, not ReadyForUsers.
	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthPending}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthReadyForLinks, result, "should advance one stage at a time")
}

func TestDeviceHealthEvaluator_ReadyForLinks_UserCriterionFails(t *testing.T) {
	passingCriterion := &mockDeviceCriterion{name: "links_pass", result: true}
	failingCriterion := &mockDeviceCriterion{name: "users_fail", result: false, reason: "not ready"}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{passingCriterion},
		ReadyForUsersCriteria: []DeviceCriterion{failingCriterion},
		Log:                   testLogger(),
	}

	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthReadyForLinks}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthReadyForLinks, result, "should stay at ReadyForLinks when user criterion fails")
}

func TestDeviceHealthEvaluator_ReadyForLinks_LinkCriterionFails(t *testing.T) {
	failingCriterion := &mockDeviceCriterion{name: "links_fail", result: false, reason: "regressed"}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{failingCriterion},
		Log:                   testLogger(),
	}

	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthReadyForLinks}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthReadyForLinks, result, "should stay at ReadyForLinks when links criterion regresses")
}

func TestDeviceHealthEvaluator_MultipleCriteria_AllMustPass(t *testing.T) {
	passing := &mockDeviceCriterion{name: "pass", result: true}
	failing := &mockDeviceCriterion{name: "fail", result: false, reason: "nope"}
	eval := &DeviceHealthEvaluator{
		ReadyForLinksCriteria: []DeviceCriterion{passing, failing},
		Log:                   testLogger(),
	}

	device := serviceability.Device{DeviceHealth: serviceability.DeviceHealthPending}
	result := eval.Evaluate(context.Background(), device)
	assert.Equal(t, serviceability.DeviceHealthPending, result, "should not advance when any criterion fails")
}

func TestLinkHealthEvaluator_NoCriteria_AdvancesToReadyForService(t *testing.T) {
	eval := &LinkHealthEvaluator{Log: testLogger()}

	tests := []struct {
		name           string
		currentHealth  serviceability.LinkHealth
		expectedHealth serviceability.LinkHealth
	}{
		{"unknown advances", serviceability.LinkHealthUnknown, serviceability.LinkHealthReadyForService},
		{"pending advances", serviceability.LinkHealthPending, serviceability.LinkHealthReadyForService},
		{"ready stays", serviceability.LinkHealthReadyForService, serviceability.LinkHealthReadyForService},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := serviceability.Link{LinkHealth: tt.currentHealth}
			result := eval.Evaluate(context.Background(), link)
			assert.Equal(t, tt.expectedHealth, result)
		})
	}
}

func TestLinkHealthEvaluator_CriterionFails_BlocksAdvancement(t *testing.T) {
	failingCriterion := &mockLinkCriterion{name: "test_fail", result: false, reason: "nope"}
	eval := &LinkHealthEvaluator{
		ReadyForServiceCriteria: []LinkCriterion{failingCriterion},
		Log:                     testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthPending}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthPending, result, "should not advance when criterion fails")
}

func TestLinkHealthEvaluator_ReadyForService_ImpairmentFails_DemotesToImpaired(t *testing.T) {
	failing := &mockLinkCriterion{name: "impair", result: false, reason: "isis down"}
	eval := &LinkHealthEvaluator{
		ImpairmentCriteria: []LinkCriterion{failing},
		Log:                testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthReadyForService}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthImpaired, result, "RFS link with failing impairment criterion should demote")
}

func TestLinkHealthEvaluator_ReadyForService_ImpairmentPasses_Stays(t *testing.T) {
	passing := &mockLinkCriterion{name: "impair", result: true}
	eval := &LinkHealthEvaluator{
		ImpairmentCriteria: []LinkCriterion{passing},
		Log:                testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthReadyForService}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthReadyForService, result)
}

func TestLinkHealthEvaluator_ReadyForService_NoImpairmentCriteria_Stays(t *testing.T) {
	// Backwards compat: deployments without ClickHouse have no impairment criteria
	// and must not see any RFS demotion path.
	eval := &LinkHealthEvaluator{Log: testLogger()}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthReadyForService}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthReadyForService, result)
}

func TestLinkHealthEvaluator_Impaired_RecoveryPasses_PromotesToRFS(t *testing.T) {
	passing := &mockLinkCriterion{name: "recovery", result: true}
	eval := &LinkHealthEvaluator{
		RecoveryCriteria: []LinkCriterion{passing},
		Log:              testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthImpaired}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthReadyForService, result)
}

func TestLinkHealthEvaluator_Impaired_RecoveryFails_Stays(t *testing.T) {
	failing := &mockLinkCriterion{name: "recovery", result: false, reason: "still bad"}
	eval := &LinkHealthEvaluator{
		RecoveryCriteria: []LinkCriterion{failing},
		Log:              testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthImpaired}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthImpaired, result)
}

func TestLinkHealthEvaluator_Impaired_NoRecoveryCriteria_Stays(t *testing.T) {
	eval := &LinkHealthEvaluator{Log: testLogger()}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthImpaired}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthImpaired, result)
}

func TestLinkHealthEvaluator_Impaired_MixedRecovery_AnyFailKeepsImpaired(t *testing.T) {
	passing := &mockLinkCriterion{name: "recovery_pass", result: true}
	failing := &mockLinkCriterion{name: "recovery_fail", result: false, reason: "nope"}
	eval := &LinkHealthEvaluator{
		RecoveryCriteria: []LinkCriterion{passing, failing},
		Log:              testLogger(),
	}

	link := serviceability.Link{LinkHealth: serviceability.LinkHealthImpaired}
	result := eval.Evaluate(context.Background(), link)
	assert.Equal(t, serviceability.LinkHealthImpaired, result)
}

func TestLinkBurnIn_ExtractsDrainedWindow(t *testing.T) {
	now := time.Now()
	drainedStart := now.Add(-30 * time.Minute)
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		ProvisioningStart: now.Add(-20 * time.Hour),
		DrainedStart:      drainedStart,
		Now:               now,
	})

	start, end, expectedMinutes, ok := LinkBurnIn(ctx)
	assert.True(t, ok)
	assert.Equal(t, drainedStart, start)
	assert.Equal(t, now, end)
	assert.Equal(t, int64(30), expectedMinutes)
}

func TestLinkBurnIn_NoContextValues(t *testing.T) {
	_, _, _, ok := LinkBurnIn(context.Background())
	assert.False(t, ok)
}

func TestLinkBurnIn_ZeroLengthWindow(t *testing.T) {
	now := time.Now()
	ctx := ContextWithBurnInTimes(context.Background(), BurnInTimes{
		DrainedStart: now,
		Now:          now,
	})

	_, _, expectedMinutes, ok := LinkBurnIn(ctx)
	assert.True(t, ok)
	assert.Equal(t, int64(0), expectedMinutes)
}
