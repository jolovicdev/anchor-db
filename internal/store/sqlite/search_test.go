package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"anchordb/internal/domain"
	sqlitestore "anchordb/internal/store/sqlite"
)

func TestStoreSearchFindsAnchorAndCommentBodies(t *testing.T) {
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
		Body:      "Retries here can duplicate work when idempotency breaks.",
		Author:    "agent://planner",
		SourceRef: "HEAD",
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
		},
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	if _, err := store.CreateComment(ctx, domain.Comment{
		ID:       "comment-1",
		AnchorID: anchor.ID,
		Author:   "human://reviewer",
		Body:     "This bug surfaced during a retry incident and needs a regression test.",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	hits, err := store.Search(ctx, domain.SearchQuery{
		Query:  "warning",
		RepoID: repo.ID,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %d", len(hits))
	}

	commentHits, err := store.Search(ctx, domain.SearchQuery{Query: "regression"})
	if err != nil {
		t.Fatalf("search comment body: %v", err)
	}
	if len(commentHits) != 1 || commentHits[0].DocumentType != "comment" {
		t.Fatalf("expected one comment hit, got %#v", commentHits)
	}

	pathHits, err := store.Search(ctx, domain.SearchQuery{
		Query: "duplicate",
		Path:  "internal/billing/retry.go",
	})
	if err != nil {
		t.Fatalf("search by path: %v", err)
	}
	if len(pathHits) == 0 {
		t.Fatalf("expected path filtered search hit")
	}
}
