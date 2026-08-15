package resolver

import (
	"strings"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
	"github.com/jolovicdev/anchor-db/internal/gitmap"
)

type Resolution struct {
	Binding    domain.Binding
	Status     domain.AnchorStatus
	Reason     string
	Confidence float64
}

type Service struct{}

func New() *Service {
	return &Service{}
}

// Request is everything resolution needs about one anchor at one moment.
type Request struct {
	Anchor  domain.Anchor
	Content string
	Symbols []domain.Symbol
	// Lines maps the anchor's recorded line numbers onto the current content
	// using git history. Nil when no diff base is available, in which case
	// resolution falls back to matching on text alone.
	Lines *gitmap.LineMap
}

// Resolve locates an anchor in the current content, trying strategies from
// most to least certain and stopping at the first that holds up.
func (s *Service) Resolve(request Request) (Resolution, error) {
	binding := request.Anchor.Binding

	// An anchor with no selected text cannot be relocated. Report it as stale
	// rather than failing: Resolve runs over every anchor in a path, and one
	// unusable record must not abort resolution for its neighbours.
	if binding.SelectedText == "" {
		return Stale(binding, "anchor has no selected text"), nil
	}
	if sameSpan(request.Content, binding) {
		moved := binding
		moved.Confidence = 1
		return Resolution{Binding: moved, Status: domain.AnchorStatusActive, Reason: "exact span match", Confidence: 1}, nil
	}
	// Git knows where the lines went; text matching can only guess, and guesses
	// wrong when the same code appears twice. Prefer the deterministic answer.
	if moved, ok := gitMatch(request.Content, binding, request.Lines); ok {
		return Resolution{Binding: moved, Status: domain.AnchorStatusActive, Reason: "git line mapping", Confidence: moved.Confidence}, nil
	}
	if moved, ok := symbolMatch(request.Content, binding, request.Symbols); ok {
		return Resolution{Binding: moved, Status: domain.AnchorStatusActive, Reason: "symbol match", Confidence: moved.Confidence}, nil
	}
	if moved, ok := textMatch(request.Content, binding); ok {
		return Resolution{Binding: moved, Status: domain.AnchorStatusActive, Reason: "text/context match", Confidence: moved.Confidence}, nil
	}
	return Stale(binding, staleReason(binding, request.Lines)), nil
}

// staleReason uses the diff to say *why* an anchor could not be placed, which
// separates "the code here was rewritten" from "these lines are gone".
func staleReason(binding domain.Binding, lines *gitmap.LineMap) string {
	if lines == nil {
		return "no match"
	}
	if _, _, ok := lines.MapRange(binding.StartLine, binding.EndLine); ok {
		return "code at the mapped location was rewritten"
	}
	return "the anchored lines were deleted or restructured"
}

// gitMatch translates the span through the diff and then verifies it: the
// mapping is only trusted when the text that landed there is the text the
// anchor recorded. That verification is what makes it safe to apply the
// mapping even when the anchor was written against a dirty working tree.
func gitMatch(content string, binding domain.Binding, lines *gitmap.LineMap) (domain.Binding, bool) {
	if lines == nil || lines.Unchanged() {
		return domain.Binding{}, false
	}
	startLine, endLine, ok := lines.MapRange(binding.StartLine, binding.EndLine)
	if !ok {
		return domain.Binding{}, false
	}
	selected, err := code.Slice(content, startLine, binding.StartCol, endLine, binding.EndCol)
	if err != nil || selected != binding.SelectedText {
		return domain.Binding{}, false
	}
	before, after := code.Context(content, startLine, endLine, 1)
	moved := binding
	moved.StartLine = startLine
	moved.EndLine = endLine
	moved.BeforeContext = before
	moved.BeforeHash = code.HashText(before)
	moved.AfterContext = after
	moved.AfterHash = code.HashText(after)
	moved.Confidence = 0.99
	return moved, true
}

// Stale marks a binding as unresolvable while preserving its recorded position,
// so the anchor keeps pointing at its last known location for a human to fix.
func Stale(binding domain.Binding, reason string) Resolution {
	binding.Confidence = 0
	return Resolution{Binding: binding, Status: domain.AnchorStatusStale, Reason: reason, Confidence: 0}
}

func sameSpan(content string, binding domain.Binding) bool {
	selected, err := code.Slice(content, binding.StartLine, binding.StartCol, binding.EndLine, binding.EndCol)
	if err != nil {
		return false
	}
	return selected == binding.SelectedText
}

