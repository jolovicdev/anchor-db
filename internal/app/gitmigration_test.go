package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func gitRepoWithFile(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")
	return root
}

func serviceFor(t *testing.T, root string) (*app.Service, domain.Repo) {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	svc, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	repo, err := svc.RegisterRepo(context.Background(), "demo", root)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}
	return svc, repo
}

// Three identical blocks. Text matching cannot tell them apart and scores them
// equally, so it falls back to the first occurrence. The diff knows which one
// the anchor was actually on.
const duplicateBlocks = `package demo

func alpha() error {
	return retry()
}

func bravo() error {
	return retry()
}

func charlie() error {
	return retry()
}
`

func TestGitMappingPicksTheRightDuplicateBlock(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "dup.go", duplicateBlocks)
	svc, repo := serviceFor(t, root)

	// Anchor the *third* identical block (lines 11-13).
	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "dup.go",
		StartLine: 11, StartCol: 1, EndLine: 13, EndCol: 2,
		Kind: "warning", Title: "third block", Body: "This one specifically.", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if anchor.Binding.BaseCommit == "" {
		t.Fatal("anchor should record a base commit")
	}

	// Insert four lines at the top and commit, pushing every block down by 4.
	shifted := "// header\n// header\n// header\n// header\n" + duplicateBlocks
	if err := os.WriteFile(filepath.Join(root, "dup.go"), []byte(shifted), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	updated, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "dup.go")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("expected 1 resolved anchor, got %d", len(updated))
	}

	got := updated[0]
	if got.Status != domain.AnchorStatusActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
	// The third block now starts at line 15, not line 7 (the first block).
	if got.Binding.StartLine != 15 || got.Binding.EndLine != 17 {
		t.Errorf("anchor landed on lines %d-%d, want 15-17 (the third block)",
			got.Binding.StartLine, got.Binding.EndLine)
	}

	events, err := svc.ListAnchorEvents(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var reason string
	for _, event := range events {
		if event.Type == domain.AnchorEventMoved {
			reason = event.Reason
		}
	}
	if reason != "git line mapping" {
		t.Errorf("move reason = %q, want %q", reason, "git line mapping")
	}
}

// Without a base commit the resolver has only text to go on, which is what
// makes the duplicate case ambiguous. This pins that contrast so the value of
// the git path stays visible.
func TestWithoutBaseCommitDuplicatesAreAmbiguous(t *testing.T) {
	ctx := context.Background()
	root := gitRepoWithFile(t, "dup.go", duplicateBlocks)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	repo, err := svc.RegisterRepo(ctx, "demo", root)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// A legacy-shaped anchor: same span, but no base commit recorded.
	legacy, err := store.CreateAnchor(ctx, domain.Anchor{
		ID: domain.NewID("anchor"), RepoID: repo.ID,
		Kind: domain.AnchorKindWarning, Status: domain.AnchorStatusActive,
		Title: "third block", Body: "b", Author: "a", SourceRef: "WORKTREE",
		Binding: domain.Binding{
			Type: domain.BindingTypeSpan, Ref: "WORKTREE", Path: "dup.go",
			StartLine: 11, StartCol: 1, EndLine: 13, EndCol: 2,
			SelectedText: "func charlie() error {\n\treturn retry()\n}",
		},
	})
	if err != nil {
		t.Fatalf("seed legacy anchor: %v", err)
	}

	shifted := "// header\n// header\n// header\n// header\n" + duplicateBlocks
	if err := os.WriteFile(filepath.Join(root, "dup.go"), []byte(shifted), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "dup.go"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, err := svc.GetAnchor(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// It still resolves (the selected text is unique enough to find *a* match),
	// but it lands on a lower-confidence text match rather than the exact block.
	if got.Binding.Confidence >= 0.99 {
		t.Errorf("confidence = %v; a text match should score below the git path", got.Binding.Confidence)
	}
}

// When a diff base exists, a stale anchor should explain itself in terms of
// what git saw rather than the bare "no match".
//
// Note the code deliberately survives a changed function *body*: the symbol is
// still there, so symbol matching relocates it and it stays active. Going stale
// requires the anchored code to genuinely be gone.
func TestStaleReasonExplainsDeletedCode(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc target() int {\n\treturn 1\n}\n\nfunc keep() int {\n\treturn 2\n}\n"
	root := gitRepoWithFile(t, "s.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "s.go",
		StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 2,
		Kind: "warning", Title: "t", Body: "b", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	// Delete the anchored function outright.
	if err := os.WriteFile(filepath.Join(root, "s.go"),
		[]byte("package demo\n\nfunc keep() int {\n\treturn 2\n}\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "s.go"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.AnchorStatusStale {
		t.Fatalf("status = %q, want stale", got.Status)
	}

	events, err := svc.ListAnchorEvents(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var reason string
	for _, event := range events {
		if event.Type == domain.AnchorEventStale {
			reason = event.Reason
		}
	}
	if reason == "no match" || reason == "" {
		t.Errorf("stale reason = %q, want a git-informed explanation", reason)
	}
	if !strings.Contains(reason, "deleted") {
		t.Errorf("stale reason = %q, want it to mention deletion", reason)
	}
}

// A changed function body keeps the anchor alive via symbol matching. This
// pins that the git path does not make resolution *more* brittle.
func TestChangedBodyStillResolvesViaSymbol(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc target() int {\n\treturn 1\n}\n"
	root := gitRepoWithFile(t, "b.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "b.go",
		StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 2,
		Kind: "warning", Title: "t", Body: "b", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"),
		[]byte("package demo\n\nfunc target() int {\n\treturn compute()\n}\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "b.go"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.AnchorStatusActive {
		t.Errorf("status = %q, want active: the symbol still exists", got.Status)
	}
}

// Moving code far down a file without changing it is the case git mapping
// should nail exactly.
func TestPureMoveKeepsHighConfidence(t *testing.T) {
	ctx := context.Background()
	source := "package demo\n\nfunc target() int {\n\treturn 42\n}\n"
	root := gitRepoWithFile(t, "m.go", source)
	svc, repo := serviceFor(t, root)

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID: repo.ID, Ref: "WORKTREE", Path: "m.go",
		StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 2,
		Kind: "warning", Title: "t", Body: "b", Author: "a",
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	padding := strings.Repeat("// filler\n", 30)
	if err := os.WriteFile(filepath.Join(root, "m.go"),
		[]byte("package demo\n\n"+padding+"func target() int {\n\treturn 42\n}\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "m.go"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.AnchorStatusActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
	if got.Binding.StartLine != 33 {
		t.Errorf("start line = %d, want 33", got.Binding.StartLine)
	}
	if got.Binding.Confidence < 0.99 {
		t.Errorf("confidence = %v, want >= 0.99 for an unchanged block that only moved", got.Binding.Confidence)
	}
}
