package symbols_test

import (
	"context"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/symbols"
)

// Tree-sitter reports columns in bytes while spans are stored in runes. Without
// conversion, any symbol sitting after non-ASCII text on its own line produced
// a column that Slice could not resolve, so symbol-based relocation silently
// gave up for that file.
func TestExtractedSymbolsSliceBackCorrectlyWithNonASCII(t *testing.T) {
	cases := []struct {
		name     string
		language string
		path     string
		source   string
		symbol   string
		want     string
	}{
		{
			name:     "go symbol after multibyte text on the same line",
			language: "go",
			path:     "sample.go",
			source:   "package sample\n\nvar s = \"héllö wörld\"; func Add() int { return 1 }\n",
			symbol:   "Add",
			want:     "func Add() int { return 1 }",
		},
		{
			name:     "go symbol after a multibyte line",
			language: "go",
			path:     "sample.go",
			source:   "package sample\n\n// héllo wörld — em dash\nfunc Add(a int) int {\n\treturn a\n}\n",
			symbol:   "Add",
			want:     "func Add(a int) int {\n\treturn a\n}",
		},
		{
			name:     "python symbol after a multibyte line",
			language: "python",
			path:     "sample.py",
			source:   "# héllo wörld — em dash\ndef add(a, b):\n    return a + b\n",
			symbol:   "add",
			want:     "def add(a, b):\n    return a + b",
		},
	}

	svc := symbols.NewService()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := svc.Extract(context.Background(), tc.language, tc.path, []byte(tc.source))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			var matched bool
			for _, symbol := range found {
				if symbol.SymbolPath != tc.symbol {
					continue
				}
				matched = true
				got, err := code.Slice(tc.source, symbol.StartLine, symbol.StartCol, symbol.EndLine, symbol.EndCol)
				if err != nil {
					t.Fatalf("Slice at extracted position %d:%d-%d:%d: %v",
						symbol.StartLine, symbol.StartCol, symbol.EndLine, symbol.EndCol, err)
				}
				if got != tc.want {
					t.Errorf("sliced %q, want %q", got, tc.want)
				}
			}
			if !matched {
				t.Fatalf("symbol %q not extracted", tc.symbol)
			}
		})
	}
}

func TestExtractHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := symbols.NewService()
	if _, err := svc.Extract(ctx, "go", "sample.go", []byte("package sample\n")); err == nil {
		t.Error("Extract with a cancelled context returned no error")
	}
}
