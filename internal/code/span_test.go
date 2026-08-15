package code_test

import (
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/code"
)

// Context is called with line numbers taken from stored bindings, which can
// point past the end of a file that has since shrunk.
func TestContextClampsOutOfRangeLines(t *testing.T) {
	content := "one\ntwo\nthree\n"
	cases := []struct{ startLine, endLine, window int }{
		{100, 101, 1},
		{1, 1, 1},
		{1, 1, 50},
		{3, 3, 2},
		{-5, -1, 3},
		{0, 0, 1},
		{2, 100, 1},
	}
	for _, tc := range cases {
		// Before clamping this panicked with a slice-bounds error.
		before, after := code.Context(content, tc.startLine, tc.endLine, tc.window)
		if strings.Contains(before, "\x00") || strings.Contains(after, "\x00") {
			t.Errorf("Context(%d,%d,%d) produced corrupt output", tc.startLine, tc.endLine, tc.window)
		}
	}
}

func TestContextReturnsSurroundingLines(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\n"
	before, after := code.Context(content, 3, 3, 1)
	if before != "two" {
		t.Errorf("before = %q, want %q", before, "two")
	}
	if after != "four" {
		t.Errorf("after = %q, want %q", after, "four")
	}
}

func TestPositionIndexCountsColumnsInRunes(t *testing.T) {
	content := "package sample\nvar s = \"héllö\"; var t = 1\n"
	index := code.NewPositionIndex(content)

	// Byte offset of the second `var`, which sits after two 2-byte runes.
	offset := strings.Index(content, "var t")
	line, col := index.LineCol(offset)
	if line != 2 {
		t.Fatalf("line = %d, want 2", line)
	}

	// The reported column must round-trip through Slice, which counts runes.
	got, err := code.Slice(content, line, col, line, col+len("var t = 1"))
	if err != nil {
		t.Fatalf("Slice at reported position: %v", err)
	}
	if got != "var t = 1" {
		t.Errorf("Slice = %q, want %q", got, "var t = 1")
	}
}

func TestPositionIndexClampsOutOfRangeOffsets(t *testing.T) {
	index := code.NewPositionIndex("a\nb\n")
	if line, col := index.LineCol(-10); line != 1 || col != 1 {
		t.Errorf("LineCol(-10) = %d:%d, want 1:1", line, col)
	}
	if line, _ := index.LineCol(1000); line < 1 {
		t.Errorf("LineCol(1000) returned line %d, want >= 1", line)
	}
}

func TestSliceRoundTripsWithRangeFromOffsets(t *testing.T) {
	content := "alpha\nbétä\ngamma\n"
	start := strings.Index(content, "bétä")
	end := start + len("bétä")

	startLine, startCol, endLine, endCol := code.RangeFromOffsets(content, start, end)
	got, err := code.Slice(content, startLine, startCol, endLine, endCol)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if got != "bétä" {
		t.Errorf("round trip = %q, want %q", got, "bétä")
	}
}
