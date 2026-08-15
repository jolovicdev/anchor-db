package repos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/repos"
)

func securityRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")
	return root
}

func TestReadFileRejectsPathsOutsideRepo(t *testing.T) {
	ctx := context.Background()
	root := securityRepo(t)
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	svc := repos.NewService()
	for _, path := range []string{
		"../secret.txt",
		"../../secret.txt",
		"sub/../../secret.txt",
		"/etc/passwd",
		".git/config",
	} {
		if content, err := svc.ReadFile(ctx, root, "WORKTREE", path); err == nil {
			t.Errorf("ReadFile(%q) succeeded and returned %q, want rejection", path, string(content))
		}
	}
}

// A symlink inside the repo pointing out of it must not become a way around
// the path check, since the cleaned path itself looks perfectly ordinary.
func TestReadFileRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	root := securityRepo(t)
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	svc := repos.NewService()
	if content, err := svc.ReadFile(ctx, root, "WORKTREE", "link.txt"); err == nil {
		t.Errorf("ReadFile through symlink succeeded and returned %q, want rejection", string(content))
	}
}

func TestReadFileStillReadsRepoFiles(t *testing.T) {
	ctx := context.Background()
	root := securityRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "pkg", "inner"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "inner", "deep.go"), []byte("package inner\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	svc := repos.NewService()
	for _, path := range []string{"sample.go", "./sample.go", "pkg/inner/deep.go", "pkg/../sample.go"} {
		if _, err := svc.ReadFile(ctx, root, "WORKTREE", path); err != nil {
			t.Errorf("ReadFile(%q) failed: %v", path, err)
		}
	}
}

// A ref beginning with "-" is parsed by git as an option. "git show
// --output=<path>" writes a file and exits zero, so this must be refused
// before it reaches git.
func TestRefsThatLookLikeOptionsAreRejected(t *testing.T) {
	ctx := context.Background()
	root := securityRepo(t)
	svc := repos.NewService()
	victim := filepath.Join(t.TempDir(), "pwned.txt")

	for _, ref := range []string{"--output=" + victim, "-x", "--help"} {
		if _, err := svc.ReadFile(ctx, root, ref, "sample.go"); !errors.Is(err, repos.ErrInvalidRef) {
			t.Errorf("ReadFile with ref %q: got err %v, want ErrInvalidRef", ref, err)
		}
		if _, err := svc.ListFiles(ctx, root, ref); !errors.Is(err, repos.ErrInvalidRef) {
			t.Errorf("ListFiles with ref %q: got err %v, want ErrInvalidRef", ref, err)
		}
	}
	if entries, err := filepath.Glob(filepath.Join(filepath.Dir(victim), "*")); err != nil {
		t.Fatalf("glob: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("git wrote unexpected files: %v", entries)
	}
}

func TestValidRefsStillResolve(t *testing.T) {
	ctx := context.Background()
	root := securityRepo(t)
	svc := repos.NewService()

	head, err := svc.Head(ctx, root)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	for _, ref := range []string{head, "HEAD", "WORKTREE", ""} {
		if _, err := svc.ReadFile(ctx, root, ref, "sample.go"); err != nil {
			t.Errorf("ReadFile with ref %q failed: %v", ref, err)
		}
		if _, err := svc.ListFiles(ctx, root, ref); err != nil {
			t.Errorf("ListFiles with ref %q failed: %v", ref, err)
		}
	}
}
