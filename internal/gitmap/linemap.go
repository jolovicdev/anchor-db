// Package gitmap turns a unified diff into a deterministic old->new line
// mapping.
//
// Text matching can only guess where a span went, and it guesses badly when the
// same code appears more than once in a file. A diff knows exactly: if git says
// twelve lines were inserted above a span, the span moved down twelve lines.
// This package extracts that answer so resolution can use it before falling
// back to similarity scoring.
//
// Diffs are expected to be generated with -U0. Zero context means every hunk is
// purely added and removed lines, so a line is either inside a changed region
// or cleanly outside one, with no context lines to disambiguate.
package gitmap

import (
	"regexp"
	"strconv"
	"strings"
)

// hunkHeader matches "@@ -oldStart[,oldCount] +newStart[,newCount] @@".
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
}

// LineMap maps line numbers from the "old" side of a diff to the "new" side.
// The zero value maps every line to itself, which is the correct behaviour for
// an unchanged file.
type LineMap struct {
	hunks []hunk
}

// Parse reads the hunk headers of a unified diff. Content lines are ignored;
// with -U0 the headers alone fully describe the mapping.
func Parse(diff string) *LineMap {
	var hunks []hunk
	for _, line := range strings.Split(diff, "\n") {
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		hunks = append(hunks, hunk{
			oldStart: atoi(match[1]),
			oldCount: countOrOne(match[2]),
			newStart: atoi(match[3]),
			newCount: countOrOne(match[4]),
		})
	}
	return &LineMap{hunks: hunks}
}

// Unchanged reports whether the diff contained no changes at all.
func (m *LineMap) Unchanged() bool {
	return m == nil || len(m.hunks) == 0
}

// MapLine returns where a 1-based old line number ended up. It reports false
// when the line fell inside a region the diff deleted, meaning the line no
// longer exists and no honest mapping is possible.
func (m *LineMap) MapLine(old int) (int, bool) {
	if old < 1 {
		return 0, false
	}
	if m == nil {
		return old, true
	}
	delta := 0
	for _, h := range m.hunks {
		// A pure insertion carries oldCount 0 and sits *after* oldStart, so
		// the line at oldStart itself is untouched by it.
		if h.oldCount == 0 {
			if old <= h.oldStart {
				break
			}
			delta += h.newCount
			continue
		}
		if old < h.oldStart {
			break
		}
		if old <= h.oldStart+h.oldCount-1 {
			return 0, false
		}
		delta += h.newCount - h.oldCount
	}
	return old + delta, true
}

// MapRange maps both ends of a span. It reports false unless both ends survive
// and the span stays well-formed, so a partially deleted range is rejected
// rather than silently shrunk to something the note never described.
func (m *LineMap) MapRange(startLine, endLine int) (int, int, bool) {
	mappedStart, ok := m.MapLine(startLine)
	if !ok {
		return 0, 0, false
	}
	mappedEnd, ok := m.MapLine(endLine)
	if !ok {
		return 0, 0, false
	}
	if mappedEnd < mappedStart {
		return 0, 0, false
	}
	// The span must keep its shape; a different height means lines inside it
	// were added or removed and the mapping is no longer a pure translation.
	if mappedEnd-mappedStart != endLine-startLine {
		return 0, 0, false
	}
	return mappedStart, mappedEnd, true
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

// A hunk header omits the count when it is exactly 1.
func countOrOne(value string) int {
	if value == "" {
		return 1
	}
	return atoi(value)
}
