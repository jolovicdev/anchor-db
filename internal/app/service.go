package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
	"github.com/jolovicdev/anchor-db/internal/gitmap"
	"github.com/jolovicdev/anchor-db/internal/repos"
	"github.com/jolovicdev/anchor-db/internal/resolver"
	"github.com/jolovicdev/anchor-db/internal/symbols"
)

type Store interface {
	CreateRepo(context.Context, domain.Repo) (domain.Repo, error)
	ListRepos(context.Context) ([]domain.Repo, error)
	GetRepo(context.Context, string) (domain.Repo, error)
	UpdateRepo(context.Context, domain.Repo) (domain.Repo, error)
	DeleteRepo(context.Context, string) error
	CreateAnchor(context.Context, domain.Anchor) (domain.Anchor, error)
	UpdateAnchor(context.Context, domain.Anchor, string) (domain.Anchor, error)
	GetAnchor(context.Context, string) (domain.Anchor, error)
	ListAnchors(context.Context, domain.AnchorFilter) ([]domain.Anchor, error)
	ApplyResolution(context.Context, string, domain.Binding, domain.AnchorStatus, string, float64) (domain.Anchor, error)
	ListAnchorEvents(context.Context, string) ([]domain.AnchorEvent, error)
	CreateComment(context.Context, domain.Comment) (domain.Comment, error)
	ListComments(context.Context, string) ([]domain.Comment, error)
	Search(context.Context, domain.SearchQuery) ([]domain.SearchHit, error)
}

type Service struct {
	store    Store
	repos    *repos.Service
	symbols  *symbols.Service
	resolver *resolver.Service
}

type CreateAnchorInput struct {
	RepoID    string
	Ref       string
	Path      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Kind      string
	Title     string
	Body      string
	Author    string
	Tags      []string
	Symbol    string
}

type UpdateAnchorInput struct {
	ID          string
	Kind        string
	Title       string
	Body        string
	Author      string
	Tags        []string
	ReplaceTags bool
}

type ContextRequest struct {
	RepoID string
	Ref    string
	Path   string
	Symbol string
}

type ContextResponse struct {
	Repo    domain.Repo     `json:"repo"`
	Anchors []domain.Anchor `json:"anchors"`
}

type FileView struct {
	Repo     domain.Repo
	Ref      string
	Path     string
	Content  string
	Lines    []FileLine
	Diff     string
	Files    []string
	Anchors  []domain.Anchor
	Comments map[string][]domain.Comment
	History  map[string][]AnchorHistoryEntry
	// Candidates holds relocation suggestions, populated only for stale
	// anchors: computing them costs a parse of the file, and an anchor that
	// resolved cleanly has nothing to triage.
	Candidates map[string][]RelocationCandidate
}

