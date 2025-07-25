package collector

import (
	"os"
	"testing"
)

// TestMain sets up the test environment once for all tests in the package
func TestMain(m *testing.M) {
	// Initialize logger once for all tests
	InitLogger(LogLevelWarn)

	// Run tests
	code := m.Run()

	// Exit with the test result code
	os.Exit(code)
}
