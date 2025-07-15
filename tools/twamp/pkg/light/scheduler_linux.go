package twamplight

/*
#define _GNU_SOURCE
#include <pthread.h>
#include <sched.h>
#include <unistd.h>

int set_realtime_priority(int prio) {
	struct sched_param param;
	param.sched_priority = prio;
	return pthread_setschedparam(pthread_self(), SCHED_FIFO, &param);
}
*/
import "C"

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

func SetRealtimePriority(priority int) error {
	runtime.LockOSThread()
	if ret := C.set_realtime_priority(C.int(priority)); ret != 0 {
		return fmt.Errorf("pthread_setschedparam failed: %d", ret)
	}
	return nil
}

func PinCurrentThreadToCPU(cpu int) error {
	runtime.LockOSThread()
	var mask unix.CPUSet
	mask.Set(cpu)
	// Current thread is pinned
	return unix.SchedSetaffinity(0, &mask)
}
