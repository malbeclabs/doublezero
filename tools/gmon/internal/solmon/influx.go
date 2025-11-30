package solmon

import (
	"time"

	write "github.com/influxdata/influxdb-client-go/v2/api/write"
)

type InfluxSample struct {
	res       ValidatorProbeResult
	extraTags map[string]string
}

func NewInfluxSample(res ValidatorProbeResult, extraTags map[string]string) InfluxSample {
	return InfluxSample{
		res:       res,
		extraTags: extraTags,
	}
}

func (p InfluxSample) Points() []*write.Point {
	tags := map[string]string{
		"kind":             "pub",
		"validator_pubkey": p.res.Pubkey.String(),
	}

	for k, v := range p.extraTags {
		if tags[k] == "" {
			continue
		}
		tags[k] = v
	}

	fields := map[string]any{
		// per-probe (raw)
		"probe_ok": p.res.OK,

		// rolling-window snapshot values
		"window_avail":       p.res.WindowAvail,
		"ewma_avail":         p.res.Health.EWMAAvailability,
		"window_mean_rtt_ms": float64(p.res.WindowMeanRTT.Milliseconds()),
		"window_successes":   int64(p.res.WindowSuccesses),
		"window_failures":    int64(p.res.WindowFailures),
		"consecutive_fail":   int64(p.res.Health.ConsecutiveFail),
		"warmup":             p.res.Warmup,
		"warmup_failures":    int64(p.res.WarmupFailures),
	}

	if s := p.res.Stats; s != nil {
		// raw per-probe RTT values
		fields["probe_rtt_smoothed_ms"] = float64(s.SmoothedRTT.Milliseconds())
		fields["probe_rtt_latest_ms"] = float64(s.LatestRTT.Milliseconds())
		fields["probe_rtt_min_ms"] = float64(s.MinRTT.Milliseconds())
		fields["probe_rtt_dev_ms"] = float64(s.MeanDeviation.Milliseconds())

		// raw per-probe traffic stats
		fields["probe_bytes_sent"] = int64(s.BytesSent)
		fields["probe_bytes_recv"] = int64(s.BytesReceived)
		fields["probe_packets_sent"] = int64(s.PacketsSent)
		fields["probe_packets_recv"] = int64(s.PacketsReceived)
		fields["probe_packets_lost"] = int64(s.PacketsLost)

		if s.PacketsSent > 0 {
			fields["probe_loss_rate"] = float64(s.PacketsLost) / float64(s.PacketsSent)
		}
	}

	pt := write.NewPoint(
		"solana_validator_tpuquic_probe",
		tags,
		fields,
		time.Now(),
	)

	return []*write.Point{pt}
}
