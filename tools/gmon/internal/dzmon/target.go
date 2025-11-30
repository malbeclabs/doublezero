package dzmon

import (
	"context"
	"fmt"
	"time"

	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
)

const (
	defaultWindowSlots      = 60
	defaultWindowResolution = 10 * time.Second
	defaultHealthEWMAAlpha  = 0.2
	defaultWarmupPeriod     = 30 * time.Second
)

type DoubleZeroProbeResult struct {
	solmon.ValidatorProbeResult
}

type DoubleZeroTargetConfig = solmon.ValidatorTargetConfig

type DoubleZeroTarget struct {
	inner *solmon.ValidatorTarget
}

func NewDoubleZeroTarget(cfg *DoubleZeroTargetConfig) (*DoubleZeroTarget, error) {
	vt, err := solmon.NewValidatorTarget(cfg)
	if err != nil {
		return nil, err
	}
	return &DoubleZeroTarget{inner: vt}, nil
}

func (t *DoubleZeroTarget) ID() gmon.TargetID {
	return t.inner.ID()
}

func (t *DoubleZeroTarget) Probe(ctx context.Context) (gmon.ProbeResult, error) {
	res, err := t.inner.Probe(ctx)
	if err != nil {
		return nil, err
	}

	vres, ok := res.(solmon.ValidatorProbeResult)
	if !ok {
		return nil, fmt.Errorf("unexpected probe result type %T", res)
	}

	return DoubleZeroProbeResult{ValidatorProbeResult: vres}, nil
}
