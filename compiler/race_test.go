//go:build race

package compiler

// raceEnabled skips the checks the race detector makes too slow.
const raceEnabled = true
