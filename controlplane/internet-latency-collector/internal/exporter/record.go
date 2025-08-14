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
	SourceExchangeCode string
	TargetExchangeCode string
	Timestamp          time.Time
	RTT                time.Duration
}

func (r *Record) Validate() error {
	if r.DataProvider == "" {
		return fmt.Errorf("record given to ledger exporter has no data provider")
	}
	if r.SourceExchangeCode == "" {
		return fmt.Errorf("record given to ledger exporter has no source exchange code")
	}
	if r.TargetExchangeCode == "" {
		return fmt.Errorf("record given to ledger exporter has no target exchange code")
	}
	return nil
}
