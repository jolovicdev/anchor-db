package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
	"github.com/jolovicdev/anchor-db/internal/repos"
	"github.com/jolovicdev/anchor-db/internal/resolver"
)

// candidatePreviewLimit caps how much code a suggestion carries, so a listing
// stays readable and an MCP response stays small.
const candidatePreviewLimit = 400

// RelocationCandidate is a proposed new home for a stale anchor.
type RelocationCandidate struct {
	Index      int     `json:"index"`
	StartLine  int     `json:"start_line"`
	StartCol   int     `json:"start_col"`
	EndLine    int     `json:"end_line"`
	EndCol     int     `json:"end_col"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	SymbolPath string  `json:"symbol_path,omitempty"`
	Preview    string  `json:"preview"`
}

// StaleEntry is one row of the triage queue: the anchor plus its best guess,
// so a reviewer can judge without opening each anchor individually.
type StaleEntry struct {
	Anchor        domain.Anchor        `json:"anchor"`
	StaleReason   string               `json:"stale_reason,omitempty"`
	BestCandidate *RelocationCandidate `json:"best_candidate,omitempty"`
}

// RelocateInput selects where an anchor should be re-pinned, either by
// accepting a ranked candidate or by naming an explicit span.
type RelocateInput struct {
	AnchorID  string
	Candidate *int
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// StaleQueue lists anchors needing attention, most recently affected first,
// each with its strongest relocation suggestion.
func (s *Service) StaleQueue(ctx context.Context, repoID string, limit int) ([]StaleEntry, error) {
	anchors, err := s.store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repoID,
		Status: domain.AnchorStatusStale,
	})
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(anchors) > limit {
		anchors = anchors[:limit]
	}

	entries := make([]StaleEntry, 0, len(anchors))
	for _, anchor := range anchors {
		entry := StaleEntry{Anchor: anchor, StaleReason: latestStaleReason(ctx, s, anchor.ID)}
		// A candidate needs the file read and parsed. If that fails the anchor
		// still belongs in the queue -- it just arrives without a suggestion.
		if candidates, err := s.candidatesFor(ctx, anchor, 1); err == nil && len(candidates) > 0 {
			entry.BestCandidate = &candidates[0]
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func latestStaleReason(ctx context.Context, s *Service, anchorID string) string {
	events, err := s.store.ListAnchorEvents(ctx, anchorID)
	if err != nil {
		return ""
	}
	reason := ""
	for _, event := range events {
		if event.Type == domain.AnchorEventStale {
			reason = event.Reason
		}
	}
	return reason
}

// RelocationCandidates ranks the places an anchor might now belong.
func (s *Service) RelocationCandidates(ctx context.Context, anchorID string) ([]RelocationCandidate, error) {
	anchor, err := s.store.GetAnchor(ctx, anchorID)
	if err != nil {
		return nil, err
	}
	return s.candidatesFor(ctx, anchor, 3)
}

func (s *Service) candidatesFor(ctx context.Context, anchor domain.Anchor, limit int) ([]RelocationCandidate, error) {
	repo, err := s.store.GetRepo(ctx, anchor.RepoID)
	if err != nil {
		return nil, err
	}
	// Suggestions have to describe the file as it is now. Reading the ref the
	// anchor was created against would offer places in a snapshot nobody is
	// editing, which is how a stale anchor came to be offered the range it
	// already had.
	content, err := s.repos.ReadFile(ctx, repo.RootPath, repos.RefWorktree, anchor.Binding.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", anchor.Binding.Path, err)
	}
	language := s.repos.LanguageForPath(anchor.Binding.Path)
	symbols, err := s.symbols.Extract(ctx, language, anchor.Binding.Path, content)
	if err != nil {
		return nil, err
	}

	ranked := s.resolver.Candidates(resolver.Request{
		Anchor:  anchor,
		Content: string(content),
		Symbols: symbols,
		Lines:   s.lineMap(ctx, repo.RootPath, repos.RefWorktree, anchor.Binding.Path, anchor.Binding.BaseCommit),
	}, limit)

	out := make([]RelocationCandidate, 0, len(ranked))
	for idx, candidate := range ranked {
		out = append(out, RelocationCandidate{
			Index:      idx,
			StartLine:  candidate.Binding.StartLine,
			StartCol:   candidate.Binding.StartCol,
			EndLine:    candidate.Binding.EndLine,
			EndCol:     candidate.Binding.EndCol,
			Confidence: candidate.Confidence,
			Reason:     candidate.Reason,
			SymbolPath: candidate.Binding.SymbolPath,
			Preview:    truncateRunes(candidate.Binding.SelectedText, candidatePreviewLimit),
		})
	}
	return out, nil
}

// AcceptRelocation re-pins an anchor and returns it to active. The new span is
// re-read from the file rather than trusted from the request, so the stored
// text and hashes always describe what is really there.
func (s *Service) AcceptRelocation(ctx context.Context, input RelocateInput) (domain.Anchor, error) {
	anchor, err := s.store.GetAnchor(ctx, input.AnchorID)
	if err != nil {
		return domain.Anchor{}, err
	}

	startLine, startCol := input.StartLine, input.StartCol
	endLine, endCol := input.EndLine, input.EndCol
	reason := "relocation accepted manually"

	if input.Candidate != nil {
		candidates, err := s.candidatesFor(ctx, anchor, 3)
		if err != nil {
			return domain.Anchor{}, err
		}
		index := *input.Candidate
		if index < 0 || index >= len(candidates) {
			return domain.Anchor{}, fmt.Errorf("candidate %d out of range (%d available)", index, len(candidates))
		}
		chosen := candidates[index]
		startLine, startCol = chosen.StartLine, chosen.StartCol
		endLine, endCol = chosen.EndLine, chosen.EndCol
		reason = "relocation accepted: " + chosen.Reason
	}

	if startLine < 1 || endLine < 1 || startCol < 1 || endCol < 1 {
		return domain.Anchor{}, errors.New("line and column values must be positive")
	}
	if endLine < startLine || (endLine == startLine && endCol < startCol) {
		return domain.Anchor{}, errors.New("end position must not precede start position")
	}

	repo, err := s.store.GetRepo(ctx, anchor.RepoID)
	if err != nil {
		return domain.Anchor{}, err
	}
	// The line numbers being accepted describe the file on disk, so the span has
	// to be read from there. Reading them out of the creation ref bound the
	// anchor to whatever occupied those lines back then instead.
	content, err := s.repos.ReadFile(ctx, repo.RootPath, repos.RefWorktree, anchor.Binding.Path)
	if err != nil {
		return domain.Anchor{}, err
	}
	selected, err := code.Slice(string(content), startLine, startCol, endLine, endCol)
	if err != nil {
		return domain.Anchor{}, err
	}
	if selected == "" {
		return domain.Anchor{}, errors.New("selected range is empty: end position must be after start position")
	}

	before, after := code.Context(string(content), startLine, endLine, 1)
	language := s.repos.LanguageForPath(anchor.Binding.Path)
	symbols, err := s.symbols.Extract(ctx, language, anchor.Binding.Path, content)
	if err != nil {
		return domain.Anchor{}, err
	}

	binding := anchor.Binding
	binding.StartLine, binding.StartCol = startLine, startCol
	binding.EndLine, binding.EndCol = endLine, endCol
	binding.SelectedText = selected
	binding.SelectedTextHash = code.HashText(selected)
	binding.BeforeContext = before
	binding.BeforeHash = code.HashText(before)
	binding.AfterContext = after
	binding.AfterHash = code.HashText(after)
	binding.Language = language
	// Re-derive the symbol: accepting a relocation may well have moved the
	// anchor into a different function than the one it started in.
	binding.SymbolPath = findSymbol(symbols, startLine, endLine)
	binding.Type = domain.BindingTypeSpan
	if binding.SymbolPath != "" {
		binding.Type = domain.BindingTypeSymbol
	}
	// Re-base onto the current commit so the next automatic pass can follow
	// this span through git instead of starting from stale coordinates.
	if head, err := s.repos.ResolveCommit(ctx, repo.RootPath, ""); err == nil {
		binding.BaseCommit = head
	}

	return s.store.ApplyResolution(ctx, anchor.ID, binding, domain.AnchorStatusActive, reason, 1)
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimRight(value, "\n")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
