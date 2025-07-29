package exporter

import "context"

type Exporter interface {
	WriteRecords(ctx context.Context, records []Record) error
	Close() error
}
