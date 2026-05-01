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
// If nsName is empty, the function is called in the current (root) namespace
// without any namespace switching. This is the correct behavior for the global
// routing table on Arista EOS, which lives in the root network namespace rather
// than a named namespace under /var/run/netns/.
//
// This is safe for use in single-threaded, short-lived operations; not safe for
// concurrent use.
func RunInNamespace[T any](nsName string, fn func() (T, error)) (T, error) {
	if nsName == "" {
		return fn()
	}

	var zero T

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNS, err := netns.Get()
	if err != nil {
		return zero, fmt.Errorf("get current netns: %w", err)
	}
	defer origNS.Close()

	targetNS, err := netns.GetFromName(nsName)
	if err != nil {
		return zero, fmt.Errorf("get target netns %q: %w", nsName, err)
	}
	defer targetNS.Close()

	if err := netns.Set(targetNS); err != nil {
		return zero, fmt.Errorf("setns to %q: %w", nsName, err)
	}

	result, fnErr := fn()

	if err := netns.Set(origNS); err != nil {
		return zero, fmt.Errorf("restore original netns: %w", err)
	}

	return result, fnErr
}
