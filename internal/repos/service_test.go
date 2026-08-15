package repos_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/repos"
)

func TestServiceReadsWorkingTreeAndHead(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")

	filePath := filepath.Join(root, "sample.go")
	if err := os.WriteFile(filePath, []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	svc := repos.NewService()
	head, err := svc.Head(ctx, root)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if strings.TrimSpace(head) == "" {
		t.Fatalf("expected head sha")
	}

	content, err := svc.ReadFile(ctx, root, "WORKTREE", "sample.go")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != "package sample\n" {
		t.Fatalf("unexpected file contents: %q", string(content))
	}
}

func TestServiceReturnsStructuredGitErrors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	svc := repos.NewService()
	_, err := svc.Head(ctx, root)
	if err == nil {
		t.Fatalf("expected git error")
	}
	message := err.Error()
	if !strings.Contains(message, "stderr:") {
		t.Fatalf("expected stderr label in error, got %q", message)
	}
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
