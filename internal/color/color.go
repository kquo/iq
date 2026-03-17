// Package color provides simple ANSI terminal color functions for IQ's CLI output.
// Replaces github.com/queone/utl color wrappers with zero external dependencies.
// Colors are suppressed when stdout is not a terminal (piped output) or when
// the NO_COLOR environment variable is set (https://no-color.org).
package color

import (
	"fmt"
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

func wrap(code string, v any) string {
	s := fmt.Sprint(v)
	if !enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// Gra renders v in dark gray.
func Gra(v any) string { return wrap("90", v) }

// Grn renders v in green.
func Grn(v any) string { return wrap("32", v) }

// GrnR renders v in reverse video green (green background, dark text).
func GrnR(v any) string { return wrap("7;32", v) }

// Yel renders v in yellow.
func Yel(v any) string { return wrap("33", v) }

// Blu renders v in bright blue.
func Blu(v any) string { return wrap("94", v) }

// Cya renders v in cyan.
func Cya(v any) string { return wrap("36", v) }

// Red renders v in bright red.
func Red(v any) string { return wrap("91", v) }

// Whi renders v in white.
func Whi(v any) string { return wrap("37", v) }

// Whi2 renders v in bright white.
func Whi2(v any) string { return wrap("97", v) }
