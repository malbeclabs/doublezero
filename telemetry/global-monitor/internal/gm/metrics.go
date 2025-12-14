package gm

import "strings"

func metricsLabelsFromTarget(t ProbeTarget) (probeType, path string) {
	id := string(t.ID())
	switch {
	case strings.Contains(id, "icmp/"):
		probeType = string(ProbeTypeICMP)
	case strings.Contains(id, "tpuquic/"):
		probeType = string(ProbeTypeTPUQUIC)
	default:
		probeType = string(ProbeTypeUnknown)
	}

	if strings.Contains(id, "/"+ProbePathDoubleZero.String()+"/") ||
		strings.Contains(id, "/doublezero") {
		path = string(ProbePathDoubleZero)
	} else {
		path = string(ProbePathPublicInternet)
	}
	return
}

func metricsLabelsFromTargetID(id ProbeTargetID) (probeType, path string) {
	s := string(id)
	if strings.Contains(s, "icmp/") {
		probeType = string(ProbeTypeICMP)
	} else {
		probeType = string(ProbeTypeTPUQUIC)
	}

	if strings.Contains(s, "/"+ProbePathDoubleZero.String()+"/") {
		path = string(ProbePathDoubleZero)
	} else {
		path = string(ProbePathPublicInternet)
	}
	return
}

func metricsPathFromIface(iface string) string {
	if iface == "" {
		return "unknown"
	}
	if strings.Contains(iface, "doublezero") {
		return string(ProbePathDoubleZero)
	}
	return string(ProbePathPublicInternet)
}

func metricsProbeTypeFromKind(k PlanKind) string {
	if strings.Contains(string(k), "icmp") {
		return string(ProbeTypeICMP)
	} else if strings.Contains(string(k), "tpuquic") {
		return string(ProbeTypeTPUQUIC)
	} else {
		return string(ProbeTypeUnknown)
	}
}
