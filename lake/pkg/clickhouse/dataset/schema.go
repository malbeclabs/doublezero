package dataset

// DimensionSchema defines the structure of a dimension dataset for ClickHouse
type DimensionSchema interface {
	// Name returns the dataset name (e.g., "dz_contributors")
	Name() string
	// PrimaryKeyColumns returns the column definitions for primary key fields
	PrimaryKeyColumns() []string
	// PayloadColumns returns the column definitions for all other fields
	PayloadColumns() []string
}

// FactSchema defines the structure of a fact dataset for ClickHouse
type FactSchema interface {
	// Name returns the dataset name (e.g., "dz_device_interface_counters")
	Name() string
	// UniqueKeyColumns returns the column definitions for unique key fields
	UniqueKeyColumns() []string
	// Columns returns the column definitions for all fields
	Columns() []string
	// TimeColumn returns the column name for the time column
	TimeColumn() string
	// PartitionByTime returns true if the dataset should be partitioned by time
	PartitionByTime() bool
	// DedupMode returns the dedup mode of the dataset
	DedupMode() DedupMode
	// DedupVersionColumn returns the column name for the dedup version column
	DedupVersionColumn() string
}
