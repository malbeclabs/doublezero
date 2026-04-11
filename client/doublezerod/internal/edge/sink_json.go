package edge

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// JSONFileSink writes records as newline-delimited JSON (JSONL) to a file.
type JSONFileSink struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewJSONFileSink opens (or creates) the file at path for JSONL output.
func NewJSONFileSink(path string) (*JSONFileSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening output file: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return &JSONFileSink{file: f, enc: enc}, nil
}

func (s *JSONFileSink) Write(records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range records {
		if err := s.enc.Encode(&records[i]); err != nil {
			return fmt.Errorf("encoding record: %w", err)
		}
	}
	return nil
}

func (s *JSONFileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
