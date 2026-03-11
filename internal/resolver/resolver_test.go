package resolver

import (
	"testing"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

func TestResolverMovesAnchorWhenLinesAreInserted(t *testing.T) {
	svc := New()
	anchor := domain.Anchor{
		ID:     "anchor-1",
		RepoID: "repo-1",
		Kind:   domain.AnchorKindWarning,
		Status: domain.AnchorStatusActive,
		Binding: domain.Binding{
			Type:             domain.BindingTypeSymbol,
			Path:             "calc.go",
			Language:         "go",
			SymbolPath:       "Add",
			StartLine:        1,
			StartCol:         1,
			EndLine:          3,
			EndCol:           2,
			SelectedText:     "func Add(a int, b int) int {\n\treturn a + b\n}",
			SelectedTextHash: "selected",
			BeforeContext:    "",
			BeforeHash:       "",
			AfterContext:     "func Mul(a int, b int) int {",
			AfterHash:        "after",
		},
	}

	content := "package calc\n\nimport \"fmt\"\n\nfunc Add(a int, b int) int {\n\treturn a + b\n}\n\nfunc Mul(a int, b int) int {\n\treturn a * b\n}\n"
	symbols := []domain.Symbol{
		{
			Path:       "calc.go",
			Language:   "go",
			Kind:       "function",
			SymbolPath: "Add",
			StartLine:  5,
			StartCol:   1,
			EndLine:    7,
			EndCol:     2,
		},
	}

	result, err := svc.Resolve(anchor, content, symbols)
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	if result.Status != domain.AnchorStatusActive {
		t.Fatalf("expected active status, got %s", result.Status)
	}
	if result.Binding.StartLine != 5 {
		t.Fatalf("expected start line 5, got %d", result.Binding.StartLine)
	}
	if result.Binding.Confidence < 0.9 {
		t.Fatalf("expected high confidence, got %f", result.Binding.Confidence)
	}
}

func TestResolverMarksAnchorStaleWhenCodeDisappears(t *testing.T) {
	svc := New()
	anchor := domain.Anchor{
		ID:     "anchor-1",
		RepoID: "repo-1",
		Kind:   domain.AnchorKindWarning,
		Status: domain.AnchorStatusActive,
		Binding: domain.Binding{
			Type:             domain.BindingTypeSpan,
			Path:             "calc.go",
			Language:         "go",
			StartLine:        1,
			StartCol:         1,
			EndLine:          3,
			EndCol:           2,
			SelectedText:     "func Add(a int, b int) int {\n\treturn a + b\n}",
			SelectedTextHash: "selected",
			AfterContext:     "func Mul(a int, b int) int {",
			AfterHash:        "after",
		},
	}

	result, err := svc.Resolve(anchor, "package calc\n", nil)
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	if result.Status != domain.AnchorStatusStale {
		t.Fatalf("expected stale status, got %s", result.Status)
	}
}

func TestResolverRejectsSymbolMatchWhenSelectedTextAndContextDoNotMatch(t *testing.T) {
	svc := New()
	anchor := domain.Anchor{
		ID:     "anchor-1",
		RepoID: "repo-1",
		Kind:   domain.AnchorKindWarning,
		Status: domain.AnchorStatusActive,
		Binding: domain.Binding{
			Type:             domain.BindingTypeSymbol,
			Path:             "calc.go",
			Language:         "go",
			SymbolPath:       "Add",
			StartLine:        1,
			StartCol:         1,
			EndLine:          3,
			EndCol:           2,
			SelectedText:     "func Add(a int, b int) int {\n\treturn a + b\n}",
			SelectedTextHash: "selected",
			BeforeContext:    "type Calculator struct{}",
			BeforeHash:       "before",
			AfterContext:     "func Mul(a int, b int) int {",
			AfterHash:        "after",
		},
	}

	content := "package calc\n\nfunc Add(values []int) int {\n\ttotal := 0\n\tfor _, value := range values {\n\t\ttotal += value\n\t}\n\treturn total\n}\n"
	symbols := []domain.Symbol{
		{
			Path:       "calc.go",
			Language:   "go",
			Kind:       "function",
			SymbolPath: "Add",
			StartLine:  3,
			StartCol:   1,
			EndLine:    8,
			EndCol:     2,
		},
	}

	result, err := svc.Resolve(anchor, content, symbols)
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	if result.Status != domain.AnchorStatusStale {
		t.Fatalf("expected stale status, got %s", result.Status)
	}
}

func TestLineSimilarityPenalizesReorderedLines(t *testing.T) {
	similarity := lineSimilarity(
		"first()\nsecond()\nthird()",
		"second()\nfirst()\nthird()",
	)
	if similarity >= 0.6 {
		t.Fatalf("expected reordered lines to fall below match threshold, got %f", similarity)
	}
}

func TestLineSimilarityKeepsInsertedLineSimilarEnough(t *testing.T) {
	similarity := lineSimilarity(
		"first()\nsecond()\nthird()",
		"first()\nlog()\nsecond()\nthird()",
	)
	if similarity < 0.6 {
		t.Fatalf("expected inserted line to stay above match threshold, got %f", similarity)
	}
}
