package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jolovicdev/anchor-db/internal/code"
	"github.com/jolovicdev/anchor-db/internal/domain"
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
}

type FileLine struct {
	Number      int
	Text        string
	Highlighted bool
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
	anchors, err := s.listResolvableAnchors(ctx, repo.ID, "")
	if err != nil {
		return domain.Repo{}, err
	}
	seen := map[string]struct{}{}
	for _, anchor := range anchors {
		key := anchor.Binding.Ref + "::" + anchor.Binding.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := s.ResolvePath(ctx, repo.ID, anchor.Binding.Ref, anchor.Binding.Path); err != nil {
			return domain.Repo{}, err
		}
	}
	return updatedRepo, nil
}

func (s *Service) RemoveRepo(ctx context.Context, id string) error {
	return s.store.DeleteRepo(ctx, id)
}

func (s *Service) CreateAnchor(ctx context.Context, input CreateAnchorInput) (domain.Anchor, error) {
	repo, err := s.store.GetRepo(ctx, input.RepoID)
	if err != nil {
		return domain.Anchor{}, err
	}
	ref := input.Ref
	if ref == "" {
		ref = "WORKTREE"
	}
	content, err := s.repos.ReadFile(ctx, repo.RootPath, ref, input.Path)
	if err != nil {
		return domain.Anchor{}, err
	}
	selected, err := code.Slice(string(content), input.StartLine, input.StartCol, input.EndLine, input.EndCol)
	if err != nil {
		return domain.Anchor{}, err
	}
	before, after := code.Context(string(content), input.StartLine, input.EndLine, 1)
	language := s.repos.LanguageForPath(input.Path)
	symbols, err := s.symbols.Extract(language, input.Path, content)
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
		ref = "WORKTREE"
	}
	content, err := s.repos.ReadFile(ctx, repo.RootPath, ref, path)
	if err != nil {
		return nil, err
	}
	anchors, err := s.listResolvableAnchors(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	language := s.repos.LanguageForPath(path)
	symbols, err := s.symbols.Extract(language, path, content)
	if err != nil {
		return nil, err
	}
	var updated []domain.Anchor
	for _, anchor := range anchors {
		resolution, err := s.resolver.Resolve(anchor, string(content), symbols)
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
		ref = "WORKTREE"
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
	for _, anchor := range anchors {
		items, err := s.store.ListComments(ctx, anchor.ID)
		if err != nil {
			return FileView{}, err
		}
		comments[anchor.ID] = items
	}
	lines := buildFileLines(string(content), anchors)
	return FileView{
		Repo:     repo,
		Ref:      ref,
		Path:     path,
		Content:  string(content),
		Lines:    lines,
		Diff:     diff,
		Files:    files,
		Anchors:  anchors,
		Comments: comments,
	}, nil
}

func (s *Service) ResolveAll(ctx context.Context) error {
	repos, err := s.store.ListRepos(ctx)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		anchors, err := s.listResolvableAnchors(ctx, repo.ID, "")
		if err != nil {
			return err
		}
		seen := map[string]struct{}{}
		for _, anchor := range anchors {
			key := repo.ID + "::" + anchor.Binding.Ref + "::" + anchor.Binding.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if _, err := s.ResolvePath(ctx, repo.ID, anchor.Binding.Ref, anchor.Binding.Path); err != nil {
				return err
			}
		}
	}
	return nil
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
	lineMap := map[int]bool{}
	for _, anchor := range anchors {
		start := anchor.Binding.StartLine
		end := anchor.Binding.EndLine
		if start <= 0 {
			start = 1
		}
		if end < start {
			end = start
		}
		for line := start; line <= end; line++ {
			lineMap[line] = true
		}
	}
	sourceLines := strings.Split(content, "\n")
	lines := make([]FileLine, 0, len(sourceLines))
	for idx, line := range sourceLines {
		lines = append(lines, FileLine{
			Number:      idx + 1,
			Text:        line,
			Highlighted: lineMap[idx+1],
		})
	}
	return lines
}
