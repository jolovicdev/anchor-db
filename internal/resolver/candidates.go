package resolver

import (
	"sort"
	"strings"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
)

// Candidate is a place an anchor might belong, offered for a human (or an
// agent) to accept or reject.
type Candidate struct {
	Binding    domain.Binding
	Confidence float64
	Reason     string
}

// minCandidateConfidence drops suggestions too weak to be worth reading.
const minCandidateConfidence = 0.3

// Candidates proposes relocations for an anchor that automatic resolution could
// not place, ranked most plausible first.
//
// Where Resolve stops at the first strategy confident enough to act on its own,
// this gathers every strategy's opinion, including the near-misses Resolve
// rejects. A stale anchor is precisely the case where no single strategy was
// certain, so the useful output is a shortlist, not a verdict.
func (s *Service) Candidates(request Request, limit int) []Candidate {
	binding := request.Anchor.Binding
	if binding.SelectedText == "" || request.Content == "" {
		return nil
	}
	if limit <= 0 {
		limit = 3
	}

	var found []Candidate
	// A zero Reason marks a strategy that had nothing to offer.
	if candidate := gitCandidate(request, binding); candidate.Reason != "" {
		found = append(found, candidate)
	}
	found = append(found, symbolCandidates(request, binding)...)
	found = append(found, textCandidates(request, binding)...)
	found = append(found, similarityCandidates(request, binding)...)
	found = append(found, contextCandidates(request, binding)...)

	// Several strategies routinely land on the same lines; keep the strongest
	// opinion for each distinct location.
	best := map[[2]int]Candidate{}
	for _, candidate := range found {
		if candidate.Confidence < minCandidateConfidence {
			continue
		}
		key := [2]int{candidate.Binding.StartLine, candidate.Binding.EndLine}
		if existing, ok := best[key]; ok && existing.Confidence >= candidate.Confidence {
			continue
		}
		best[key] = candidate
	}

	ranked := make([]Candidate, 0, len(best))
	for _, candidate := range best {
		ranked = append(ranked, candidate)
	}
	// A total order, not just a good one: callers accept a candidate by index
	// and the list is recomputed before it is applied, so two candidates tying
	// on confidence and start line could swap between the preview and the
	// accept, pinning a span the reviewer never saw.
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Confidence != ranked[j].Confidence {
			return ranked[i].Confidence > ranked[j].Confidence
		}
		if ranked[i].Binding.StartLine != ranked[j].Binding.StartLine {
			return ranked[i].Binding.StartLine < ranked[j].Binding.StartLine
		}
		if ranked[i].Binding.EndLine != ranked[j].Binding.EndLine {
			return ranked[i].Binding.EndLine < ranked[j].Binding.EndLine
		}
		return ranked[i].Reason < ranked[j].Reason
	})

	// The sliding-window scan naturally proposes the same region at several
	// offsets. Offering "lines 13-17" and "lines 14-18" as separate choices is
	// noise, so once a region is suggested, later overlaps are dropped.
	kept := make([]Candidate, 0, limit)
	for _, candidate := range ranked {
		if len(kept) == limit {
			break
		}
		if overlapsAny(kept, candidate) {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

// wholeLines returns a line range verbatim, used when a span's columns do not
// describe anything meaningful at a proposed location.
func wholeLines(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		return ""
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

// contextCandidates proposes the region between an anchor's recorded
// surroundings.
//
// When a body is rewritten outright, no line of the original survives, so the
// line-seeded scan has nothing to work from and the only suggestion left is the
// enclosing symbol -- often dozens of lines, at a confidence that says little.
// The code *around* the anchor usually does survive, and it brackets where the
// anchor belongs even when everything between has changed.
func contextCandidates(request Request, binding domain.Binding) []Candidate {
	before := strings.TrimSpace(binding.BeforeContext)
	after := strings.TrimSpace(binding.AfterContext)
	if before == "" && after == "" {
		return nil
	}

	lines := strings.Split(request.Content, "\n")
	find := func(needle string) int {
		if needle == "" {
			return -1
		}
		for idx, line := range lines {
			if strings.TrimSpace(line) == needle {
				return idx + 1
			}
		}
		return -1
	}

	beforeLine, afterLine := find(before), find(after)
	switch {
	case beforeLine > 0 && afterLine > beforeLine+1:
		// Both anchors of the window survived and still bracket a region.
		return []Candidate{buildCandidate(request.Content, binding,
			beforeLine+1, afterLine-1, 0.55, "between the surrounding lines")}
	case beforeLine > 0:
		// Only the leading context survived; offer a span of the original height
		// starting after it.
		height := strings.Count(binding.SelectedText, "\n")
		end := beforeLine + 1 + height
		if end > len(lines) {
			end = len(lines)
		}
		return []Candidate{buildCandidate(request.Content, binding,
			beforeLine+1, end, 0.45, "after the preceding line")}
	case afterLine > 1:
		height := strings.Count(binding.SelectedText, "\n")
		start := afterLine - 1 - height
		if start < 1 {
			start = 1
		}
		return []Candidate{buildCandidate(request.Content, binding,
			start, afterLine-1, 0.45, "before the following line")}
	}
	return nil
}

// overlapsAny reports whether a candidate is already covered by one that was
// kept.
//
// Suppressing every overlap is right for the sliding-window scan, which
// proposes the same region at several offsets. It is wrong when a broad
// suggestion -- an enclosing symbol spanning dozens of lines -- swallows a
// narrow one pointing at the handful of lines that actually changed. The
// narrow candidate is the more useful answer, so a substantially tighter span
// survives the overlap.
func overlapsAny(kept []Candidate, candidate Candidate) bool {
	height := func(c Candidate) int { return c.Binding.EndLine - c.Binding.StartLine + 1 }
	for _, existing := range kept {
		if candidate.Binding.StartLine > existing.Binding.EndLine ||
			existing.Binding.StartLine > candidate.Binding.EndLine {
			continue
		}
		// Half the height or less is a meaningfully different proposal rather
		// than the same region rediscovered at another offset.
		if height(candidate)*2 <= height(existing) {
			continue
		}
		return true
	}
	return false
}

// gitCandidate offers wherever the diff says the span went, even when the text
// there has changed -- that is exactly the "this code was rewritten in place"
// case a reviewer most wants to see.
func gitCandidate(request Request, binding domain.Binding) Candidate {
	if request.Lines == nil {
		return Candidate{}
	}
	startLine, endLine, ok := request.Lines.MapRange(binding.StartLine, binding.EndLine)
	if !ok {
		return Candidate{}
	}
	found, err := code.Slice(request.Content, startLine, binding.StartCol, endLine, binding.EndCol)
	if err != nil {
		return Candidate{}
	}
	confidence := 0.99
	if found != binding.SelectedText {
		confidence = 0.6 + 0.3*lineSimilarity(binding.SelectedText, found)
	}
	return buildCandidate(request.Content, binding, startLine, endLine, confidence, "git line mapping")
}

func symbolCandidates(request Request, binding domain.Binding) []Candidate {
	if binding.SymbolPath == "" {
		return nil
	}
	var out []Candidate
	for _, symbol := range request.Symbols {
		if symbol.SymbolPath != binding.SymbolPath {
			continue
		}
		found, err := code.Slice(request.Content, symbol.StartLine, symbol.StartCol, symbol.EndLine, symbol.EndCol)
		if err != nil {
			continue
		}
		confidence := 0.5 + 0.45*lineSimilarity(binding.SelectedText, found)
		if found == binding.SelectedText {
			confidence = 0.97
		}
		candidate := buildCandidate(request.Content, binding, symbol.StartLine, symbol.EndLine, confidence, "symbol "+symbol.SymbolPath)
		candidate.Binding.StartCol = symbol.StartCol
		candidate.Binding.EndCol = symbol.EndCol
		candidate.Binding.Type = domain.BindingTypeSymbol
		out = append(out, candidate)
	}
	return out
}

func textCandidates(request Request, binding domain.Binding) []Candidate {
	var out []Candidate
	for _, start := range occurrences(request.Content, binding.SelectedText) {
		startLine, _, endLine, _ := code.RangeFromOffsets(request.Content, start, start+len(binding.SelectedText))
		before, after := surrounding(request.Content, start, start+len(binding.SelectedText))
		confidence := 0.8
		if binding.BeforeContext != "" && strings.Contains(before, binding.BeforeContext) {
			confidence += 0.05
		}
		if binding.AfterContext != "" && strings.Contains(after, binding.AfterContext) {
			confidence += 0.05
		}
		out = append(out, buildCandidate(request.Content, binding, startLine, endLine, confidence, "exact text match"))
	}
	return out
}

// similarityCandidates finds regions that read like the anchored code without
// matching it exactly, which is the only signal left once the code has been
// edited rather than moved.
//
// Scanning every window of a large file would be wasteful, so candidate
// positions come from lines that appear in the original span; a rewrite that
// shares no line with the original is not something this can honestly rank.
func similarityCandidates(request Request, binding domain.Binding) []Candidate {
	wanted := nonEmptyLines(binding.SelectedText)
	if len(wanted) == 0 {
		return nil
	}
	needles := make(map[string]struct{}, len(wanted))
	for _, line := range wanted {
		needles[line] = struct{}{}
	}

	height := strings.Count(binding.SelectedText, "\n")
	contentLines := strings.Split(request.Content, "\n")
	starts := map[int]struct{}{}
	for idx, line := range contentLines {
		if _, ok := needles[strings.TrimSpace(line)]; !ok {
			continue
		}
		// The matching line could sit anywhere inside the span, so try windows
		// that place it at each offset.
		for offset := 0; offset <= height; offset++ {
			start := idx - offset
			if start >= 0 && start+height < len(contentLines) {
				starts[start] = struct{}{}
			}
		}
	}

	var out []Candidate
	for start := range starts {
		startLine := start + 1
		endLine := startLine + height
		found := strings.Join(contentLines[start:start+height+1], "\n")
		similarity := lineSimilarity(binding.SelectedText, found)
		if similarity <= 0 {
			continue
		}
		out = append(out, buildCandidate(request.Content, binding, startLine, endLine, similarity*0.85, "similar code"))
	}
	return out
}

// buildCandidate assembles a full binding for a proposed location, carrying the
// text actually found there so callers can show a preview.
func buildCandidate(content string, binding domain.Binding, startLine, endLine int, confidence float64, reason string) Candidate {
	moved := binding
	moved.StartLine = startLine
	moved.EndLine = endLine
	before, after := code.Context(content, startLine, endLine, 1)
	moved.BeforeContext = before
	moved.BeforeHash = code.HashText(before)
	moved.AfterContext = after
	moved.AfterHash = code.HashText(after)
	// A preview must describe what is at the proposed location now. Keeping the
	// anchor's own text when the slice failed showed the reviewer the code they
	// were trying to move away from, which is precisely backwards when the
	// question is "does this candidate look right?".
	found, err := code.Slice(content, startLine, moved.StartCol, endLine, moved.EndCol)
	if err != nil || strings.TrimSpace(found) == "" {
		found = wholeLines(content, startLine, endLine)
	}
	moved.SelectedText = found
	moved.SelectedTextHash = code.HashText(found)
	if confidence > 1 {
		confidence = 1
	}
	moved.Confidence = confidence
	return Candidate{Binding: moved, Confidence: confidence, Reason: reason}
}
