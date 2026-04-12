// Package color provides ANSI terminal color helpers for CLI output.
// When the terminal advertises 256-color support (via COLORTERM or TERM
// containing "256color"), functions emit 256-color SGR sequences (38;5;N).
// Otherwise they fall back to basic ANSI codes (30–97) that render
// correctly on any color-capable terminal. Colors are suppressed entirely
// when stdout is not a terminal or NO_COLOR is set (https://no-color.org).
package color

import (
	"fmt"
	"os"
	"strings"
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

// color256 is true when the terminal advertises 256-color support.
var color256 = func() bool {
	ct := os.Getenv("COLORTERM")
	if ct == "truecolor" || ct == "24bit" {
		return true
	}
	return strings.Contains(os.Getenv("TERM"), "256color")
}()

func wrap(code string, v any) string {
	s := fmt.Sprint(v)
	if !enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// pick returns c256 when the terminal supports 256 colors, cBasic otherwise.
func pick(c256, cBasic string) string {
	if color256 {
		return c256
	}
	return cBasic
}

// Gra renders v in gray.
func Gra(v any) string { return wrap(pick("38;5;246", "90"), v) }

// Grn renders v in green.
func Grn(v any) string { return wrap(pick("38;5;2", "32"), v) }

// GrnR renders v in reverse video green (green background, dark text).
func GrnR(v any) string { return wrap(pick("7;38;5;2", "7;32"), v) }

// GrnD renders v in dark green.
func GrnD(v any) string { return wrap(pick("38;5;28", "32"), v) }

// Yel renders v in yellow.
func Yel(v any) string { return wrap(pick("38;5;3", "33"), v) }

// Blu renders v in bright blue.
func Blu(v any) string { return wrap(pick("38;5;12", "94"), v) }

// Cya renders v in cyan.
func Cya(v any) string { return wrap(pick("38;5;6", "36"), v) }

// Red renders v in bright red.
func Red(v any) string { return wrap(pick("38;5;9", "91"), v) }

// RedR renders v in white text on red background.
func RedR(v any) string { return wrap(pick("38;5;15;48;5;1", "97;41"), v) }

// RedD renders v in dark red.
func RedD(v any) string { return wrap(pick("38;5;124", "31"), v) }

// Whi renders v in white.
func Whi(v any) string { return wrap(pick("38;5;7", "37"), v) }

// Whi2 renders v in bright white.
func Whi2(v any) string { return wrap(pick("38;5;15", "97"), v) }

// BoldW renders v in bold bright white.
func BoldW(v any) string { return wrap(pick("1;38;5;15", "1;97"), v) }

// ShowPalette prints a labeled swatch of every color function to stdout.
// Useful for verifying terminal rendering and choosing colors.
func ShowPalette() {
	sample := "The quick brown fox"
	type entry struct {
		name string
		fn   func(any) string
		c256 string
		cB   string
	}
	entries := []entry{
		{"Gra ", Gra, "38;5;246", "90"},
		{"Grn ", Grn, "38;5;2", "32"},
		{"GrnR", GrnR, "7;38;5;2", "7;32"},
		{"GrnD", GrnD, "38;5;28", "32"},
		{"Yel ", Yel, "38;5;3", "33"},
		{"Blu ", Blu, "38;5;12", "94"},
		{"Cya ", Cya, "38;5;6", "36"},
		{"Red ", Red, "38;5;9", "91"},
		{"RedR", RedR, "38;5;15;48;5;1", "97;41"},
		{"RedD", RedD, "38;5;124", "31"},
		{"Whi ", Whi, "38;5;7", "37"},
		{"Whi2", Whi2, "38;5;15", "97"},
		{"BoldW", BoldW, "1;38;5;15", "1;97"},
	}
	mode := "basic ANSI"
	if color256 {
		mode = "256-color"
	}
	fmt.Printf("%s (%s)\n", BoldW("Color palette"), mode)
	fmt.Println()
	for _, e := range entries {
		code := e.cB
		if color256 {
			code = e.c256
		}
		fmt.Printf("  %-6s %-20s  %s\n", e.name, e.fn(sample), Gra(code))
	}
	fmt.Println()
}

// UsageLine is a single flag+description pair for FormatUsage.
type UsageLine struct {
	Flag string
	Desc string
}

func formatFlag(flag string) (string, int) {
	rawLen := len(flag)
	idx := strings.LastIndex(flag, " ")
	if idx < 0 {
		return flag, rawLen
	}
	suffix := flag[idx+1:]
	switch suffix {
	case "string", "int", "float", "bool", "duration":
		return flag[:idx+1] + Gra(suffix), rawLen
	}
	return flag, rawLen
}

// FormatUsage builds a formatted help string with a heading, flag table, and optional footer.
func FormatUsage(heading string, lines []UsageLine, footer string) string {
	var b strings.Builder
	b.WriteString(BoldW("Usage:"))
	b.WriteString(" ")
	b.WriteString(heading)
	b.WriteString("\n")
	for _, l := range lines {
		flag, flagLen := formatFlag(l.Flag)
		col := 2 + flagLen
		b.WriteString("  ")
		b.WriteString(flag)
		if col < 38 {
			b.WriteString(strings.Repeat(" ", 38-col))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(l.Desc)
		b.WriteString("\n")
	}
	if footer != "" {
		b.WriteString("\n")
		b.WriteString(footer)
		if !strings.HasSuffix(footer, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
