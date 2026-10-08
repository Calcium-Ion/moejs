// Package pluginbytecode holds the test that the plugins compile to the
// bytecode recorded in testdata/bytecode.txt. The plugin workload's speed
// changes only with that bytecode or with the code that runs it, so a
// change that keeps this test passing and touches no code the plugins run
// takes a lighter gate (docs/DESIGN.md §14.2).
package pluginbytecode
