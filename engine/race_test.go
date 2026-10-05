//go:build race

package engine

// raceEnabled cuts down the checks the race detector makes too slow to run
// in full.
const raceEnabled = true
