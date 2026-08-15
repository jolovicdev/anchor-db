package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jolovicdev/anchor-db/internal/domain"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func searchFixture(t *testing.T) (*sqlitestore.Store, string) {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID: domain.NewID("repo"), Name: "demo", RootPath: "/tmp/demo", DefaultRef: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if _, err := store.CreateAnchor(ctx, domain.Anchor{
		ID:     domain.NewID("anchor"),
		RepoID: repo.ID,
		Kind:   domain.AnchorKindWarning,
		Status: domain.AnchorStatusActive,
		Title:  "Retry logic",
		Body:   "The C++ shim does not handle don't-retry (v2) responses.",
		Author: "agent://planner",
		Binding: domain.Binding{
			Type: domain.BindingTypeSpan, Ref: "WORKTREE", Path: "sample.go",
			StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1,
			SelectedText: "x", SelectedTextHash: "h",
		},
	}); err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	return store, repo.ID
}

// Users type prose, not FTS5 expressions. None of these may produce an error.
func TestSearchAcceptsOrdinaryTextThatLooksLikeFTSSyntax(t *testing.T) {
	store, repoID := searchFixture(t)
	ctx := context.Background()

	for _, query := range []string{
		"retry",
		"C++",
		"don't",
		"(v2)",
		"retry OR",
		"AND",
		"NOT",
		`foo"bar`,
		"-",
		"*",
		"^caret",
		"a:b",
		"   ",
		"",
		"retry -logic",
		strings.Repeat("x", 500),
	} {
		if _, err := store.Search(ctx, domain.SearchQuery{Query: query, RepoID: repoID}); err != nil {
			t.Errorf("Search(%q) returned error: %v", query, err)
		}
	}
}

func TestSearchStillFindsMatches(t *testing.T) {
	store, repoID := searchFixture(t)
	ctx := context.Background()

	cases := map[string]bool{
		"retry":       true,
		"Retry":       true, // FTS5 is case-insensitive
		"shim":        true,
		"don't":       true, // punctuation inside a term still matches
		"C++":         true,
		"ret*":        true, // prefix search is preserved
		"retry shim":  true, // multiple terms are ANDed
		"nonexistent": false,
		"retry zzzz":  false, // AND semantics: one missing term means no hit
	}
	for query, wantHit := range cases {
		hits, err := store.Search(ctx, domain.SearchQuery{Query: query, RepoID: repoID})
		if err != nil {
			t.Errorf("Search(%q): %v", query, err)
			continue
		}
		if gotHit := len(hits) > 0; gotHit != wantHit {
			t.Errorf("Search(%q): got %d hits, wantHit=%v", query, len(hits), wantHit)
		}
	}
}

// Terms that tokenize to nothing must not silently match every document.
func TestSearchWithNoUsableTermsReturnsNothing(t *testing.T) {
	store, repoID := searchFixture(t)
	ctx := context.Background()

	for _, query := range []string{"", "   ", "-", "*", "--", "()"} {
		hits, err := store.Search(ctx, domain.SearchQuery{Query: query, RepoID: repoID})
		if err != nil {
			t.Errorf("Search(%q): %v", query, err)
			continue
		}
		if len(hits) != 0 {
			t.Errorf("Search(%q) returned %d hits, want 0", query, len(hits))
		}
	}
}

// Comment search rows carry a denormalised copy of their anchor's path, symbol,
// kind and title. When resolution moves an anchor, those copies must move with
// it or a path-filtered search silently stops finding the comments.
func TestCommentSearchRowsFollowTheirAnchor(t *testing.T) {
	store, repoID := searchFixture(t)
	ctx := context.Background()

	anchors, err := store.ListAnchors(ctx, domain.AnchorFilter{RepoID: repoID})
	if err != nil || len(anchors) != 1 {
		t.Fatalf("list anchors: %v (%d found)", err, len(anchors))
	}
	anchor := anchors[0]

	if _, err := store.CreateComment(ctx, domain.Comment{
		ID: domain.NewID("comment"), AnchorID: anchor.ID,
		Author: "human://reviewer", Body: "Reproduced with a zzzunique marker.",
	}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	moved := anchor.Binding
	moved.Path = "renamed.go"
	moved.StartLine, moved.EndLine = 40, 41
	if _, err := store.ApplyResolution(ctx, anchor.ID, moved, domain.AnchorStatusActive, "moved", 0.9); err != nil {
		t.Fatalf("apply resolution: %v", err)
	}

	hits, err := store.Search(ctx, domain.SearchQuery{Query: "zzzunique", Path: "renamed.go"})
	if err != nil {
		t.Fatalf("search at new path: %v", err)
	}
	if len(hits) != 1 || hits[0].DocumentType != "comment" {
		t.Errorf("comment search row did not follow the anchor to renamed.go: %#v", hits)
	}

	stale, err := store.Search(ctx, domain.SearchQuery{Query: "zzzunique", Path: "sample.go"})
	if err != nil {
		t.Fatalf("search at old path: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("comment still indexed under the old path: %#v", stale)
	}
}

func TestSearchSnippetHandlesMultibyteText(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "snippet.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID: domain.NewID("repo"), Name: "demo", RootPath: "/tmp/demo", DefaultRef: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	// Long enough to be truncated, with a multi-byte rune straddling the cut.
	body := "retry " + strings.Repeat("é", 400)
	if _, err := store.CreateAnchor(ctx, domain.Anchor{
		ID: domain.NewID("anchor"), RepoID: repo.ID,
		Kind: domain.AnchorKindWarning, Status: domain.AnchorStatusActive,
		Title: "unicode", Body: body, Author: "a",
		Binding: domain.Binding{
			Type: domain.BindingTypeSpan, Ref: "WORKTREE", Path: "s.go",
			StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1, SelectedText: "x",
		},
	}); err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	hits, err := store.Search(ctx, domain.SearchQuery{Query: "retry", RepoID: repo.ID})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a hit")
	}
	if !utf8.ValidString(hits[0].Snippet) {
		t.Errorf("snippet is not valid UTF-8: %q", hits[0].Snippet)
	}
	if strings.HasSuffix(hits[0].Snippet, "�...") {
		t.Errorf("snippet truncation split a rune: %q", hits[0].Snippet)
	}
}
