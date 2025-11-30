package summary

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/tools/gmon/internal/dzmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/gmon"
	"github.com/malbeclabs/doublezero/tools/gmon/internal/solmon"
)

type SummaryConfig struct {
	Logger *slog.Logger

	LogInterval           time.Duration
	AvailabilityThreshold float64
}

func (c *SummaryConfig) normalize() {
	if c.LogInterval <= 0 {
		c.LogInterval = 15 * time.Second
	}
	if c.AvailabilityThreshold <= 0 {
		c.AvailabilityThreshold = 0.95
	}
}

func StartSummaryLogger(
	ctx context.Context,
	cfg SummaryConfig,
	results <-chan gmon.ProbeResult,
	targetCount func() int,
	targetList func() []gmon.Target,
) {
	cfg.normalize()
	log := cfg.Logger

	state := make(map[gmon.TargetID]gmon.ProbeResult)

	ticker := time.NewTicker(cfg.LogInterval)
	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case res, ok := <-results:
				if !ok {
					return
				}
				state[res.TargetID()] = res

			case <-ticker.C:
				totalConfigured := targetCount()

				if len(state) == 0 {
					log.Info("solana targets summary",
						"targetsConfigured", totalConfigured,
						"targetsProbed", 0,
						"unhealthy", 0,
						"availNever", 0,
						"availBelowThreshold", 0,
						"targetsMissing", totalConfigured,
					)
					continue
				}

				// Per-group aggregates
				var totalAgg summaryGroup
				var pubAgg summaryGroup
				var dzAgg summaryGroup

				// For cross-path comparisons
				pubByPK := make(map[string]solmon.ValidatorProbeResult)
				dzByPK := make(map[string]dzmon.DoubleZeroProbeResult)

				// Configured-but-never-probed targets, and per-kind configured/missing.
				configured := targetList()
				missing := make([]string, 0)

				var pubConfigured, dzConfigured int
				var pubMissing, dzMissing int

				for _, gres := range state {
					switch res := gres.(type) {
					case solmon.ValidatorProbeResult:
						totalAgg.addSample(res, cfg.AvailabilityThreshold)
						pubAgg.addSample(res, cfg.AvailabilityThreshold)
						pubByPK[res.Pubkey.String()] = res
						pubConfigured++
					case dzmon.DoubleZeroProbeResult:
						totalAgg.addSample(res.ValidatorProbeResult, cfg.AvailabilityThreshold)
						dzAgg.addSample(res.ValidatorProbeResult, cfg.AvailabilityThreshold)
						dzByPK[res.Pubkey.String()] = res
						dzConfigured++
					}
				}

				for _, target := range configured {
					id := target.ID()
					if _, ok := state[id]; !ok {
						missing = append(missing, id.String())
						switch target.(type) {
						case *solmon.ValidatorTarget:
							pubMissing++
						case *dzmon.DoubleZeroTarget:
							dzMissing++
						}
					}
				}

				// DZ-only / RTT comparison metrics.
				var dzUnhealthyOnlyDZ int
				var dzNeverAvailableOnlyDZ int
				var dzBelowThresholdOnlyDZ int
				var dzRTTWorseThanPublic int

				for pk, dzRes := range dzByPK {
					pubRes, hasPub := pubByPK[pk]

					// Classification for DZ path (non-warmup)
					if !dzRes.Warmup {
						avail := dzRes.WindowAvail
						h := dzRes.Health

						isNeverAvail := false
						isBelowThresh := false
						isUnhealthy := false

						if avail == 0 {
							if h.LastSuccess.IsZero() {
								isUnhealthy = true
								isNeverAvail = true
							}
						} else if avail < cfg.AvailabilityThreshold {
							isUnhealthy = true
							isBelowThresh = true
						}

						if isUnhealthy && !hasPub {
							dzUnhealthyOnlyDZ++
							if isNeverAvail {
								dzNeverAvailableOnlyDZ++
							}
							if isBelowThresh {
								dzBelowThresholdOnlyDZ++
							}
						}
					}

					// RTT comparison vs public internet, only if both sides have some data.
					if hasPub &&
						!dzRes.Warmup && !pubRes.Warmup &&
						dzRes.WindowAvail > 0 && pubRes.WindowAvail > 0 &&
						dzRes.WindowMeanRTT > 0 && pubRes.WindowMeanRTT > 0 &&
						dzRes.WindowMeanRTT > pubRes.WindowMeanRTT {
						dzRTTWorseThanPublic++
					}
				}

				// Detect if any target is still in warmup
				warmupActive := false
				for _, gres := range state {
					switch res := gres.(type) {
					case solmon.ValidatorProbeResult:
						if res.Warmup {
							warmupActive = true
							break
						}
					case dzmon.DoubleZeroProbeResult:
						if res.Warmup {
							warmupActive = true
							break
						}
					}
				}

				totalFields := []any{
					"targetsConfigured", totalConfigured,
					"targetsProbed", len(state),
					"unhealthy", uint32(totalAgg.unhealthy),
					"availNever", uint32(totalAgg.neverAvailable),
					"availBelowThreshold", uint32(totalAgg.belowThreshold),
				}
				if len(missing) > 0 {
					totalFields = append(totalFields,
						"targetsMissing", missing,
					)
				}

				publicFields := []any{
					"targetsConfigured", pubConfigured,
					"targetsProbed", pubAgg.probed,
					"unhealthy", pubAgg.unhealthy,
					"availNever", pubAgg.neverAvailable,
					"availBelowThreshold", pubAgg.belowThreshold,
				}
				if pubMissing > 0 {
					publicFields = append(publicFields,
						"targetsMissing", pubMissing,
					)
				}

				doublezeroFields := []any{
					"targetsConfigured", dzConfigured,
					"targetsProbed", dzAgg.probed,
					"unhealthy", dzAgg.unhealthy,
					"availNever", dzAgg.neverAvailable,
					"availBelowThreshold", dzAgg.belowThreshold,
				}
				if dzMissing > 0 {
					doublezeroFields = append(doublezeroFields,
						"targetsMissing", dzMissing,
					)
				}

				comparisonFields := []any{
					"dzUnhealthyOnlyDZ", dzUnhealthyOnlyDZ,
					"dzAvailNeverOnlyDZ", dzNeverAvailableOnlyDZ,
					"dzAvailBelowThresholdOnlyDZ", dzBelowThresholdOnlyDZ,
					"dzRTTWorseThanPublic", dzRTTWorseThanPublic,
				}

				if warmupActive {
					totalFields = append(totalFields,
						"availThreshold", cfg.AvailabilityThreshold,
						"warmupTargets", uint32(totalAgg.warmupTargets),
						"warmupWithFailures", uint32(totalAgg.warmupWithFailures),
					)
					publicFields = append(publicFields,
						"availThreshold", cfg.AvailabilityThreshold,
						"warmupTargets", pubAgg.warmupTargets,
						"warmupWithFailures", pubAgg.warmupWithFailures,
					)
					doublezeroFields = append(doublezeroFields,
						"availThreshold", cfg.AvailabilityThreshold,
						"warmupTargets", dzAgg.warmupTargets,
						"warmupWithFailures", dzAgg.warmupWithFailures,
					)
					comparisonFields = append(comparisonFields,
						"warmupTargets", uint32(totalAgg.warmupTargets),
						"warmupWithFailures", uint32(totalAgg.warmupWithFailures),
					)
				}

				// 1. Total/global summary (backwards compatible)
				log.Info("solana validator targets summary (total)", totalFields...)

				// 2. Public internet (pub/) summary
				log.Info("solana validator targets summary (public internet)", publicFields...)

				// 3. DoubleZero (dz/) summary
				log.Info("solana validator targets summary (doublezero)", doublezeroFields...)

				// 4. DoubleZero vs public internet comparison
				log.Info("solana validator targets comparison (doublezero vs public internet)", comparisonFields...)

				// Per-target DEBUG health/window summary so you can understand why
				// each target is in a given bucket.
				for id, gres := range state {
					// For now we only have validator probe results, so we can safely cast to
					// solmon.ValidatorProbeResult, but in the future this will change.
					var s solmon.ValidatorProbeResult
					switch res := gres.(type) {
					case solmon.ValidatorProbeResult:
						s = res
					case dzmon.DoubleZeroProbeResult:
						s = res.ValidatorProbeResult
					default:
						log.Error("unknown probe result type", "target", id.String(), "type", fmt.Sprintf("%T", gres))
						continue
					}

					category := "healthy"

					switch {
					case s.Warmup:
						category = "warmup"
					case s.WindowAvail == 0 && s.Health.LastSuccess.IsZero():
						category = "neverAvailable"
					case s.WindowAvail < cfg.AvailabilityThreshold:
						category = "belowThreshold"
					}

					// Extract a few RTT stats if we have them.
					var (
						rttSmoothed time.Duration
						rttLatest   time.Duration
						rttMin      time.Duration
						bytesSent   uint64
						bytesRecv   uint64
					)
					if s.Stats != nil {
						rttSmoothed = s.Stats.SmoothedRTT
						rttLatest = s.Stats.LatestRTT
						rttMin = s.Stats.MinRTT
						bytesSent = s.Stats.BytesSent
						bytesRecv = s.Stats.BytesReceived
					}

					windowSuccesses := s.WindowSuccesses
					windowFailures := s.WindowFailures

					log.Debug("solana validator target health",
						"target", id.String(),
						"category", category,
						"ok", s.OK,
						"err", s.Error,
						"windowAvail", s.WindowAvail,
						"windowSuccesses", windowSuccesses,
						"windowFailures", windowFailures,
						"ewmaAvail", s.Health.EWMAAvailability,
						"windowMeanRTT", s.WindowMeanRTT,
						"lastSuccess", s.Health.LastSuccess,
						"lastFailure", s.Health.LastFailure,
						"consecutiveFail", s.Health.ConsecutiveFail,
						"rttSmoothed", rttSmoothed,
						"rttLatest", rttLatest,
						"rttMin", rttMin,
						"bytesSent", bytesSent,
						"bytesRecv", bytesRecv,
						"warmup", s.Warmup,
						"warmupFailures", s.WarmupFailures,
					)
				}

				// Also useful to see which targets never produced any result at all.
				if len(missing) > 0 {
					log.Debug("solana validator targets with no probe results yet",
						"missingTargets", missing,
					)
				}
			}
		}
	}()
}

type summaryGroup struct {
	probed             int
	unhealthy          int
	neverAvailable     int
	belowThreshold     int
	warmupTargets      int
	warmupWithFailures int
}

func (g *summaryGroup) addSample(s solmon.ValidatorProbeResult, threshold float64) {
	g.probed++

	if s.Warmup {
		g.warmupTargets++
		if s.WarmupFailures > 0 {
			g.warmupWithFailures++
		}
		// Don’t classify warmup targets as unhealthy yet.
		return
	}

	avail := s.WindowAvail
	h := s.Health

	if avail == 0 {
		if h.LastSuccess.IsZero() {
			g.unhealthy++
			g.neverAvailable++
		}
		return
	}

	if avail < threshold {
		g.unhealthy++
		g.belowThreshold++
	}
}
