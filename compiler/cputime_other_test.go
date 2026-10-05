//go:build !linux

package compiler

import "time"

// cpuTime returns how long f takes: wall time, where the thread's CPU time
// is not at hand.
func cpuTime(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}
