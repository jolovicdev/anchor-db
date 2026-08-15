package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/domain"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func TestStoreCreatesListsAndTracksAnchorHistory(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID:         "repo-1",
		Name:       "demo",
		RootPath:   "/tmp/demo",
		DefaultRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	anchor, err := store.CreateAnchor(ctx, domain.Anchor{
		ID:        "anchor-1",
		RepoID:    repo.ID,
		Kind:      domain.AnchorKindWarning,
		Status:    domain.AnchorStatusActive,
		Title:     "Idempotency warning",
		Body:      "Retries here can duplicate work.",
		Author:    "agent://planner",
		SourceRef: "HEAD",
		Tags:      []string{"billing", "warning"},
		Binding: domain.Binding{
			Type:             domain.BindingTypeSymbol,
			Ref:              "HEAD",
			Path:             "internal/billing/retry.go",
			Language:         "go",
			SymbolPath:       "billing.(*Retrier).RetryCharge",
			StartLine:        10,
			StartCol:         1,
			EndLine:          18,
			EndCol:           2,
			SelectedText:     "func (r *Retrier) RetryCharge() error { return nil }",
			SelectedTextHash: "selected",
			BeforeContext:    "type Retrier struct{}",
			BeforeHash:       "before",
			AfterContext:     "func helper() {}",
			AfterHash:        "after",
			Confidence:       1,
		},
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	anchors, err := store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repo.ID,
		Path:   anchor.Binding.Path,
	})
	if err != nil {
		t.Fatalf("list anchors: %v", err)
	}
	if len(anchors) != 1 {
		t.Fatalf("expected one anchor, got %d", len(anchors))
	}

	updated, err := store.ReplaceBinding(
		ctx,
		anchor.ID,
		domain.Binding{
			Type:             domain.BindingTypeSymbol,
			Ref:              "HEAD",
			Path:             anchor.Binding.Path,
			Language:         "go",
			SymbolPath:       anchor.Binding.SymbolPath,
			StartLine:        14,
			StartCol:         1,
			EndLine:          22,
			EndCol:           2,
			SelectedText:     anchor.Binding.SelectedText,
			SelectedTextHash: anchor.Binding.SelectedTextHash,
			BeforeContext:    anchor.Binding.BeforeContext,
			BeforeHash:       anchor.Binding.BeforeHash,
			AfterContext:     anchor.Binding.AfterContext,
			AfterHash:        anchor.Binding.AfterHash,
			Confidence:       0.92,
		},
		"inserted imports moved the symbol",
		0.92,
	)
	if err != nil {
		t.Fatalf("replace binding: %v", err)
	}
	if updated.Binding.StartLine != 14 {
		t.Fatalf("expected updated binding start line 14, got %d", updated.Binding.StartLine)
	}

	events, err := store.ListAnchorEvents(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("list anchor events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected create and move events, got %d", len(events))
	}
	if events[1].Type != domain.AnchorEventMoved {
		t.Fatalf("expected move event, got %s", events[1].Type)
	}
}

func TestStoreListAnchorsFiltersAndPaginatesInQuery(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID:         "repo-1",
		Name:       "demo",
		RootPath:   "/tmp/demo",
		DefaultRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	create := func(id, path, symbol string, line int) {
		t.Helper()
		_, err := store.CreateAnchor(ctx, domain.Anchor{
			ID:        id,
			RepoID:    repo.ID,
			Kind:      domain.AnchorKindWarning,
			Status:    domain.AnchorStatusActive,
			Title:     id,
			Body:      "body",
			Author:    "human://test",
			SourceRef: "HEAD",
			Binding: domain.Binding{
				Type:             domain.BindingTypeSymbol,
				Ref:              "HEAD",
				Path:             path,
				Language:         "go",
				SymbolPath:       symbol,
				StartLine:        line,
				StartCol:         1,
				EndLine:          line + 1,
				EndCol:           2,
				SelectedText:     "func sample() {}",
				SelectedTextHash: "hash",
			},
		})
		if err != nil {
			t.Fatalf("create anchor %s: %v", id, err)
		}
	}

	create("anchor-1", "a.go", "A", 1)
	create("anchor-2", "b.go", "B", 10)
	create("anchor-3", "b.go", "C", 20)

	filtered, err := store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repo.ID,
		Path:   "b.go",
	})
	if err != nil {
		t.Fatalf("list filtered anchors: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 anchors for b.go, got %d", len(filtered))
	}

	symbolFiltered, err := store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID:     repo.ID,
		Path:       "b.go",
		SymbolPath: "C",
	})
	if err != nil {
		t.Fatalf("list symbol filtered anchors: %v", err)
	}
	if len(symbolFiltered) != 1 || symbolFiltered[0].ID != "anchor-3" {
		t.Fatalf("expected only anchor-3, got %#v", symbolFiltered)
	}

	paged, err := store.ListAnchors(ctx, domain.AnchorFilter{
		RepoID: repo.ID,
		Limit:  1,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("list paged anchors: %v", err)
	}
	if len(paged) != 1 || paged[0].ID != "anchor-2" {
		t.Fatalf("expected only anchor-2 in second page, got %#v", paged)
	}
}

func TestStoreDeletesRepoAndRemovesAnchorsFromSearch(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID:         "repo-1",
		Name:       "demo",
		RootPath:   "/tmp/demo",
		DefaultRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	anchor, err := store.CreateAnchor(ctx, domain.Anchor{
		ID:        "anchor-1",
		RepoID:    repo.ID,
		Kind:      domain.AnchorKindWarning,
		Status:    domain.AnchorStatusActive,
		Title:     "warning",
		Body:      "watch this",
		Author:    "human://test",
		SourceRef: "HEAD",
		Binding: domain.Binding{
			Type:             domain.BindingTypeSpan,
			Ref:              "HEAD",
			Path:             "sample.go",
			Language:         "go",
			StartLine:        1,
			StartCol:         1,
			EndLine:          1,
			EndCol:           8,
			SelectedText:     "package",
			SelectedTextHash: "hash",
		},
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	if _, err := store.CreateComment(ctx, domain.Comment{
		ID:       "comment-1",
		AnchorID: anchor.ID,
		Author:   "human://test",
		Body:     "note",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	hits, err := store.Search(ctx, domain.SearchQuery{Query: "warning", RepoID: repo.ID})
	if err != nil {
		t.Fatalf("search before delete: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected search hits before repo delete")
	}

	if err := store.DeleteRepo(ctx, repo.ID); err != nil {
		t.Fatalf("delete repo: %v", err)
	}

	if _, err := store.GetRepo(ctx, repo.ID); err != domain.ErrNotFound {
		t.Fatalf("expected repo to be deleted, got %v", err)
	}

	anchors, err := store.ListAnchors(ctx, domain.AnchorFilter{RepoID: repo.ID})
	if err != nil {
		t.Fatalf("list anchors after delete: %v", err)
	}
	if len(anchors) != 0 {
		t.Fatalf("expected anchors to be deleted, got %d", len(anchors))
	}

	hits, err = store.Search(ctx, domain.SearchQuery{Query: "warning", RepoID: repo.ID})
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected search hits to be deleted, got %d", len(hits))
	}
}
