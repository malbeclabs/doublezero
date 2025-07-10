package netns

import (
	"fmt"
	"runtime"

	"github.com/vishvananda/netns"
)

// RunInNamespace executes the given function within the context of the specified
// Linux network namespace. It locks the current OS thread, switches to the target
// namespace using setns, invokes the function, and then restores the original
// namespace. This allows thread-local operations like dialing sockets to be scoped
// to a namespace without affecting the rest of the program.
//
// This is safe for use in single-threaded, short-lived operations; not safe for
// concurrent use.
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
