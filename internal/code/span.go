package code

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
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
	startLine, startCol := lineColFromOffset(content, start)
	endLine, endCol := lineColFromOffset(content, end)
	return startLine, startCol, endLine, endCol
}

func Context(content string, startLine, endLine int, window int) (string, string) {
	lines := strings.Split(content, "\n")
	beforeStart := max(0, startLine-1-window)
	beforeEnd := max(0, startLine-1)
	afterStart := min(len(lines), endLine)
	afterEnd := min(len(lines), endLine+window)
	before := strings.Join(lines[beforeStart:beforeEnd], "\n")
	after := strings.Join(lines[afterStart:afterEnd], "\n")
	return before, after
}

func lineColFromOffset(content string, offset int) (int, int) {
	line := 1
	col := 1
	for idx, r := range content {
		if idx >= offset {
			break
		}
		if r == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
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
	line := 1
	col := 1
	for idx, r := range content {
		if line == targetLine && col == targetCol {
			return idx, nil
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	if line == targetLine && col == targetCol {
		return len(content), nil
	}
	return 0, errors.New("position out of range")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
