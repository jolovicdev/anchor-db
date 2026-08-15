package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/domain"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func TestStoreCreatesThreadedComments(t *testing.T) {
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
		Author:    "agent://planner",
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

	root, err := store.CreateComment(ctx, domain.Comment{
		ID:       "comment-1",
		AnchorID: anchor.ID,
		Author:   "agent://planner",
		Body:     "check the idempotency path",
	})
	if err != nil {
		t.Fatalf("create root comment: %v", err)
	}

	_, err = store.CreateComment(ctx, domain.Comment{
		ID:       "comment-2",
		AnchorID: anchor.ID,
		ParentID: root.ID,
		Author:   "human://reviewer",
		Body:     "agreed",
	})
	if err != nil {
		t.Fatalf("create reply: %v", err)
	}

	comments, err := store.ListComments(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[1].ParentID != root.ID {
		t.Fatalf("expected reply parent %s, got %s", root.ID, comments[1].ParentID)
	}
}
