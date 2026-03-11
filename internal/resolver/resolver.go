package resolver

import (
	"errors"
	"strings"

	"anchordb/internal/code"
	"anchordb/internal/domain"
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

func (s *Service) Resolve(anchor domain.Anchor, content string, symbols []domain.Symbol) (Resolution, error) {
	if anchor.Binding.SelectedText == "" {
		return Resolution{}, errors.New("anchor binding selected text is required")
	}
	if sameSpan(content, anchor.Binding) {
		binding := anchor.Binding
		binding.Confidence = 1
		return Resolution{Binding: binding, Status: domain.AnchorStatusActive, Reason: "exact span match", Confidence: 1}, nil
	}
	if binding, ok := symbolMatch(content, anchor.Binding, symbols); ok {
		return Resolution{Binding: binding, Status: domain.AnchorStatusActive, Reason: "symbol match", Confidence: binding.Confidence}, nil
	}
	if binding, ok := textMatch(content, anchor.Binding); ok {
		return Resolution{Binding: binding, Status: domain.AnchorStatusActive, Reason: "text/context match", Confidence: binding.Confidence}, nil
	}
	binding := anchor.Binding
	binding.Confidence = 0
	return Resolution{Binding: binding, Status: domain.AnchorStatusStale, Reason: "no match", Confidence: 0}, nil
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
			Confidence:       0.97,
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
