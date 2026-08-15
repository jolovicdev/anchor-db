package code_test

import (
	"testing"

	"github.com/jolovicdev/anchor-db/internal/code"
)

const clampSample = "alpha\nbravo charlie\ndelta\n"

// Requiring an exact end column made callers responsible for knowing each
// line's width, and silently broke relocation previews, which reuse an old
// span's columns at a new location where they often do not fit.
func TestSliceClampsColumnsPastEndOfLine(t *testing.T) {
	cases := []struct {
		name                                 string
		startLine, startCol, endLine, endCol int
		want                                 string
	}{
		{"exact end column", 1, 1, 1, 6, "alpha"},
		{"one past the end", 1, 1, 1, 7, "alpha"},
		{"far past the end", 1, 1, 1, 9999, "alpha"},
		{"across lines, end clamped", 1, 1, 2, 9999, "alpha\nbravo charlie"},
		{"start column clamped", 1, 99, 2, 6, "\nbravo"},
		{"whole file", 1, 1, 3, 9999, "alpha\nbravo charlie\ndelta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := code.Slice(clampSample, tc.startLine, tc.startCol, tc.endLine, tc.endCol)
			if err != nil {
				t.Fatalf("Slice: %v", err)
			}
			if got != tc.want {
				t.Errorf("Slice = %q, want %q", got, tc.want)
			}
		})
	}
}

// Clamping columns must not extend to lines. A line that does not exist is a
// real mistake, not an imprecise way of saying "the end".
func TestSliceStillRejectsMissingLines(t *testing.T) {
	// The sample ends with a newline, so line 4 is the empty position after it
	// and is legitimately addressable. Line 5 is not.
	for _, line := range []int{5, 50} {
		if _, err := code.Slice(clampSample, 1, 1, line, 1); err == nil {
			t.Errorf("Slice to line %d should fail: the file ends at line 4", line)
		}
	}
	if _, err := code.Slice(clampSample, 0, 1, 1, 1); err == nil {
		t.Error("Slice from line 0 should fail")
	}
}

// Columns are rune offsets, so clamping has to walk runes rather than bytes.
func TestSliceClampsCorrectlyWithMultibyteText(t *testing.T) {
	content := "héllo wörld\nnext\n"
	got, err := code.Slice(content, 1, 1, 1, 9999)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if got != "héllo wörld" {
		t.Errorf("Slice = %q, want the whole first line", got)
	}
}
