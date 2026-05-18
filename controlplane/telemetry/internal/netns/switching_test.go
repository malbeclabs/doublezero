package netns

import (
	"strings"
	"testing"
)

// TestRunInNamespace_EmptyNameErrors verifies that an empty namespace name is
// not a no-op fallback to the current namespace, but a loud failure. The
// short-circuit was removed when bgpstatus stopped using "" as a sentinel for
// the Arista default VRF.
func TestRunInNamespace_EmptyNameErrors(t *testing.T) {
	_, err := RunInNamespace("", func() (struct{}, error) { return struct{}{}, nil })
	if err == nil {
		t.Fatal("expected error for empty namespace name")
	}
	if !strings.Contains(err.Error(), "get target netns") {
		t.Errorf("expected GetFromName error, got %v", err)
	}
}
