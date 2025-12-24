package dztelem

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type InternetMetroCircuit struct {
	Code            string
	OriginMetroPK   string
	TargetMetroPK   string
	OriginMetroCode string
	TargetMetroCode string
}

func ComputeInternetMetroCircuits(metros []dzsvc.Metro) []InternetMetroCircuit {
	circuits := make([]InternetMetroCircuit, 0)
	circuitsByCode := make(map[string]struct{})

	for _, originMetro := range metros {
		for _, targetMetro := range metros {
			if originMetro.Code == targetMetro.Code {
				continue
			}

			// Ensure consistent ordering (origin < target) to avoid duplicates
			var origin, target dzsvc.Metro
			if originMetro.Code < targetMetro.Code {
				origin, target = originMetro, targetMetro
			} else {
				origin, target = targetMetro, originMetro
			}

			code := fmt.Sprintf("%s → %s", origin.Code, target.Code)
			if _, ok := circuitsByCode[code]; ok {
				continue
			}

			circuitsByCode[code] = struct{}{}
			circuits = append(circuits, InternetMetroCircuit{
				Code:            code,
				OriginMetroPK:   origin.PK,
				TargetMetroPK:   target.PK,
				OriginMetroCode: origin.Code,
				TargetMetroCode: target.Code,
			})
		}
	}

	// Sort for consistent ordering
	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	return circuits
}

type InternetMetroLatencySample struct {
	CircuitCode           string
	DataProvider          string
	Epoch                 uint64
	SampleIndex           int
	TimestampMicroseconds uint64
	RTTMicroseconds       uint32
}

func (v *View) refreshInternetMetroLatencySamples(ctx context.Context, circuits []InternetMetroCircuit) error {
	v.log.Debug("telemetry/internet-metro: starting sample refresh", "circuits", len(circuits), "data_providers", len(v.cfg.InternetDataProviders))

	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch
	v.log.Debug("telemetry/internet-metro: current epoch", "epoch", currentEpoch)

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}
	v.log.Debug("telemetry/internet-metro: fetching epochs", "epochs", epochsToFetch, "max_concurrency", v.cfg.MaxConcurrency)

	var allSamples []InternetMetroLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	circuitsProcessed := 0
	circuitsWithSamples := 0
	var circuitsWithSamplesMu sync.Mutex

	for _, circuit := range circuits {
		// Check for context cancellation before starting new goroutines
		select {
		case <-ctx.Done():
			v.log.Debug("telemetry/internet-metro: context cancelled, stopping circuit processing")
			goto done
		default:
		}

		circuitsProcessed++
		originPK, err := solana.PublicKeyFromBase58(circuit.OriginMetroPK)
		if err != nil {
			v.log.Debug("telemetry/internet-metro: invalid origin metro PK", "circuit", circuit.Code, "error", err)
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(circuit.TargetMetroPK)
		if err != nil {
			v.log.Debug("telemetry/internet-metro: invalid target metro PK", "circuit", circuit.Code, "error", err)
			continue
		}

		// Fetch samples for each data provider
		for _, dataProvider := range v.cfg.InternetDataProviders {
			// Check for context cancellation before starting new goroutines
			select {
			case <-ctx.Done():
				v.log.Debug("telemetry/internet-metro: context cancelled, stopping data provider processing")
				goto done
			default:
			}

			wg.Add(1)
			// Try to acquire semaphore with context cancellation support
			select {
			case <-ctx.Done():
				wg.Done()
				goto done
			case sem <- struct{}{}:
				go func(circuit InternetMetroCircuit, originPK, targetPK solana.PublicKey, dataProvider string) {
					defer wg.Done()
					defer func() { <-sem }() // Release semaphore

					circuitHasSamples := false
					var circuitSamples []InternetMetroLatencySample

					for _, epoch := range epochsToFetch {
						// Check for context cancellation before each RPC call
						select {
						case <-ctx.Done():
							v.log.Debug("telemetry/internet-metro: context cancelled during fetch", "circuit", circuit.Code, "data_provider", dataProvider)
							return
						default:
						}

						samples, err := v.cfg.TelemetryRPC.GetInternetLatencySamples(ctx, dataProvider, originPK, targetPK, v.cfg.InternetLatencyAgentPK, epoch)
						if err != nil {
							if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
								return
							}
							if errors.Is(err, telemetry.ErrAccountNotFound) {
								v.log.Debug("telemetry/internet-metro: no samples found", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch)
								continue
							}
							v.log.Debug("telemetry/internet-metro: failed to get latency samples", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch, "error", err)
							continue
						}

						circuitHasSamples = true
						sampleCount := len(samples.Samples)
						v.log.Debug("telemetry/internet-metro: fetched samples", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch, "samples", sampleCount)

						// Convert samples to our format
						converted := convertInternetLatencySamples(samples, circuit.Code, dataProvider, epoch)
						circuitSamples = append(circuitSamples, converted...)
					}

					if circuitHasSamples {
						circuitsWithSamplesMu.Lock()
						circuitsWithSamples++
						circuitsWithSamplesMu.Unlock()
					}

					// Append samples to shared slice
					if len(circuitSamples) > 0 {
						samplesMu.Lock()
						allSamples = append(allSamples, circuitSamples...)
						samplesMu.Unlock()
					}
				}(circuit, originPK, targetPK, dataProvider)
			}
		}
	}

