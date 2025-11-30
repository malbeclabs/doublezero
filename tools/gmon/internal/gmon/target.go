package gmon

import (
	"context"
)

type ProbeResult interface {
	TargetID() TargetID
}

type TargetID string

func (id TargetID) String() string { return string(id) }

type Target interface {
	ID() TargetID
	Probe(ctx context.Context) (ProbeResult, error)
}
