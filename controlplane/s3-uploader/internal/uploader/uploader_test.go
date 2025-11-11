package uploader

import (
	"io"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/s3-uploader/internal/config"
)

func TestComputeMD5(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "1B2M2Y8AsgTpgAmY7PhCfg==",
		},
		{
			name:     "hello world",
			data:     []byte("hello world"),
			expected: "XrY7u+Ae7tCTyyK7j1rNww==",
		},
		{
			name:     "json data",
			data:     []byte(`{"key":"value"}`),
			expected: "pzU/fN3OgI3gAydHoLe+UA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeMD5(tt.data)
			if got != tt.expected {
				t.Errorf("computeMD5() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetS3URL(t *testing.T) {
	tests := []struct {
		name        string
		bucket      string
		region      string
		key         string
		endpointURL *string
		expected    string
	}{
		{
			name:        "standard AWS URL",
			bucket:      "my-bucket",
			region:      "us-east-1",
			key:         "test.json",
			endpointURL: nil,
			expected:    "https://my-bucket.s3.us-east-1.amazonaws.com/test.json",
		},
		{
			name:        "custom endpoint",
			bucket:      "my-bucket",
			region:      "us-east-1",
			key:         "test.json",
			endpointURL: stringPtr("http://localhost:9000"),
			expected:    "http://localhost:9000/my-bucket/test.json",
		},
		{
			name:        "key with prefix",
			bucket:      "my-bucket",
			region:      "eu-west-1",
			key:         "prefix/test.json",
			endpointURL: nil,
			expected:    "https://my-bucket.s3.eu-west-1.amazonaws.com/prefix/test.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &Uploader{
				config: &config.Config{
					AWS: config.AWSConfig{
						Bucket:      tt.bucket,
						Region:      tt.region,
						EndpointURL: tt.endpointURL,
					},
				},
			}

			got := u.getS3URL(tt.key)
			if got != tt.expected {
				t.Errorf("getS3URL() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBytesReader(t *testing.T) {
	data := []byte("hello world")
	reader := newBytesReader(data)

	// Test Read
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Errorf("Read() failed: %v", err)
	}
	if n != 5 {
		t.Errorf("Read() returned %d bytes, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("Read() = %s, want 'hello'", string(buf))
	}

	// Test Seek to beginning
	_, err = reader.Seek(0, io.SeekStart)
	if err != nil {
		t.Errorf("Seek() failed: %v", err)
	}

	// Read again from beginning
	_, err = reader.Read(buf)
	if err != nil {
		t.Errorf("Read() after Seek failed: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("Read() after Seek = %s, want 'hello'", string(buf))
	}

	// Test Seek to end
	pos, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		t.Errorf("Seek(SeekEnd) failed: %v", err)
	}
	if pos != int64(len(data)) {
		t.Errorf("Seek(SeekEnd) = %d, want %d", pos, len(data))
	}

	// Test Read at end returns EOF
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Errorf("Read() at end should return EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("Read() at end returned %d bytes, want 0", n)
	}
}

func TestBytesReaderSeek(t *testing.T) {
	data := []byte("0123456789")
	reader := newBytesReader(data)

	tests := []struct {
		name    string
		offset  int64
		whence  int
		wantPos int64
		wantErr bool
	}{
		{"seek start 5", 5, io.SeekStart, 5, false},
		{"seek current +2", 2, io.SeekCurrent, 7, false},
		{"seek end -3", -3, io.SeekEnd, 7, false},
		{"seek start 0", 0, io.SeekStart, 0, false},
		{"seek negative", -1, io.SeekStart, 0, true},
		{"seek beyond end", 100, io.SeekStart, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, err := reader.Seek(tt.offset, tt.whence)
			if (err != nil) != tt.wantErr {
				t.Errorf("Seek() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && pos != tt.wantPos {
				t.Errorf("Seek() pos = %d, want %d", pos, tt.wantPos)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
