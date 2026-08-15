// Package code converts between byte offsets and the line/column coordinates
// AnchorDB stores in bindings.
//
// Lines are 1-based. Columns are 1-based *rune* offsets within their line, not
// byte offsets. Producers that natively work in bytes -- tree-sitter, for one --
// must convert through PositionIndex before writing a span, otherwise any line
// containing non-ASCII text yields a column that Slice cannot resolve.
package code

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

func HashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func Slice(content string, startLine, startCol, endLine, endCol int) (string, error) {
	start, end, err := offsets(content, startLine, startCol, endLine, endCol)
	if err != nil {
		return "", err
	}
	return content[start:end], nil
}

func Offsets(content string, startLine, startCol, endLine, endCol int) (int, int, error) {
	return offsets(content, startLine, startCol, endLine, endCol)
}

func RangeFromOffsets(content string, start, end int) (int, int, int, int) {
	index := NewPositionIndex(content)
	startLine, startCol := index.LineCol(start)
	endLine, endCol := index.LineCol(end)
	return startLine, startCol, endLine, endCol
}

// PositionIndex converts byte offsets in a fixed piece of content into the
// line/rune-column coordinates described in the package docs. Building it once
// per file keeps conversion cheap when a file yields many symbols.
type PositionIndex struct {
	content    string
	lineStarts []int
}

func NewPositionIndex(content string) *PositionIndex {
	starts := make([]int, 1, strings.Count(content, "\n")+1)
	for idx := 0; idx < len(content); idx++ {
		if content[idx] == '\n' {
			starts = append(starts, idx+1)
		}
	}
	return &PositionIndex{content: content, lineStarts: starts}
}

// LineCol returns the 1-based line and 1-based rune column for a byte offset.
// Offsets outside the content are clamped to its bounds.
func (p *PositionIndex) LineCol(offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(p.content) {
		offset = len(p.content)
	}
	line := sort.Search(len(p.lineStarts), func(i int) bool {
		return p.lineStarts[i] > offset
	}) - 1
	if line < 0 {
		line = 0
	}
	return line + 1, utf8.RuneCountInString(p.content[p.lineStarts[line]:offset]) + 1
}

// Context returns the `window` lines immediately before startLine and
// immediately after endLine. Line numbers outside the content are clamped, so
// a stale binding pointing past the end of a shrunken file yields empty
// context instead of panicking.
func Context(content string, startLine, endLine int, window int) (string, string) {
	lines := strings.Split(content, "\n")
	clamp := func(value int) int {
		return MinInt(MaxInt(0, value), len(lines))
	}
	beforeEnd := clamp(startLine - 1)
	beforeStart := MinInt(clamp(startLine-1-window), beforeEnd)
	afterStart := clamp(endLine)
	afterEnd := MaxInt(clamp(endLine+window), afterStart)
	before := strings.Join(lines[beforeStart:beforeEnd], "\n")
	after := strings.Join(lines[afterStart:afterEnd], "\n")
	return before, after
}

func offsets(content string, startLine, startCol, endLine, endCol int) (int, int, error) {
	start, err := offsetFor(content, startLine, startCol)
	if err != nil {
		return 0, 0, err
	}
	end, err := offsetFor(content, endLine, endCol)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, errors.New("invalid range")
	}
	return start, end, nil
}

func offsetFor(content string, targetLine, targetCol int) (int, error) {
	if targetLine < 1 || targetCol < 1 {
		return 0, errors.New("line and column must be positive")
	}

	// Walk to the start of the target line. A line that does not exist is a real
	// mistake and still fails.
	lineStart := 0
	for current := 1; current < targetLine; current++ {
		idx := strings.IndexByte(content[lineStart:], '\n')
		if idx < 0 {
			return 0, errors.New("position out of range")
		}
		lineStart += idx + 1
	}
	lineEnd := len(content)
	if idx := strings.IndexByte(content[lineStart:], '\n'); idx >= 0 {
		lineEnd = lineStart + idx
	}

	// A column past the end of its line is clamped to the end of that line.
	// "column 999" and "the end of this line" are the same request, and refusing
	// it made callers responsible for knowing every line's exact width. It also
	// broke relocation previews, which reuse an old span's columns at a new
	// location where those columns often do not fit.
	offset := lineStart
	for col := 1; col < targetCol && offset < lineEnd; col++ {
		_, size := utf8.DecodeRuneInString(content[offset:])
		offset += size
	}
	return offset, nil
}

func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
