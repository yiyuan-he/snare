package color

import (
	"os"
)

// ANSI escape codes for terminal colors.
const (
	Red    = "\033[31m"
	Yellow = "\033[33m"
	Green  = "\033[32m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Reset  = "\033[0m"
)

// enabled caches whether color output should be used.
var enabled = shouldEnable()

func shouldEnable() bool {
	// Respect NO_COLOR convention (https://no-color.org/)
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	// Check if stdout is a terminal
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Enabled returns whether color output is active.
func Enabled() bool {
	return enabled
}

// SetEnabled overrides automatic detection (useful for testing).
func SetEnabled(v bool) {
	enabled = v
}

// Apply wraps s with the given ANSI code if color is enabled.
func Apply(code, s string) string {
	if !enabled {
		return s
	}
	return code + s + Reset
}
