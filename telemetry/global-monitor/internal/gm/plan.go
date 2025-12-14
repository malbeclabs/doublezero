package gm

type PlanKind string

const (
	PlanKindSolValICMP    PlanKind = "solval/icmp"
	PlanKindSolValTPUQUIC PlanKind = "solval/tpuquic"
	PlanKindDZUserICMP    PlanKind = "dzuser/icmp"
)

type ProbePlan struct {
	ID     ProbeTargetID
	Kind   PlanKind
	Path   ProbePath
	Record func(res *ProbeResult)
}
