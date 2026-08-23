package color

import (
	"fmt"
	"testing"
)

func TestColorEnabled(t *testing.T) {
	tests := []struct {
		name string
		tty  bool
		env  map[string]string
		want bool
	}{
		{name: "truecolor", tty: true, env: map[string]string{"COLORTERM": "truecolor"}, want: true},
		{name: "24bit", tty: true, env: map[string]string{"COLORTERM": "24bit"}, want: true},
		{name: "term 256color", tty: true, env: map[string]string{"TERM": "xterm-256color"}, want: true},
		{name: "non tty", env: map[string]string{"COLORTERM": "truecolor"}},
		{name: "no color", tty: true, env: map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor"}},
		{name: "dumb terminal", tty: true, env: map[string]string{"TERM": "dumb", "COLORTERM": "truecolor"}},
		{name: "no 256 color", tty: true, env: map[string]string{"TERM": "xterm"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(key string) string { return test.env[key] }
			if got := colorEnabled(getenv, test.tty); got != test.want {
				t.Fatalf("colorEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestHelpers(t *testing.T) {
	tests := []struct {
		name string
		fn   func(any) string
		code string
	}{
		{name: "Gra5", fn: Gra5, code: "38;5;245"},
		{name: "Grn5", fn: Grn5, code: "38;5;46"},
		{name: "Red5", fn: Red5, code: "38;5;196"},
		{name: "Whi5", fn: Whi5, code: "38;5;231"},
		{name: "Whi9", fn: Whi9, code: "38;5;255"},
		{name: "Yel5", fn: Yel5, code: "38;5;220"},
	}

	previous := enabled
	t.Cleanup(func() { enabled = previous })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			enabled = true
			want := "\x1b[" + test.code + "m42\x1b[0m"
			if got := test.fn(42); got != want {
				t.Fatalf("enabled helper = %q, want %q", got, want)
			}

			enabled = false
			if got := test.fn(42); got != "42" {
				t.Fatalf("disabled helper = %q, want %q", got, "42")
			}
		})
	}
}

func TestHelpersUseSprint(t *testing.T) {
	previous := enabled
	enabled = false
	t.Cleanup(func() { enabled = previous })

	value := struct{ Name string }{Name: "iq"}
	for _, test := range []struct {
		name string
		fn   func(any) string
	}{
		{name: "Gra5", fn: Gra5},
		{name: "Grn5", fn: Grn5},
		{name: "Red5", fn: Red5},
		{name: "Whi5", fn: Whi5},
		{name: "Whi9", fn: Whi9},
		{name: "Yel5", fn: Yel5},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, want := test.fn(value), fmt.Sprint(value); got != want {
				t.Fatalf("helper(value) = %q, want %q", got, want)
			}
		})
	}
}
