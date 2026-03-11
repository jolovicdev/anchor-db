package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func TestServiceCreatesAnchorsReturnsContextAndResolvesMoves(t *testing.T) {
	ctx := context.Background()
	repoRoot := initRepo(t)
	dbPath := filepath.Join(t.TempDir(), "anchors.db")

	store, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	svc, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}

	anchor, err := svc.CreateAnchor(ctx, app.CreateAnchorInput{
		RepoID:    repo.ID,
		Ref:       "WORKTREE",
		Path:      "sample.go",
		StartLine: 5,
		StartCol:  1,
		EndLine:   7,
		EndCol:    2,
		Kind:      "warning",
		Title:     "Retry invariant",
		Body:      "Do not break idempotency here.",
		Author:    "agent://planner",
		Tags:      []string{"warning"},
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	contextResult, err := svc.Context(ctx, app.ContextRequest{
		RepoID: repo.ID,
		Ref:    "WORKTREE",
		Path:   "sample.go",
		Symbol: "Add",
	})
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(contextResult.Anchors) != 1 {
		t.Fatalf("expected one anchor, got %d", len(contextResult.Anchors))
	}

	updated := []byte("package sample\n\nimport \"fmt\"\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\tfmt.Println(a, b)\n\treturn a + b\n}\n")
	if err := os.WriteFile(filepath.Join(repoRoot, "sample.go"), updated, 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}

	resolved, err := svc.ResolvePath(ctx, repo.ID, "WORKTREE", "sample.go")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved anchor, got %d", len(resolved))
	}
	if resolved[0].ID != anchor.ID {
		t.Fatalf("expected same anchor id")
	}
	if resolved[0].Binding.StartLine <= anchor.Binding.StartLine {
		t.Fatalf("expected anchor to move down from %d to > %d", resolved[0].Binding.StartLine, anchor.Binding.StartLine)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")

	content := []byte("package sample\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\treturn a + b\n}\n")
	if err := os.WriteFile(filepath.Join(root, "sample.go"), content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
