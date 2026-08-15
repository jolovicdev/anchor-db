package jobs_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/jobs"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func TestSyncerRunOnceReResolvesTrackedAnchors(t *testing.T) {
	ctx := context.Background()
	repoRoot := initRepo(t)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	service, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	repo, err := service.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}

	anchor, err := service.CreateAnchor(ctx, app.CreateAnchorInput{
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
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "sample.go"), []byte("package sample\n\nimport \"fmt\"\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\tfmt.Println(a, b)\n\treturn a + b\n}\n"), 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}

	syncer := jobs.NewSyncer(service)
	if err := syncer.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}

	updated, err := service.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get anchor: %v", err)
	}
	if updated.Binding.StartLine <= anchor.Binding.StartLine {
		t.Fatalf("expected moved anchor start line > %d, got %d", anchor.Binding.StartLine, updated.Binding.StartLine)
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
