package exporter

import "time"

type DataProviderName string

const (
	DataProviderNameRIPEAtlas  DataProviderName = "ripeatlas"
	DataProviderNameWheresitup DataProviderName = "wheresitup"
)

type Record struct {
	DataProvider       DataProviderName
	SourceLocationCode string
	TargetLocationCode string
	Timestamp          time.Time
	RTT                time.Duration
}
