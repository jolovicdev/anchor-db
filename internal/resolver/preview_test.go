package resolver_test

import (
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
	"github.com/jolovicdev/anchor-db/internal/resolver"
)

const beforeRewrite = `class Store:
    def lookup(self, key):
        for item in self.items:
            if item.key == key:
                return item
        raise KeyError(key)
`

const afterRewrite = `class Store:
    def __init__(self):
        self.items = []
        self.index = {}

    def lookup(self, key):
        found = self.index.get(key)
        if found is None:
            raise KeyError(key)
        return found
`

func anchoredOn(t *testing.T, content string, startLine, startCol, endLine, endCol int, symbol string) domain.Anchor {
	t.Helper()
	selected, err := code.Slice(content, startLine, startCol, endLine, endCol)
	if err != nil {
		t.Fatalf("slice fixture: %v", err)
	}
	before, after := code.Context(content, startLine, endLine, 1)
	return domain.Anchor{
		Binding: domain.Binding{
			Type: domain.BindingTypeSymbol, Path: "svc.py", SymbolPath: symbol,
			StartLine: startLine, StartCol: startCol, EndLine: endLine, EndCol: endCol,
			SelectedText: selected, SelectedTextHash: code.HashText(selected),
			BeforeContext: before, AfterContext: after,
		},
	}
}

// A preview answers "does this candidate look right?", so it has to describe
// what is at the proposed location now. Echoing the anchor's own text showed
// the reviewer the code they were trying to move away from.
func TestCandidatePreviewsShowCurrentContent(t *testing.T) {
	anchor := anchoredOn(t, beforeRewrite, 2, 1, 6, 24, "lookup")

	candidates := resolver.New().Candidates(resolver.Request{
		Anchor:  anchor,
		Content: afterRewrite,
		Symbols: []domain.Symbol{{
			SymbolPath: "lookup", StartLine: 6, StartCol: 1, EndLine: 10, EndCol: 21,
		}},
	}, 3)

	if len(candidates) == 0 {
		t.Fatal("no candidates offered for a rewritten body")
	}
	for _, candidate := range candidates {
		preview := candidate.Binding.SelectedText
		if strings.Contains(preview, "for item in self.items") {
			t.Errorf("candidate for lines %d-%d previews the anchor's old text:\n%s",
				candidate.Binding.StartLine, candidate.Binding.EndLine, preview)
		}
		// Whatever it proposes must actually appear in the current file.
		if !strings.Contains(afterRewrite, strings.TrimSpace(strings.Split(preview, "\n")[0])) {
			t.Errorf("candidate preview is not present in the current content:\n%s", preview)
		}
	}
}

const wideClass = `class Task:
    def one(self):
        return 1
    def parse(self, raw):
        return int(raw)
    def two(self):
        return 2
    def three(self):
        return 3
`

const wideClassRewritten = `class Task:
    def one(self):
        return 1
    def parse(self, value, *, strict=True):
        return self._coerce(value, strict)
    def two(self):
        return 2
    def three(self):
        return 3
`

// When a body is rewritten outright, no line of the original survives and the
// line-seeded scan has nothing to work from. The code around it usually does
// survive, and brackets where the anchor belongs.
func TestContextCandidateFindsARewrittenSpan(t *testing.T) {
	anchor := anchoredOn(t, wideClass, 4, 1, 5, 25, "parse")

	candidates := resolver.New().Candidates(resolver.Request{
		Anchor:  anchor,
		Content: wideClassRewritten,
		// Deliberately no symbols: this is the case where symbol extraction
		// gives nothing usable and only the surroundings are left.
		Symbols: nil,
	}, 3)

	if len(candidates) == 0 {
		t.Fatal("no candidate offered for a rewritten span with surviving context")
	}
	best := candidates[0]
	if best.Binding.StartLine != 4 || best.Binding.EndLine != 5 {
		t.Errorf("best candidate is lines %d-%d, want 4-5",
			best.Binding.StartLine, best.Binding.EndLine)
	}
	// The candidate carries text read from the current content, not the anchor's
	// own. It is sliced with the anchor's columns, so it may stop short of the
	// line's end -- callers render whole lines -- but it must not be the old code.
	if strings.Contains(best.Binding.SelectedText, "int(raw)") {
		t.Errorf("candidate carries the anchor's old text:\n%s", best.Binding.SelectedText)
	}
	if !strings.Contains(best.Binding.SelectedText, "def parse(self, value") {
		t.Errorf("candidate should carry the rewritten code, got:\n%s", best.Binding.SelectedText)
	}
}
