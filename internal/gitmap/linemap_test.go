package gitmap_test

import (
	"testing"

	"github.com/jolovicdev/anchor-db/internal/gitmap"
)

func TestMapLineShiftsAfterInsertion(t *testing.T) {
	// Three lines inserted after old line 4.
	diff := "@@ -4,0 +5,3 @@\n+one\n+two\n+three\n"
	m := gitmap.Parse(diff)

	cases := map[int]int{
		1: 1, // before the insertion, unmoved
		4: 4, // the insertion sits after this line
		5: 8, // everything after shifts down by three
		9: 12,
	}
	for old, want := range cases {
		got, ok := m.MapLine(old)
		if !ok {
			t.Errorf("MapLine(%d) reported deleted", old)
			continue
		}
		if got != want {
			t.Errorf("MapLine(%d) = %d, want %d", old, got, want)
		}
	}
}

func TestMapLineShiftsAfterDeletion(t *testing.T) {
	// Old lines 3-4 removed.
	diff := "@@ -3,2 +2,0 @@\n-gone\n-also gone\n"
	m := gitmap.Parse(diff)

	if got, ok := m.MapLine(2); !ok || got != 2 {
		t.Errorf("MapLine(2) = %d,%v want 2,true", got, ok)
	}
	for _, deleted := range []int{3, 4} {
		if _, ok := m.MapLine(deleted); ok {
			t.Errorf("MapLine(%d) should report the line as deleted", deleted)
		}
	}
	if got, ok := m.MapLine(5); !ok || got != 3 {
		t.Errorf("MapLine(5) = %d,%v want 3,true", got, ok)
	}
}

func TestMapLineAcrossMultipleHunks(t *testing.T) {
	diff := "@@ -2,0 +3,2 @@\n+a\n+b\n@@ -10,3 +12,1 @@\n-x\n-y\n-z\n+w\n"
	m := gitmap.Parse(diff)

	// Before everything.
	if got, _ := m.MapLine(1); got != 1 {
		t.Errorf("MapLine(1) = %d, want 1", got)
	}
	// Between the hunks: shifted by the first insertion only.
	if got, _ := m.MapLine(5); got != 7 {
		t.Errorf("MapLine(5) = %d, want 7", got)
	}
	// Inside the second hunk's deleted range.
	if _, ok := m.MapLine(11); ok {
		t.Error("MapLine(11) should report the line as deleted")
	}
	// After both: +2 from the insertion, -2 from the replacement.
	if got, _ := m.MapLine(13); got != 13 {
		t.Errorf("MapLine(13) = %d, want 13", got)
	}
}

func TestHunkHeaderWithoutExplicitCount(t *testing.T) {
	// git omits the count when it is exactly 1.
	m := gitmap.Parse("@@ -5 +5 @@\n-old\n+new\n")
	if _, ok := m.MapLine(5); ok {
		t.Error("MapLine(5) should report the replaced line as deleted")
	}
	if got, ok := m.MapLine(6); !ok || got != 6 {
		t.Errorf("MapLine(6) = %d,%v want 6,true", got, ok)
	}
}

func TestUnchangedFileMapsIdentically(t *testing.T) {
	m := gitmap.Parse("")
	if !m.Unchanged() {
		t.Error("empty diff should report Unchanged")
	}
	for _, line := range []int{1, 50, 1000} {
		if got, ok := m.MapLine(line); !ok || got != line {
			t.Errorf("MapLine(%d) = %d,%v want identity", line, got, ok)
		}
	}
}

func TestMapRangeRejectsReshapedSpans(t *testing.T) {
	// Two lines inserted in the middle of the 5-8 span.
	m := gitmap.Parse("@@ -6,0 +7,2 @@\n+a\n+b\n")
	if _, _, ok := m.MapRange(5, 8); ok {
		t.Error("MapRange should reject a span whose interior changed height")
	}
	// A span entirely below the insertion translates cleanly.
	start, end, ok := m.MapRange(10, 12)
	if !ok {
		t.Fatal("MapRange(10,12) should map")
	}
	if start != 12 || end != 14 {
		t.Errorf("MapRange(10,12) = %d,%d want 12,14", start, end)
	}
}

func TestMapRangeRejectsDeletedSpans(t *testing.T) {
	m := gitmap.Parse("@@ -5,3 +5,0 @@\n-a\n-b\n-c\n")
	if _, _, ok := m.MapRange(5, 7); ok {
		t.Error("MapRange over a deleted region should fail")
	}
}

func TestParseIgnoresDiffNoise(t *testing.T) {
	diff := `diff --git a/sample.go b/sample.go
index 1234567..89abcde 100644
--- a/sample.go
+++ b/sample.go
@@ -2,0 +3,1 @@
+inserted
`
	m := gitmap.Parse(diff)
	if m.Unchanged() {
		t.Fatal("expected one hunk to be parsed")
	}
	if got, _ := m.MapLine(3); got != 4 {
		t.Errorf("MapLine(3) = %d, want 4", got)
	}
}
