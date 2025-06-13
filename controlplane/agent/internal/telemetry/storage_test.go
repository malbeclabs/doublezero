package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileStorage(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test creating storage
	storage, err := NewFileStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}

	if storage.basePath != tmpDir {
		t.Errorf("Expected base path %s, got %s", tmpDir, storage.basePath)
	}

	// Test with non-existent parent directory
	nestedPath := filepath.Join(tmpDir, "nested", "path")
	storage2, err := NewFileStorage(nestedPath)
	if err != nil {
		t.Fatalf("Failed to create nested storage: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("Expected nested directory to be created")
	}

	_ = storage2 // Avoid unused variable warning
}

func TestSaveAndLoadSamples(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	// Create test samples
	samples := &LinkSamples{
		DeviceAPubkey:   "device1",
		DeviceZPubkey:   "device2",
		LinkPubkey:      "link1",
		LocationAPubkey: "location1",
		LocationZPubkey: "location2",
		Samples: []RTTSample{
			{RTTMicroseconds: 1000, Timestamp: time.Now(), PacketID: 1},
			{RTTMicroseconds: 1100, Timestamp: time.Now(), PacketID: 2},
			{RTTMicroseconds: 1200, Timestamp: time.Now(), PacketID: 3},
		},
		StartTimestamp:               time.Now(),
		NextSampleIndex:              3,
		SamplingIntervalMicroseconds: 1000000,
	}

	linkKey := "device1:device2:link1"

	// Save samples
	err = storage.SaveSamples(linkKey, samples)
	if err != nil {
		t.Fatalf("Failed to save samples: %v", err)
	}

	// Verify file exists
	expectedFile := filepath.Join(tmpDir, "device1_device2_link1.json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Error("Expected samples file to exist")
	}

	// Load samples
	loadedSamples, err := storage.LoadSamples()
	if err != nil {
		t.Fatalf("Failed to load samples: %v", err)
	}

	if len(loadedSamples) != 1 {
		t.Errorf("Expected 1 sample set, got %d", len(loadedSamples))
	}

	if loaded, ok := loadedSamples[linkKey]; ok {
		if loaded.DeviceAPubkey != "device1" {
			t.Errorf("Expected device1, got %s", loaded.DeviceAPubkey)
		}
		if len(loaded.Samples) != 3 {
			t.Errorf("Expected 3 samples, got %d", len(loaded.Samples))
		}
		if loaded.Samples[0].RTTMicroseconds != 1000 {
			t.Errorf("Expected RTT 1000, got %d", loaded.Samples[0].RTTMicroseconds)
		}
	} else {
		t.Errorf("Expected to find samples for %s", linkKey)
	}
}

func TestSaveMultipleSamples(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	// Save multiple sample sets
	for i := range 5 {
		samples := &LinkSamples{
			DeviceAPubkey:   "device1",
			DeviceZPubkey:   "device" + string(rune('2'+i)),
			LinkPubkey:      "link" + string(rune('1'+i)),
			LocationAPubkey: "location1",
			LocationZPubkey: "location" + string(rune('2'+i)),
			Samples: []RTTSample{
				{RTTMicroseconds: uint32(1000 + i*100), Timestamp: time.Now()},
			},
		}

		linkKey := "device1:device" + string(rune('2'+i)) + ":link" + string(rune('1'+i))
		err := storage.SaveSamples(linkKey, samples)
		if err != nil {
			t.Fatalf("Failed to save samples %d: %v", i, err)
		}
	}

	// Load all samples
	loadedSamples, err := storage.LoadSamples()
	if err != nil {
		t.Fatalf("Failed to load samples: %v", err)
	}

	if len(loadedSamples) != 5 {
		t.Errorf("Expected 5 sample sets, got %d", len(loadedSamples))
	}
}

