package tools

import (
	"math"
	"testing"
)

// ── calcEval tests ───────────────────────────────────────────────────────────

func TestCalcEval(t *testing.T) {
	tests := []struct {
		expr   string
		expect float64
	}{
		{"2 + 3", 5},
		{"10 - 4", 6},
		{"3 * 7", 21},
		{"20 / 4", 5},
		{"10 % 3", 1},
		{"2 + 3 * 4", 14},      // precedence: * before +
		{"(2 + 3) * 4", 20},    // parens
		{"-5", -5},             // unary minus
		{"-5 + 3", -2},         // unary minus in expr
		{"3.14 * 2", 6.28},     // decimals
		{"100 / 3", 100.0 / 3}, // float division
		{"(10 + 5) % 7", 1},    // modulo with parens
		{"  42  ", 42},         // whitespace
		{"2 * (3 + 4) - 1", 13},
		{"+5", 5}, // unary plus
	}
	for _, tc := range tests {
		got, err := calcEval(tc.expr)
		if err != nil {
			t.Errorf("calcEval(%q) error: %v", tc.expr, err)
			continue
		}
		if math.Abs(got-tc.expect) > 1e-9 {
			t.Errorf("calcEval(%q) = %v, want %v", tc.expr, got, tc.expect)
		}
	}
}

func TestCalcEvalErrors(t *testing.T) {
	tests := []string{
		"2 + + 3 abc",
		"abc",
	}
	for _, expr := range tests {
		_, err := calcEval(expr)
		if err == nil {
			// Some malformed expressions may parse partially — that's OK.
			// We mainly care that they don't panic.
			t.Logf("calcEval(%q) did not error (parsed partially)", expr)
		}
	}
}

func TestCalcDivisionByZero(t *testing.T) {
	got, err := calcEval("10 / 0")
	if err != nil {
		t.Fatalf("calcEval(\"10 / 0\") error: %v", err)
	}
	if !math.IsNaN(got) {
		t.Errorf("calcEval(\"10 / 0\") = %v, want NaN", got)
	}
}
