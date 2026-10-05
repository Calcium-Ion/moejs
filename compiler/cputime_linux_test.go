package compiler

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// cpuTime returns how long f runs on the CPU: the time its thread spends
// running it (f runs locked to it), so that waiting for the CPU on a
// loaded machine does not count.
func cpuTime(f func()) time.Duration {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	start := threadTime()
	f()
	return threadTime() - start
}

// threadTime reads CLOCK_THREAD_CPUTIME_ID (3), which counts nanoseconds;
// getrusage's thread times move by scheduler ticks.
func threadTime() time.Duration {
	var ts syscall.Timespec
	if _, _, errno := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 3, uintptr(unsafe.Pointer(&ts)), 0); errno != 0 {
		panic(errno)
	}
	return time.Duration(ts.Nano())
}
