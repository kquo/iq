// Package color provides simple ANSI terminal color functions for IQ's CLI output.
// Replaces github.com/queone/utl color wrappers with zero external dependencies.
// Colors are suppressed when stdout is not a terminal (piped output) or when
// the NO_COLOR environment variable is set (https://no-color.org).
package color

import (
	"os"
)

// enabled is true when the terminal supports color output.
var enabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}()

func wrap(code, s string) string {
	if !enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// Gra renders s in dark gray.
func Gra(s string) string { return wrap("90", s) }

// Grn renders s in green.
func Grn(s string) string { return wrap("32", s) }

// Yel renders s in yellow.
func Yel(s string) string { return wrap("33", s) }

// Red renders s in bright red.
func Red(s string) string { return wrap("91", s) }

// Whi renders s in white.
func Whi(s string) string { return wrap("37", s) }

// Whi2 renders s in bright white.
func Whi2(s string) string { return wrap("97", s) }