// AnchorHistoryEntry is one step of an anchor's life, flattened for display:
// what happened, why, and where it landed.
type AnchorHistoryEntry struct {
	Type       domain.AnchorEventType `json:"type"`
	Reason     string                 `json:"reason"`
	Confidence float64                `json:"confidence"`
	FromRange  string                 `json:"from_range,omitempty"`
	ToRange    string                 `json:"to_range,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

type FileLine struct {
	Number      int
	Text        string
	Highlighted bool
	// Stale marks a line covered by an anchor that lost its place, so the
	// gutter can distinguish "a note lives here" from "a note is lost here".
	Stale bool
	// Starts lists anchors beginning on this line, giving the viewer a scroll
	// target for each anchor card.
	Starts []string
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	return &Service{
		store:    store,
		repos:    repos.NewService(),
		symbols:  symbols.NewService(),
		resolver: resolver.New(),
	}, nil
}

func (s *Service) RegisterRepo(ctx context.Context, name, root string) (domain.Repo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return domain.Repo{}, err
	}
	head, err := s.repos.Head(ctx, absRoot)
	if err != nil {
		return domain.Repo{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(absRoot)
	}
	return s.store.CreateRepo(ctx, domain.Repo{
		ID:         domain.NewID("repo"),
		Name:       name,
		RootPath:   absRoot,
		DefaultRef: head,
	})
}

func (s *Service) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	return s.store.ListRepos(ctx)
}

func (s *Service) GetRepo(ctx context.Context, id string) (domain.Repo, error) {
	return s.store.GetRepo(ctx, id)
}

func (s *Service) SyncRepo(ctx context.Context, id string) (domain.Repo, error) {
	repo, err := s.store.GetRepo(ctx, id)
	if err != nil {
		return domain.Repo{}, err
	}
	head, err := s.repos.Head(ctx, repo.RootPath)
	if err != nil {
		return domain.Repo{}, err
	}
	repo.DefaultRef = head
	updatedRepo, err := s.store.UpdateRepo(ctx, repo)
	if err != nil {
		return domain.Repo{}, err
	}
	if err := s.resolveRepoPaths(ctx, repo.ID); err != nil {
		return domain.Repo{}, err
	}
	return updatedRepo, nil
}

// resolveRepoPaths re-resolves every distinct (ref, path) an anchor points at.
// Failures are collected rather than returned immediately so that one broken
// path cannot stop the remaining paths in the repo from being resolved.
func (s *Service) resolveRepoPaths(ctx context.Context, repoID string) error {
	anchors, err := s.listResolvableAnchors(ctx, repoID, "")
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	var errs []error
	for _, anchor := range anchors {
		key := anchor.Binding.Ref + "::" + anchor.Binding.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := s.ResolvePath(ctx, repoID, anchor.Binding.Ref, anchor.Binding.Path); err != nil {
			errs = append(errs, fmt.Errorf("resolve %s: %w", anchor.Binding.Path, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) RemoveRepo(ctx context.Context, id string) error {
	return s.store.DeleteRepo(ctx, id)
}

func (s *Service) CreateAnchor(ctx context.Context, input CreateAnchorInput) (domain.Anchor, error) {
	// Validation lives here rather than in the HTTP handler because MCP and the
	// CLI reach this method directly; the HTTP layer is not the only entrypoint.
	if err := validateCreateInput(input); err != nil {
		return domain.Anchor{}, err
	}
	repo, err := s.store.GetRepo(ctx, input.RepoID)
	if err != nil {
		return domain.Anchor{}, err
	}
	ref := input.Ref
	if ref == "" {
		ref = repos.RefWorktree
	}
	content, err := s.repos.ReadFile(ctx, repo.RootPath, ref, input.Path)
	if err != nil {
		return domain.Anchor{}, err
	}
	selected, err := code.Slice(string(content), input.StartLine, input.StartCol, input.EndLine, input.EndCol)
	if err != nil {
		return domain.Anchor{}, err
	}
	// A zero-width span produces an anchor that can never be relocated, which
	// used to leave a permanently unresolvable record behind.
	if selected == "" {
		return domain.Anchor{}, errors.New("selected range is empty: end position must be after start position")
	}
	before, after := code.Context(string(content), input.StartLine, input.EndLine, 1)
	language := s.repos.LanguageForPath(input.Path)
	symbols, err := s.symbols.Extract(ctx, language, input.Path, content)
	if err != nil {
		return domain.Anchor{}, err
	}
	symbolPath := input.Symbol
	if symbolPath == "" {
		symbolPath = findSymbol(symbols, input.StartLine, input.EndLine)
	}
	bindingType := domain.BindingTypeSpan
	if symbolPath != "" {
		bindingType = domain.BindingTypeSymbol
	}
	// Record the commit these line numbers were taken against so later
	// resolution can relocate the span from git history. A repo with no commits
	// yet simply leaves this empty and falls back to text matching.
	baseCommit, err := s.repos.ResolveCommit(ctx, repo.RootPath, "")
	if err != nil {
		baseCommit = ""
	}
	anchor := domain.Anchor{
		ID:        domain.NewID("anchor"),
		RepoID:    repo.ID,
		Kind:      normalizeKind(input.Kind),
		Status:    domain.AnchorStatusActive,
		Title:     input.Title,
		Body:      input.Body,
		Author:    input.Author,
		SourceRef: ref,
		Tags:      input.Tags,
		Binding: domain.Binding{
			Type:             bindingType,
			Ref:              ref,
			Path:             input.Path,
			Language:         language,
			SymbolPath:       symbolPath,
			StartLine:        input.StartLine,
			StartCol:         input.StartCol,
			EndLine:          input.EndLine,
			EndCol:           input.EndCol,
			SelectedText:     selected,
			SelectedTextHash: code.HashText(selected),
			BeforeContext:    before,
			BeforeHash:       code.HashText(before),
			AfterContext:     after,
			AfterHash:        code.HashText(after),
			BaseCommit:       baseCommit,
			Confidence:       1,
		},
	}
	return s.store.CreateAnchor(ctx, anchor)
}

func (s *Service) ListAnchors(ctx context.Context, filter domain.AnchorFilter) ([]domain.Anchor, error) {
	return s.store.ListAnchors(ctx, filter)
}

func (s *Service) GetAnchor(ctx context.Context, id string) (domain.Anchor, error) {
	return s.store.GetAnchor(ctx, id)
}

func (s *Service) UpdateAnchor(ctx context.Context, input UpdateAnchorInput) (domain.Anchor, error) {
	anchor, err := s.store.GetAnchor(ctx, input.ID)
	if err != nil {
		return domain.Anchor{}, err
	}
	if strings.TrimSpace(input.Kind) != "" {
		anchor.Kind = normalizeKind(input.Kind)
	}
	if strings.TrimSpace(input.Title) != "" {
		anchor.Title = strings.TrimSpace(input.Title)
	}
	if strings.TrimSpace(input.Body) != "" {
		anchor.Body = strings.TrimSpace(input.Body)
	}
	if strings.TrimSpace(input.Author) != "" {
		anchor.Author = strings.TrimSpace(input.Author)
	}
	if input.ReplaceTags {
		anchor.Tags = input.Tags
	}
	if strings.TrimSpace(anchor.Title) == "" {
		return domain.Anchor{}, errors.New("title is required")
	}
	if strings.TrimSpace(anchor.Body) == "" {
		return domain.Anchor{}, errors.New("body is required")
	}
	if strings.TrimSpace(anchor.Author) == "" {
		return domain.Anchor{}, errors.New("author is required")
	}
	return s.store.UpdateAnchor(ctx, anchor, "anchor updated")
}

// ListAnchorEvents returns an anchor's move/stale/update history. The store has
// always recorded these; this exposes them to the surfaces above it.
func (s *Service) ListAnchorEvents(ctx context.Context, anchorID string) ([]domain.AnchorEvent, error) {
	if _, err := s.store.GetAnchor(ctx, anchorID); err != nil {
		return nil, err
	}
	return s.store.ListAnchorEvents(ctx, anchorID)
}

func (s *Service) CloseAnchor(ctx context.Context, id string) (domain.Anchor, error) {
	anchor, err := s.store.GetAnchor(ctx, id)
	if err != nil {
		return domain.Anchor{}, err
	}
	anchor.Status = domain.AnchorStatusArchived
	return s.store.UpdateAnchor(ctx, anchor, "anchor closed")
}

func (s *Service) ReopenAnchor(ctx context.Context, id string) (domain.Anchor, error) {
	anchor, err := s.store.GetAnchor(ctx, id)
	if err != nil {
		return domain.Anchor{}, err
	}
	anchor.Status = domain.AnchorStatusActive
	return s.store.UpdateAnchor(ctx, anchor, "anchor reopened")
}

func (s *Service) ResolveAnchor(ctx context.Context, id string) (domain.Anchor, error) {
	anchor, err := s.store.GetAnchor(ctx, id)
	if err != nil {
		return domain.Anchor{}, err
	}
	if anchor.Status != domain.AnchorStatusActive && anchor.Status != domain.AnchorStatusStale {
		return domain.Anchor{}, errors.New("only active or stale anchors can be resolved")
	}
	updated, err := s.ResolvePath(ctx, anchor.RepoID, anchor.Binding.Ref, anchor.Binding.Path)
	if err != nil {
		return domain.Anchor{}, err
	}
	for _, item := range updated {
		if item.ID == id {
			return item, nil
		}
	}
	return s.store.GetAnchor(ctx, id)
}

func (s *Service) CreateComment(ctx context.Context, anchorID, parentID, author, body string) (domain.Comment, error) {
	if _, err := s.store.GetAnchor(ctx, anchorID); err != nil {
		return domain.Comment{}, err
	}
	return s.store.CreateComment(ctx, domain.Comment{
		ID:       domain.NewID("comment"),
		AnchorID: anchorID,
		ParentID: parentID,
		Author:   author,
		Body:     body,
	})
}

func (s *Service) ListComments(ctx context.Context, anchorID string) ([]domain.Comment, error) {
	return s.store.ListComments(ctx, anchorID)
}

func (s *Service) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchHit, error) {
	return s.store.Search(ctx, query)
}

func (s *Service) Context(ctx context.Context, request ContextRequest) (ContextResponse, error) {
	repo, err := s.store.GetRepo(ctx, request.RepoID)
	if err != nil {
		return ContextResponse{}, err
	}
	anchors, err := s.store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID:     request.RepoID,
		Path:       request.Path,
		SymbolPath: request.Symbol,
		Status:     domain.AnchorStatusActive,
	})
	if err != nil {
		return ContextResponse{}, err
	}
	return ContextResponse{Repo: repo, Anchors: anchors}, nil
}

func (s *Service) ResolvePath(ctx context.Context, repoID, ref, path string) ([]domain.Anchor, error) {
	repo, err := s.store.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if ref == "" {
		ref = repos.RefWorktree
	}
	anchors, err := s.listResolvableAnchors(ctx, repoID, path)
	if err != nil {
		return nil, err
	}

	// A deleted or unreadable file is an expected outcome of normal editing, not
	// a failure: its anchors go stale so the rest of the repo still resolves.
	content, readErr := s.repos.ReadFile(ctx, repo.RootPath, ref, path)
	if readErr != nil {
		// Unless the whole repo is missing -- an unmounted drive or a moved
		// checkout says nothing about individual anchors, and marking every one
		// of them stale would discard real state over a transient condition.
		if info, err := os.Stat(repo.RootPath); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("repo root %s unavailable: %w", repo.RootPath, readErr)
		}
		return s.applyResolutions(ctx, anchors, func(anchor domain.Anchor) (resolver.Resolution, error) {
			return resolver.Stale(anchor.Binding, "file unreadable: "+readErr.Error()), nil
		})
	}

	language := s.repos.LanguageForPath(path)
	symbols, err := s.symbols.Extract(ctx, language, path, content)
	if err != nil {
		return nil, err
	}
	// The commit anchors should be re-based onto once they resolve cleanly, so
	// the next pass diffs from the most recent known-good point.
	head, headErr := s.repos.ResolveCommit(ctx, repo.RootPath, "")
	return s.applyResolutions(ctx, anchors, func(anchor domain.Anchor) (resolver.Resolution, error) {
		resolution, err := s.resolver.Resolve(resolver.Request{
			Anchor:  anchor,
			Content: string(content),
			Symbols: symbols,
			Lines:   s.lineMap(ctx, repo.RootPath, ref, path, anchor.Binding.BaseCommit),
		})
		if err != nil {
			return resolution, err
		}
		if resolution.Status == domain.AnchorStatusActive && headErr == nil {
			resolution.Binding.BaseCommit = head
		}
		return resolution, nil
	})
}

// lineMap builds the old->new line mapping for one anchor from its recorded
// base commit. Any failure degrades to nil, which resolution treats as "no git
// information available" and falls back to text matching.
func (s *Service) lineMap(ctx context.Context, root, ref, path, baseCommit string) *gitmap.LineMap {
	if baseCommit == "" {
		return nil
	}
	diff, err := s.repos.DiffHunks(ctx, root, baseCommit, ref, path)
	if err != nil {
		return nil
	}
	return gitmap.Parse(diff)
}

func (s *Service) applyResolutions(ctx context.Context, anchors []domain.Anchor, resolve func(domain.Anchor) (resolver.Resolution, error)) ([]domain.Anchor, error) {
	var updated []domain.Anchor
	for _, anchor := range anchors {
		resolution, err := resolve(anchor)
		if err != nil {
			return nil, err
		}
		next, err := s.store.ApplyResolution(ctx, anchor.ID, resolution.Binding, resolution.Status, resolution.Reason, resolution.Confidence)
		if err != nil {
			return nil, err
		}
		updated = append(updated, next)
	}
	return updated, nil
}

func (s *Service) FileView(ctx context.Context, repoID, ref, path string) (FileView, error) {
	repo, err := s.store.GetRepo(ctx, repoID)
	if err != nil {
		return FileView{}, err
	}
	if ref == "" {
		ref = repos.RefWorktree
	}
	content, err := s.repos.ReadFile(ctx, repo.RootPath, ref, path)
	if err != nil {
		return FileView{}, err
	}
	diff, err := s.repos.DiffFile(ctx, repo.RootPath, ref, path)
	if err != nil {
		return FileView{}, err
	}
	files, err := s.repos.ListFiles(ctx, repo.RootPath, ref)
	if err != nil {
		return FileView{}, err
	}
	anchors, err := s.store.ListAnchors(ctx, domain.AnchorFilter{RepoID: repoID, Path: path})
	if err != nil {
		return FileView{}, err
	}
	comments := make(map[string][]domain.Comment, len(anchors))
	history := make(map[string][]AnchorHistoryEntry, len(anchors))
	candidates := map[string][]RelocationCandidate{}
	for _, anchor := range anchors {
		items, err := s.store.ListComments(ctx, anchor.ID)
		if err != nil {
			return FileView{}, err
		}
		comments[anchor.ID] = items

		events, err := s.store.ListAnchorEvents(ctx, anchor.ID)
		if err != nil {
			return FileView{}, err
		}
		history[anchor.ID] = buildHistory(events)

		if anchor.Status == domain.AnchorStatusStale {
			// Suggestions are best-effort: a file that will not parse should
			// still render, just without a triage panel.
			if suggestions, err := s.candidatesFor(ctx, anchor, 3); err == nil && len(suggestions) > 0 {
				candidates[anchor.ID] = suggestions
			}
		}
	}
	lines := buildFileLines(string(content), anchors)
	return FileView{
		Repo:       repo,
		Ref:        ref,
		Path:       path,
		Content:    string(content),
		Lines:      lines,
		Diff:       diff,
		Files:      files,
		Anchors:    anchors,
		Comments:   comments,
		History:    history,
		Candidates: candidates,
	}, nil
}

func buildHistory(events []domain.AnchorEvent) []AnchorHistoryEntry {
	entries := make([]AnchorHistoryEntry, 0, len(events))
	for _, event := range events {
		entry := AnchorHistoryEntry{
			Type:       event.Type,
			Reason:     event.Reason,
			Confidence: event.Confidence,
			CreatedAt:  event.CreatedAt,
		}
		if event.FromBinding != nil {
			entry.FromRange = formatRange(*event.FromBinding)
		}
		if event.ToBinding != nil {
			entry.ToRange = formatRange(*event.ToBinding)
		}
		entries = append(entries, entry)
	}
	return entries
}

func formatRange(binding domain.Binding) string {
	return fmt.Sprintf("%s:%d-%d", binding.Path, binding.StartLine, binding.EndLine)
}

func (s *Service) ResolveAll(ctx context.Context) error {
	all, err := s.store.ListRepos(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, repo := range all {
		if err := s.resolveRepoPaths(ctx, repo.ID); err != nil {
			errs = append(errs, fmt.Errorf("repo %s: %w", repo.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) listResolvableAnchors(ctx context.Context, repoID, path string) ([]domain.Anchor, error) {
	active, err := s.store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repoID,
		Path:   path,
		Status: domain.AnchorStatusActive,
	})
	if err != nil {
		return nil, err
	}
	stale, err := s.store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repoID,
		Path:   path,
		Status: domain.AnchorStatusStale,
	})
	if err != nil {
		return nil, err
	}
	return append(active, stale...), nil
}

func findSymbol(symbols []domain.Symbol, startLine, endLine int) string {
	best := ""
	bestSize := 0
	for _, symbol := range symbols {
		if startLine < symbol.StartLine || endLine > symbol.EndLine {
			continue
		}
		size := (symbol.EndLine - symbol.StartLine) + 1
		if best == "" || size < bestSize {
			best = symbol.SymbolPath
			bestSize = size
		}
	}
	return best
}

func validateCreateInput(input CreateAnchorInput) error {
	if strings.TrimSpace(input.RepoID) == "" {
		return errors.New("repo_id is required")
	}
	if strings.TrimSpace(input.Path) == "" {
		return errors.New("path is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(input.Body) == "" {
		return errors.New("body is required")
	}
	if strings.TrimSpace(input.Author) == "" {
		return errors.New("author is required")
	}
	if input.StartLine < 1 || input.StartCol < 1 || input.EndLine < 1 || input.EndCol < 1 {
		return errors.New("line and column values must be positive")
	}
	if input.EndLine < input.StartLine {
		return errors.New("end_line must be >= start_line")
	}
	if input.EndLine == input.StartLine && input.EndCol < input.StartCol {
		return errors.New("end_col must be >= start_col when on the same line")
	}
	return nil
}

func normalizeKind(value string) domain.AnchorKind {
	switch domain.AnchorKind(strings.ToLower(strings.TrimSpace(value))) {
	case domain.AnchorKindTodo:
		return domain.AnchorKindTodo
	case domain.AnchorKindHandoff:
		return domain.AnchorKindHandoff
	case domain.AnchorKindRationale:
		return domain.AnchorKindRationale
	case domain.AnchorKindInvariant:
		return domain.AnchorKindInvariant
	case domain.AnchorKindQuestion:
		return domain.AnchorKindQuestion
	default:
		return domain.AnchorKindWarning
	}
}

func buildFileLines(content string, anchors []domain.Anchor) []FileLine {
	covered := map[int]bool{}
	stale := map[int]bool{}
	starts := map[int][]string{}
	for _, anchor := range anchors {
		start := anchor.Binding.StartLine
		end := anchor.Binding.EndLine
		if start <= 0 {
			start = 1
		}
		if end < start {
			end = start
		}
		starts[start] = append(starts[start], anchor.ID)
		for line := start; line <= end; line++ {
			covered[line] = true
			if anchor.Status == domain.AnchorStatusStale {
				stale[line] = true
			}
		}
	}
	sourceLines := strings.Split(content, "\n")
	lines := make([]FileLine, 0, len(sourceLines))
	for idx, line := range sourceLines {
		number := idx + 1
		lines = append(lines, FileLine{
			Number:      number,
			Text:        line,
			Highlighted: covered[number],
			Stale:       stale[number],
			Starts:      starts[number],
		})
	}
	return lines
}