func symbolMatch(content string, binding domain.Binding, symbols []domain.Symbol) (domain.Binding, bool) {
	if binding.SymbolPath == "" {
		return domain.Binding{}, false
	}
	for _, symbol := range symbols {
		if symbol.SymbolPath != binding.SymbolPath {
			continue
		}
		selected, err := code.Slice(content, symbol.StartLine, symbol.StartCol, symbol.EndLine, symbol.EndCol)
		if err != nil {
			continue
		}
		before, after := code.Context(content, symbol.StartLine, symbol.EndLine, 1)
		confidence, ok := symbolConfidence(binding, selected, before, after)
		if !ok {
			continue
		}
		return domain.Binding{
			Type:             domain.BindingTypeSymbol,
			Ref:              binding.Ref,
			Path:             binding.Path,
			Language:         binding.Language,
			SymbolPath:       symbol.SymbolPath,
			StartLine:        symbol.StartLine,
			StartCol:         symbol.StartCol,
			EndLine:          symbol.EndLine,
			EndCol:           symbol.EndCol,
			SelectedText:     selected,
			SelectedTextHash: code.HashText(selected),
			BeforeContext:    before,
			BeforeHash:       code.HashText(before),
			AfterContext:     after,
			AfterHash:        code.HashText(after),
			Confidence:       confidence,
		}, true
	}
	return domain.Binding{}, false
}

func textMatch(content string, binding domain.Binding) (domain.Binding, bool) {
	candidates := occurrences(content, binding.SelectedText)
	if len(candidates) == 0 {
		return domain.Binding{}, false
	}
	bestStart := -1
	bestScore := -1
	for _, start := range candidates {
		end := start + len(binding.SelectedText)
		before, after := surrounding(content, start, end)
		score := 0
		if binding.BeforeContext != "" && strings.Contains(before, binding.BeforeContext) {
			score += 2
		}
		if binding.AfterContext != "" && strings.Contains(after, binding.AfterContext) {
			score += 2
		}
		if score > bestScore {
			bestStart = start
			bestScore = score
		}
	}
	if bestStart < 0 {
		return domain.Binding{}, false
	}
	startLine, startCol, endLine, endCol := code.RangeFromOffsets(content, bestStart, bestStart+len(binding.SelectedText))
	before, after := code.Context(content, startLine, endLine, 1)
	return domain.Binding{
		Type:             binding.Type,
		Ref:              binding.Ref,
		Path:             binding.Path,
		Language:         binding.Language,
		SymbolPath:       binding.SymbolPath,
		StartLine:        startLine,
		StartCol:         startCol,
		EndLine:          endLine,
		EndCol:           endCol,
		SelectedText:     binding.SelectedText,
		SelectedTextHash: binding.SelectedTextHash,
		BeforeContext:    before,
		BeforeHash:       code.HashText(before),
		AfterContext:     after,
		AfterHash:        code.HashText(after),
		Confidence:       0.9,
	}, true
}

func occurrences(content, needle string) []int {
	if needle == "" {
		return nil
	}
	var indexes []int
	offset := 0
	for {
		idx := strings.Index(content[offset:], needle)
		if idx < 0 {
			return indexes
		}
		absolute := offset + idx
		indexes = append(indexes, absolute)
		offset = absolute + 1
	}
}

func surrounding(content string, start, end int) (string, string) {
	left := code.MaxInt(0, start-120)
	right := code.MinInt(len(content), end+120)
	return content[left:start], content[end:right]
}

func symbolConfidence(binding domain.Binding, selected, before, after string) (float64, bool) {
	if selected == binding.SelectedText {
		return 0.97, true
	}
	if similarity := lineSimilarity(binding.SelectedText, selected); similarity >= 0.6 {
		return 0.95, true
	}
	beforeMatch := binding.BeforeContext != "" && strings.Contains(before, binding.BeforeContext)
	afterMatch := binding.AfterContext != "" && strings.Contains(after, binding.AfterContext)
	if signatureLine(selected) != "" &&
		signatureLine(selected) == signatureLine(binding.SelectedText) &&
		(beforeMatch || afterMatch || lineSimilarity(binding.SelectedText, selected) >= 0.4) {
		return 0.93, true
	}
	return 0, false
}

func signatureLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func lineSimilarity(left, right string) float64 {
	leftLines := nonEmptyLines(left)
	rightLines := nonEmptyLines(right)
	if len(leftLines) == 0 || len(rightLines) == 0 {
		return 0
	}
	denominator := len(leftLines)
	if len(rightLines) > denominator {
		denominator = len(rightLines)
	}
	distance := lineEditDistance(leftLines, rightLines)
	return 1 - float64(distance)/float64(denominator)
}

func nonEmptyLines(content string) []string {
	lines := strings.Split(content, "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}

func lineEditDistance(left, right []string) int {
	if len(left) == 0 {
		return len(right)
	}
	if len(right) == 0 {
		return len(left)
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 0
			if left[i-1] != right[j-1] {
				cost = 1
			}
			current[j] = minInt(
				previous[j]+1,
				current[j-1]+1,
				previous[j-1]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}
