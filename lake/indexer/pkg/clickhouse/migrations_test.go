package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransformForSingleNode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "removes ON CLUSTER clause",
			input: `CREATE TABLE IF NOT EXISTS dim_foo
ON CLUSTER lake
(
    id String
) ENGINE = ReplicatedMergeTree`,
			expected: `CREATE TABLE IF NOT EXISTS dim_foo
(
    id String
) ENGINE = MergeTree`,
		},
		{
			name:     "converts ReplicatedMergeTree to MergeTree",
			input:    "ENGINE = ReplicatedMergeTree",
			expected: "ENGINE = MergeTree",
		},
		{
			name:     "converts ReplicatedReplacingMergeTree with version column",
			input:    "ENGINE = ReplicatedReplacingMergeTree(\n  '/clickhouse/tables/{shard}/lake/fact_foo',\n  '{replica}',\n  ingested_at\n)",
			expected: "ENGINE = ReplacingMergeTree(ingested_at)",
		},
		{
			name: "full fact table transformation",
			input: `CREATE TABLE IF NOT EXISTS fact_dz_device_interface_counters
ON CLUSTER lake
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    device_pk String
)
ENGINE = ReplicatedReplacingMergeTree(
  '/clickhouse/tables/{shard}/lake/fact_dz_device_interface_counters',
  '{replica}',
  ingested_at
)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, device_pk);`,
			expected: `CREATE TABLE IF NOT EXISTS fact_dz_device_interface_counters
(
    event_ts DateTime64(3),
    ingested_at DateTime64(3),
    device_pk String
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(event_ts)
ORDER BY (event_ts, device_pk);`,
		},
		{
			name: "full dimension table transformation",
			input: `CREATE TABLE IF NOT EXISTS dim_dz_contributors_history
ON CLUSTER lake
(
    entity_id String,
    snapshot_ts DateTime64(3)
) ENGINE = ReplicatedMergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts);`,
			expected: `CREATE TABLE IF NOT EXISTS dim_dz_contributors_history
(
    entity_id String,
    snapshot_ts DateTime64(3)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(snapshot_ts)
ORDER BY (entity_id, snapshot_ts);`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformForSingleNode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
