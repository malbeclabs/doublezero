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
	DataProvider    DataProviderName
	SourceMetroCode string
	TargetMetroCode string
	Timestamp       time.Time
	RTT             time.Duration
}

func (r *Record) Validate() error {
	if r.DataProvider == "" {
		return fmt.Errorf("record given to ledger exporter has no data provider")
	}
	if r.SourceMetroCode == "" {
		return fmt.Errorf("record given to ledger exporter has no source metro code")
	}
	if r.TargetMetroCode == "" {
		return fmt.Errorf("record given to ledger exporter has no target metro code")
	}
	return nil
}