done:
	wg.Wait()

	v.log.Debug("telemetry/internet-metro: processed circuits", "total", circuitsProcessed, "with_samples", circuitsWithSamples, "total_samples", len(allSamples))

	// Refresh samples table - no lock needed, DuckDB handles concurrency
	v.log.Debug("telemetry/internet-metro: refreshing latency samples table", "samples", len(allSamples))
	if err := v.refreshInternetMetroLatencySamplesTable(allSamples); err != nil {
		v.log.Error("telemetry/internet-metro: failed to refresh latency samples", "error", err, "total_samples", len(allSamples))
		return fmt.Errorf("failed to refresh internet-metro latency samples: %w", err)
	}

	v.log.Debug("telemetry/internet-metro: sample refresh completed", "samples_inserted", len(allSamples))
	return nil
}

func convertInternetLatencySamples(samples *telemetry.InternetLatencySamples, circuitCode, dataProvider string, epoch uint64) []InternetMetroLatencySample {
	result := make([]InternetMetroLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
		result[i] = InternetMetroLatencySample{
			CircuitCode:           circuitCode,
			DataProvider:          dataProvider,
			Epoch:                 epoch,
			SampleIndex:           i,
			TimestampMicroseconds: timestamp,
			RTTMicroseconds:       rtt,
		}
	}
	return result
}

func (v *View) refreshInternetMetroLatencySamplesTable(samples []InternetMetroLatencySample) error {
	tableRefreshStart := time.Now()
	v.log.Info("telemetry: refreshing table started", "table", "dz_internet_metro_latency_samples", "rows", len(samples), "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		v.log.Info("telemetry: refreshing table completed", "table", "dz_internet_metro_latency_samples", "duration", duration.String())
	}()

	if len(samples) == 0 {
		// Use regular refreshTable for empty case
		return v.refreshTable("dz_internet_metro_latency_samples", "DELETE FROM dz_internet_metro_latency_samples", "INSERT INTO dz_internet_metro_latency_samples (circuit_code, data_provider, epoch, sample_index, timestamp_us, rtt_us) VALUES (?, ?, ?, ?, ?, ?)", 0, nil)
	}

	v.log.Debug("telemetry/internet-metro: starting bulk insert using COPY FROM", "samples", len(samples))
	startTime := time.Now()

	// Create a temporary CSV file for COPY FROM (much faster than INSERT)
	tmpFile, err := os.CreateTemp("", "internet_metro_latency_samples_*.csv")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write CSV data
	v.log.Debug("telemetry/internet-metro: writing CSV file", "samples", len(samples))
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	writeStart := time.Now()
	for _, s := range samples {
		record := []string{
			s.CircuitCode,
			s.DataProvider,
			fmt.Sprintf("%d", s.Epoch),
			fmt.Sprintf("%d", s.SampleIndex),
			fmt.Sprintf("%d", s.TimestampMicroseconds),
			fmt.Sprintf("%d", s.RTTMicroseconds),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("CSV writer error: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	writeDuration := time.Since(writeStart)
	v.log.Debug("telemetry/internet-metro: CSV file written", "duration_ms", writeDuration.Milliseconds(), "file_size_mb", float64(getFileSize(tmpFile))/1024/1024)

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Use COPY FROM for bulk load (much faster than INSERT)
	txStart := time.Now()
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	v.log.Debug("telemetry: transaction begun", "table", "dz_internet_metro_latency_samples", "tx_start_time", txStart)
	defer tx.Rollback()

	// Clear existing data using TRUNCATE (faster and avoids some concurrency issues)
	if _, err := tx.Exec("TRUNCATE dz_internet_metro_latency_samples"); err != nil {
		return fmt.Errorf("failed to clear table: %w", err)
	}

	// Use COPY FROM CSV - this is the fastest way to load data
	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY dz_internet_metro_latency_samples FROM '%s' (FORMAT CSV, HEADER false)", tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV: %w", err)
	}
	copyDuration := time.Since(copyStart)
	v.log.Debug("telemetry/internet-metro: COPY FROM completed", "duration", copyDuration.String())

	commitStart := time.Now()
	v.log.Info("telemetry: committing transaction", "table", "dz_internet_metro_latency_samples", "rows", len(samples), "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		v.log.Error("telemetry: transaction commit failed", "table", "dz_internet_metro_latency_samples", "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commitDuration := time.Since(commitStart)
	v.log.Info("telemetry: transaction committed", "table", "dz_internet_metro_latency_samples", "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	totalDuration := time.Since(startTime)
	rate := float64(len(samples)) / totalDuration.Seconds()
	v.log.Debug("telemetry/internet-metro: bulk insert completed", "samples", len(samples), "total_duration_ms", totalDuration.Milliseconds(), "rate_rows_per_sec", int(rate))
	return nil
}