func TestRotateSamples(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	samples := &LinkSamples{
		DeviceAPubkey: "device1",
		DeviceZPubkey: "device2",
		LinkPubkey:    "link1",
		Samples: []RTTSample{
			{RTTMicroseconds: 1000, Timestamp: time.Now()},
		},
	}

	linkKey := "device1:device2:link1"

	// Save samples
	err = storage.SaveSamples(linkKey, samples)
	if err != nil {
		t.Fatalf("Failed to save samples: %v", err)
	}

	// Rotate samples
	err = storage.RotateSamples(linkKey)
	if err != nil {
		t.Fatalf("Failed to rotate samples: %v", err)
	}

	// Original file should not exist
	originalFile := filepath.Join(tmpDir, "device1_device2_link1.json")
	if _, err := os.Stat(originalFile); !os.IsNotExist(err) {
		t.Error("Expected original file to be moved")
	}

	// Check for archive file
	files, _ := os.ReadDir(tmpDir)
	archiveFound := false
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".json" && len(file.Name()) > 13 {
			if file.Name()[len(file.Name())-13:] == ".archive.json" {
				archiveFound = true
				break
			}
		}
	}

	if !archiveFound {
		t.Error("Expected to find archive file")
	}

	// Load samples should return empty (original file was rotated)
	loadedSamples, _ := storage.LoadSamples()
	if len(loadedSamples) != 0 {
		t.Errorf("Expected 0 samples after rotation, got %d", len(loadedSamples))
	}
}

func TestSanitizeLinkKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"device1:device2:link1", "device1_device2_link1"},
		{"dev:ice1:dev:ice2:li:nk1", "dev_ice1_dev_ice2_li_nk1"},
		{"no_colons", "no_colons"},
	}

	for _, tt := range tests {
		result := sanitizeLinkKey(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeLinkKey(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestUnsanitizeLinkKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"device1_device2_link1", "device1:device2:link1"},
		{"device1_device2_link_with_underscore", "device1:device2:link_with_underscore"},
		{"too_many_underscores_here", "too_many_underscores_here"}, // Should return as-is
		{"no_underscores", "no_underscores"},                       // Should return as-is
	}

	for _, tt := range tests {
		result := unsanitizeLinkKey(tt.input)
		if result != tt.expected {
			t.Errorf("unsanitizeLinkKey(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestCleanupOldArchives(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	// Create some archive files with different ages
	now := time.Now()
	oldFile := filepath.Join(tmpDir, "device1_device2_link1_20230101_120000.archive.json")
	newFile := filepath.Join(tmpDir, "device1_device2_link2_20230102_120000.archive.json")

	// Create old archive file
	os.WriteFile(oldFile, []byte("{}"), 0644)
	os.Chtimes(oldFile, now.Add(-48*time.Hour), now.Add(-48*time.Hour))

	// Create new archive file
	os.WriteFile(newFile, []byte("{}"), 0644)
	os.Chtimes(newFile, now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	// Clean up archives older than 24 hours
	err = storage.CleanupOldArchives(24 * time.Hour)
	if err != nil {
		t.Fatalf("Failed to cleanup archives: %v", err)
	}

	// Old file should be removed
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Expected old archive to be removed")
	}

	// New file should still exist
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("Expected new archive to still exist")
	}
}

func TestConcurrentFileOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	// Concurrent saves
	done := make(chan bool)
	for i := range 10 {
		go func(idx int) {
			samples := &LinkSamples{
				DeviceAPubkey: "device1",
				DeviceZPubkey: "device" + string(rune('2'+idx%3)),
				LinkPubkey:    "link" + string(rune('1'+idx%3)),
				Samples: []RTTSample{
					{RTTMicroseconds: uint32(1000 + idx), Timestamp: time.Now()},
				},
			}

			linkKey := "device1:device" + string(rune('2'+idx%3)) + ":link" + string(rune('1'+idx%3))
			err := storage.SaveSamples(linkKey, samples)
			if err != nil {
				t.Errorf("Failed to save samples: %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all saves to complete
	for range 10 {
		<-done
	}

	// Concurrent loads
	for range 5 {
		go func() {
			_, err := storage.LoadSamples()
			if err != nil {
				t.Errorf("Failed to load samples: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all loads to complete
	for range 5 {
		<-done
	}

	// Final load to verify
	samples, err := storage.LoadSamples()
	if err != nil {
		t.Fatalf("Failed to load final samples: %v", err)
	}

	// Should have 3 unique link keys
	if len(samples) != 3 {
		t.Errorf("Expected 3 sample sets, got %d", len(samples))
	}
}

func TestExtractLinkKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, _ := NewFileStorage(tmpDir)

	tests := []struct {
		filename string
		expected string
	}{
		{"device1_device2_link1.json", "device1:device2:link1"},
		{"device1_device2_link_with_underscore.json", "device1:device2:link_with_underscore"},
		{"invalid_format.json", "invalid_format"}, // Returns as-is when format doesn't match
		{"no_extension", "no_extension"},
	}

	for _, tt := range tests {
		result := storage.extractLinkKey(tt.filename)
		if result != tt.expected {
			t.Errorf("extractLinkKey(%s) = %s, want %s", tt.filename, result, tt.expected)
		}
	}
}
