// Package color provides the terminal color helpers used by IQ commands.
package color

import (
	"fmt"
	"os"
	"strings"
)

// enabled reports whether helpers should emit 256-color escape sequences.
var enabled = colorEnabled(os.Getenv, stdoutIsTTY())

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func colorEnabled(getenv func(string) string, tty bool) bool {
	if !tty || getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return false
	}
	colorterm := getenv("COLORTERM")
	return colorterm == "truecolor" || colorterm == "24bit" || strings.Contains(getenv("TERM"), "256color")
}

func wrap(code string, value any) string {
	text := fmt.Sprint(value)
	if !enabled {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// Gra5 renders value in canonical gray.
func Gra5(value any) string { return wrap("38;5;245", value) }

// Grn5 renders value in canonical green.
func Grn5(value any) string { return wrap("38;5;46", value) }

// Red5 renders value in canonical red.
func Red5(value any) string { return wrap("38;5;196", value) }

// Whi5 renders value in canonical white.
func Whi5(value any) string { return wrap("38;5;231", value) }

// Whi9 renders value in bright white.
func Whi9(value any) string { return wrap("38;5;255", value) }

// Yel5 renders value in canonical yellow.
func Yel5(value any) string { return wrap("38;5;220", value) }
