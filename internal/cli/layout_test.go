package cli

import (
	"strings"
	"testing"
)

// A value exactly as wide as its field must not run into the next column.
// "standard" is eight characters and the risk field is eight wide, which
// is how "standard5 → 25 → 50 → 100" reached a live run of the report.
func TestLabeledSection_ValueFillingItsField_KeepsASeparator(t *testing.T) {
	s := labeledSection{
		title:     "Assessment",
		tailWidth: 8,
		rows: []labeledRow{
			{label: "lane", value: "standard", tail: "5 → 25 → 50 → 100   (3 gates)"},
			{label: "lane", value: "fast", tail: "5 → 100   (1 gate)"},
		},
	}
	var b strings.Builder
	s.render(&b, 18)

	if strings.Contains(b.String(), "standard5") {
		t.Fatalf("a full-width value must not butt against the next column:\n%s", b.String())
	}
	// The narrower lanes still land on the column Appendix A puts them on.
	if !strings.Contains(b.String(), "  lane              fast    5 → 100") {
		t.Fatalf("padding a short value must not change:\n%s", b.String())
	}
}

// The wrap point Appendix A's two rationales agree on: a greedy fill to
// the report width, backing up to a clause boundary only when it is
// within a word of where the line would have broken anyway.
func TestWrapRationale_MatchesAppendixAsTwoRationales(t *testing.T) {
	const indent = 28

	safe := wrapRationale(`"single-line version constant; no request path, no configuration, no error handling touched"`, indent)
	wantSafe := []string{
		`"single-line version constant; no request path,`,
		`no configuration, no error handling touched"`,
	}
	assertLines(t, "A2.1", safe, wantSafe)

	risky := wrapRationale(`"echo handler returns on the error path before writing a status code; under load this produces empty 200s, not 5xx, so readiness will not catch it"`, indent)
	wantRisky := []string{
		`"echo handler returns on the error path before`,
		`writing a status code; under load this produces`,
		`empty 200s, not 5xx, so readiness will not catch it"`,
	}
	assertLines(t, "A3.1", risky, wantRisky)

	for _, line := range append(safe, risky...) {
		if indent+runeLen(line) > reportWidth {
			t.Errorf("line runs past the report width: %q", line)
		}
	}
}

func assertLines(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s wrapped to %d lines, want %d: %q", name, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s line %d:\n want %q\n  got %q", name, i+1, want[i], got[i])
		}
	}
}
