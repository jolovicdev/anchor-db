package resolver

import (
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/domain"
	"github.com/jolovicdev/anchor-db/internal/gitmap"
)

func spanAnchor(selected string, startLine, endLine int) domain.Anchor {
	return domain.Anchor{
		ID:     "anchor-1",
		Status: domain.AnchorStatusActive,
		Binding: domain.Binding{
			Type:         domain.BindingTypeSpan,
			Path:         "f.go",
			Language:     "go",
			StartLine:    startLine,
			StartCol:     1,
			EndLine:      endLine,
			EndCol:       2,
			SelectedText: selected,
			BaseCommit:   "abc123",
		},
	}
}

// Git mapping should win over text matching, and should say so.
func TestGitMappingTakesPrecedenceOverTextMatch(t *testing.T) {
	svc := New()
	selected := "func target() int {\n\treturn 1\n}"
	anchor := spanAnchor(selected, 1, 3)

	// Two lines inserted above, so the span moves from 1-3 to 3-5.
	content := "// added\n// added\n" + selected + "\n"
	lines := gitmap.Parse("@@ -0,0 +1,2 @@\n+// added\n+// added\n")

	result, err := svc.Resolve(Request{Anchor: anchor, Content: content, Lines: lines})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Status != domain.AnchorStatusActive {
		t.Fatalf("status = %q, want active", result.Status)
	}
	if result.Reason != "git line mapping" {
		t.Errorf("reason = %q, want %q", result.Reason, "git line mapping")
	}
	if result.Binding.StartLine != 3 || result.Binding.EndLine != 5 {
		t.Errorf("span = %d-%d, want 3-5", result.Binding.StartLine, result.Binding.EndLine)
	}
	if result.Confidence < 0.99 {
		t.Errorf("confidence = %v, want >= 0.99", result.Confidence)
	}
}

// The mapping is a hint, not an authority: if the text that landed at the
// mapped position is not the anchor's text, the mapping must be discarded
// rather than silently re-pointing the note at unrelated code.
func TestGitMappingIsRejectedWhenTextDoesNotMatch(t *testing.T) {
	svc := New()
	anchor := spanAnchor("func target() int {\n\treturn 1\n}", 1, 3)

	// The map says 1->1, but the content there is something else entirely and
	// the original text appears nowhere.
	content := "type Unrelated struct {\n\tX int\n}\n"
	lines := gitmap.Parse("@@ -5,0 +6,1 @@\n+// elsewhere\n")

	result, err := svc.Resolve(Request{Anchor: anchor, Content: content, Lines: lines})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Status != domain.AnchorStatusStale {
		t.Errorf("status = %q, want stale: the mapped location holds different code",
			result.Status)
	}
}

// When the span's boundaries survive but its interior was replaced, the diff
// can still place it, so the stale reason should say the code was rewritten
// rather than implying it vanished.
func TestStaleReasonSaysRewrittenWhenSpanSurvivesButContentChanged(t *testing.T) {
	selected := "func target() int {\n\tstep()\n\tstep()\n\treturn 1\n}"
	binding := spanAnchor(selected, 3, 7).Binding

	// Lines 5-6 (inside the span) replaced one-for-one; the ends are untouched.
	lines := gitmap.Parse("@@ -5,2 +5,2 @@\n-\tstep()\n-\tstep()\n+\tother()\n+\tother()\n")

	reason := staleReason(binding, lines)
	if !strings.Contains(reason, "rewritten") {
		t.Errorf("reason = %q, want it to mention the code being rewritten", reason)
	}
}

func TestStaleReasonSaysDeletedWhenSpanIsGone(t *testing.T) {
	binding := spanAnchor("gone", 5, 7).Binding
	lines := gitmap.Parse("@@ -5,3 +4,0 @@\n-a\n-b\n-c\n")

	reason := staleReason(binding, lines)
	if !strings.Contains(reason, "deleted") {
		t.Errorf("reason = %q, want it to mention deletion", reason)
	}
}

// With no diff base there is nothing git can say, so the generic reason stands.
func TestStaleReasonFallsBackWithoutALineMap(t *testing.T) {
	binding := spanAnchor("gone", 5, 7).Binding
	if reason := staleReason(binding, nil); reason != "no match" {
		t.Errorf("reason = %q, want %q", reason, "no match")
	}
}

// An unchanged file must not be treated as a git "move"; the exact span match
// should handle it and the git path should decline.
func TestGitMappingDeclinesOnAnUnchangedFile(t *testing.T) {
	binding := spanAnchor("x", 1, 1).Binding
	if _, ok := gitMatch("x\n", binding, gitmap.Parse("")); ok {
		t.Error("gitMatch should decline when the diff is empty")
	}
}
