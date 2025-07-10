package netns

import (
	"fmt"
	"runtime"

	"github.com/vishvananda/netns"
)

// RunInNamespace executes fn within the named network namespace.
// It locks the OS thread, switches namespaces, restores the original namespace, and unlocks.
func RunInNamespace(nsName string, fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get current netns: %w", err)
	}
	defer origNS.Close()

	targetNS, err := netns.GetFromName(nsName)
	if err != nil {
		return fmt.Errorf("get target netns %q: %w", nsName, err)
	}
	defer targetNS.Close()

	if err := netns.Set(targetNS); err != nil {
		return fmt.Errorf("setns to %q: %w", nsName, err)
	}

	fnErr := fn()

	if err := netns.Set(origNS); err != nil {
		return fmt.Errorf("restore original netns: %w", err)
	}

	return fnErr
}
