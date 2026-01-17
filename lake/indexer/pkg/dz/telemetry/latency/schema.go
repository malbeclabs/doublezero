package dztelemlatency

import (
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
)

type DeviceLinkLatencySchema struct{}

func (s *DeviceLinkLatencySchema) Name() string {
	return "dz_device_link_latency"
}

func (s *DeviceLinkLatencySchema) UniqueKeyColumns() []string {
	return []string{"event_ts", "origin_device_pk", "target_device_pk", "link_pk", "epoch", "sample_index"}
}

func (s *DeviceLinkLatencySchema) Columns() []string {
	return []string{
		"ingested_at:TIMESTAMP",
		"epoch:BIGINT",
		"sample_index:INTEGER",
		"origin_device_pk:VARCHAR",
		"target_device_pk:VARCHAR",
		"link_pk:VARCHAR",
		"rtt_us:BIGINT",
		"loss:BOOLEAN",
		"ipdv_us:BIGINT",
	}
}

func (s *DeviceLinkLatencySchema) TimeColumn() string {
	return "event_ts"
}

func (s *DeviceLinkLatencySchema) PartitionByTime() bool {
	return true
}

func (s *DeviceLinkLatencySchema) Grain() string {
	return "1 minute"
}

func (s *DeviceLinkLatencySchema) DedupMode() dataset.DedupMode {
	return dataset.DedupReplacing
}

func (s *DeviceLinkLatencySchema) DedupVersionColumn() string {
	return "ingested_at"
}

type InternetMetroLatencySchema struct{}

func (s *InternetMetroLatencySchema) Name() string {
	return "dz_internet_metro_latency"
}

func (s *InternetMetroLatencySchema) UniqueKeyColumns() []string {
	return []string{"event_ts", "origin_metro_pk", "target_metro_pk", "data_provider", "epoch", "sample_index"}
}

func (s *InternetMetroLatencySchema) Columns() []string {
	return []string{
		"ingested_at:TIMESTAMP",
		"epoch:BIGINT",
		"sample_index:INTEGER",
		"origin_metro_pk:VARCHAR",
		"target_metro_pk:VARCHAR",
		"data_provider:VARCHAR",
		"rtt_us:BIGINT",
		"ipdv_us:BIGINT",
	}
}

func (s *InternetMetroLatencySchema) TimeColumn() string {
	return "event_ts"
}

func (s *InternetMetroLatencySchema) PartitionByTime() bool {
	return true
}

func (s *InternetMetroLatencySchema) Grain() string {
	return "one sample per "
}

func (s *InternetMetroLatencySchema) DedupMode() dataset.DedupMode {
	return dataset.DedupReplacing
}

func (s *InternetMetroLatencySchema) DedupVersionColumn() string {
	return "ingested_at"
}

func NewDeviceLinkLatencyDataset(log *slog.Logger) (*dataset.FactDataset, error) {
	return dataset.NewFactDataset(log, &DeviceLinkLatencySchema{})
}

func NewInternetMetroLatencyDataset(log *slog.Logger) (*dataset.FactDataset, error) {
	return dataset.NewFactDataset(log, &InternetMetroLatencySchema{})
}
