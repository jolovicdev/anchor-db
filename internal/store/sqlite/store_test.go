package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"anchordb/internal/domain"
	sqlitestore "anchordb/internal/store/sqlite"
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
