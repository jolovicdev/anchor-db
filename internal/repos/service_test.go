package repos_test

import (
	"context"
	"errors"
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

// A git failure should report what git said and nothing about how it was run.
// The argv, the exit status, and a "stderr:" label made an ordinary mistake --
// a bad ref, a directory that is not a repository -- read like an internal
// fault, and told the caller nothing they could act on.
func TestServiceReturnsStructuredGitErrors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	svc := repos.NewService()
	_, err := svc.Head(ctx, root)
	if err == nil {
		t.Fatalf("expected git error")
	}
	message := err.Error()

	if !strings.Contains(message, "not a git repository") {
		t.Errorf("error should carry git's own explanation, got %q", message)
	}
	if !strings.Contains(message, "rev-parse") {
		t.Errorf("error should name the git subcommand, got %q", message)
	}
	for _, leak := range []string{"stderr:", "stdout:", "exit status", "["} {
		if strings.Contains(message, leak) {
			t.Errorf("error leaks %q: %q", leak, message)
		}
	}
}

func TestReadFileReportsMissingFilesPlainly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runGit(t, root, "init")

	svc := repos.NewService()
	_, err := svc.ReadFile(ctx, root, repos.RefWorktree, "ghost.py")
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !errors.Is(err, repos.ErrFileNotFound) {
		t.Errorf("error should be ErrFileNotFound, got %v", err)
	}
	// "openat" names the syscall the read happens to use.
	if strings.Contains(err.Error(), "openat") {
		t.Errorf("error leaks the syscall: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "ghost.py") {
		t.Errorf("error should name the file, got %q", err.Error())
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
