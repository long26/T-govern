//go:build race

package p2p

// raceEnabled reports whether the binary was built with -race, so timing
// assertions can relax their bounds under instrumentation.
const raceEnabled = true
