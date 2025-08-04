package exporter

import (
	"fmt"
	"time"
)

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

func (r *Record) Validate() error {
	if r.DataProvider == "" {
		return fmt.Errorf("record given to ledger exporter has no data provider")
	}
	if r.SourceLocationCode == "" {
		return fmt.Errorf("record given to ledger exporter has no source location code")
	}
	if r.TargetLocationCode == "" {
		return fmt.Errorf("record given to ledger exporter has no target location code")
	}
	return nil
}
