package analytics

import (
	"testing"
	"time"
)

func TestResultParser_ParseFromValues(t *testing.T) {
	parser := NewResultParser(nil)
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		rows           [][]any
		groupBy        []string
		expectedSeries int
		expectedLabels []string
		checkData      func(t *testing.T, series []TimeSeries)
	}{
		{
			name: "no group by - single Total series",
			rows: [][]any{
				{baseTime, 1000.0},
				{baseTime.Add(1 * time.Minute), 2000.0},
				{baseTime.Add(2 * time.Minute), 1500.0},
			},
			groupBy:        []string{},
			expectedSeries: 1,
			expectedLabels: []string{"Total"},
			checkData: func(t *testing.T, series []TimeSeries) {
				if len(series[0].Data) != 3 {
					t.Errorf("expected 3 data points, got %d", len(series[0].Data))
				}
			},
		},
		{
			name: "single group by column",
			rows: [][]any{
				{baseTime, "TCP", 1000.0},
				{baseTime, "UDP", 500.0},
				{baseTime.Add(1 * time.Minute), "TCP", 1200.0},
				{baseTime.Add(1 * time.Minute), "UDP", 600.0},
			},
			groupBy:        []string{"proto"},
			expectedSeries: 2,
			expectedLabels: []string{"proto=TCP", "proto=UDP"},
		},
		{
			name: "multiple group by columns",
			rows: [][]any{
				{baseTime, "TCP", "192.168.1.1", 1000.0},
				{baseTime, "UDP", "192.168.1.2", 500.0},
			},
			groupBy:        []string{"proto", "src_addr"},
			expectedSeries: 2,
			expectedLabels: []string{"proto=TCP, src_addr=192.168.1.1", "proto=UDP, src_addr=192.168.1.2"},
		},
		{
			name:           "empty rows",
			rows:           [][]any{},
			groupBy:        []string{"proto"},
			expectedSeries: 0,
		},
		{
			name: "series sorted alphabetically",
			rows: [][]any{
				{baseTime, "Zebra", 100.0},
				{baseTime, "Apple", 200.0},
				{baseTime, "Mango", 300.0},
			},
			groupBy:        []string{"fruit"},
			expectedSeries: 3,
			expectedLabels: []string{"fruit=Apple", "fruit=Mango", "fruit=Zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			series := parser.ParseFromValues(tt.rows, tt.groupBy)

			if len(series) != tt.expectedSeries {
				t.Errorf("expected %d series, got %d", tt.expectedSeries, len(series))
			}

			if tt.expectedLabels != nil {
				for i, expected := range tt.expectedLabels {
					if i >= len(series) {
						t.Errorf("missing series at index %d", i)
						continue
					}
					if series[i].Label != expected {
						t.Errorf("series[%d].Label = %q, want %q", i, series[i].Label, expected)
					}
				}
			}

			if tt.checkData != nil {
				tt.checkData(t, series)
			}
		})
	}
}

func TestResultParser_FormatLabel(t *testing.T) {
	parser := NewResultParser(nil)

	tests := []struct {
		name     string
		groupBy  []string
		values   []string
		expected string
	}{
		{
			name:     "empty group by",
			groupBy:  []string{},
			values:   []string{},
			expected: "Total",
		},
		{
			name:     "single column",
			groupBy:  []string{"proto"},
			values:   []string{"TCP"},
			expected: "proto=TCP",
		},
		{
			name:     "multiple columns",
			groupBy:  []string{"proto", "src_addr", "dst_addr"},
			values:   []string{"TCP", "192.168.1.1", "10.0.0.1"},
			expected: "proto=TCP, src_addr=192.168.1.1, dst_addr=10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.formatLabel(tt.groupBy, tt.values)
			if got != tt.expected {
				t.Errorf("formatLabel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResultParser_DataPointTimestamps(t *testing.T) {
	parser := NewResultParser(nil)
	baseTime := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)

	rows := [][]any{
		{baseTime, 1000.0},
		{baseTime.Add(1 * time.Minute), 2000.0},
	}

	series := parser.ParseFromValues(rows, []string{})

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}

	expectedTimestamps := []int64{
		baseTime.UnixMilli(),
		baseTime.Add(1 * time.Minute).UnixMilli(),
	}

	for i, expected := range expectedTimestamps {
		if series[0].Data[i].Timestamp != expected {
			t.Errorf("Data[%d].Timestamp = %d, want %d",
				i, series[0].Data[i].Timestamp, expected)
		}
	}
}

func TestResultParser_DataPointValues(t *testing.T) {
	parser := NewResultParser(nil)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := [][]any{
		{baseTime, 1234567.89},
		{baseTime.Add(1 * time.Minute), 0.0},
		{baseTime.Add(2 * time.Minute), 9999999999.99},
	}

	series := parser.ParseFromValues(rows, []string{})

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}

	expectedValues := []float64{1234567.89, 0.0, 9999999999.99}

	for i, expected := range expectedValues {
		if series[0].Data[i].Value != expected {
			t.Errorf("Data[%d].Value = %f, want %f",
				i, series[0].Data[i].Value, expected)
		}
	}
}
