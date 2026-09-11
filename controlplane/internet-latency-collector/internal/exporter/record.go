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
	Cloud              *CloudInfo
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

// CloudInfo names the cloud each exchange code belongs to, the probe that took the sample and the
// ping packet counts behind it. It is nil on records from the onchain exchange path.
type CloudInfo struct {
	SourceCloud     string
	TargetCloud     string
	ProbeID         uint32
	PacketsSent     uint8
	PacketsReceived uint8
}

func (c *CloudInfo) Validate() error {
	if c == nil {
		return fmt.Errorf("record has no cloud info")
	}
	if c.SourceCloud == "" {
		return fmt.Errorf("record cloud info has no source cloud")
	}
	if c.TargetCloud == "" {
		return fmt.Errorf("record cloud info has no target cloud")
	}
	if c.ProbeID == 0 {
		return fmt.Errorf("record cloud info has no probe id")
	}
	return nil
}
