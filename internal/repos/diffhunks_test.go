package repos_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/gitmap"
	"github.com/jolovicdev/anchor-db/internal/repos"
)

// numbered builds a file whose every line names itself, so a mapped line number
// can be checked against the content that actually landed there.
func numbered(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func lineAt(t *testing.T, content string, line int) string {
	t.Helper()
	parts := strings.Split(content, "\n")
	if line < 1 || line > len(parts) {
		t.Fatalf("line %d out of range (%d lines)", line, len(parts))
	}
	return parts[line-1]
}

// The hunk parser is only useful if it agrees with real git output, so this
// drives actual `git diff -U0` rather than hand-written diff text.
func TestDiffHunksMapLinesAgainstRealGit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")

	original := numbered("alpha", "bravo", "charlie", "delta", "echo", "foxtrot")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	svc := repos.NewService()
	base, err := svc.ResolveCommit(ctx, root, "HEAD")
	if err != nil {
		t.Fatalf("resolve commit: %v", err)
	}

	cases := []struct {
		name    string
		updated string
		// old line number -> the line text expected at the mapped position
		expect map[int]string
		// old line numbers expected to be reported as deleted
		deleted []int
	}{
		{
			name:    "insert above",
			updated: numbered("new1", "new2", "alpha", "bravo", "charlie", "delta", "echo", "foxtrot"),
			expect:  map[int]string{1: "alpha", 3: "charlie", 6: "foxtrot"},
		},
		{
			name:    "insert in the middle",
			updated: numbered("alpha", "bravo", "inserted", "charlie", "delta", "echo", "foxtrot"),
			expect:  map[int]string{1: "alpha", 2: "bravo", 3: "charlie", 6: "foxtrot"},
		},
		{
			name:    "delete above",
			updated: numbered("charlie", "delta", "echo", "foxtrot"),
			expect:  map[int]string{3: "charlie", 6: "foxtrot"},
			deleted: []int{1, 2},
		},
		{
			name:    "replace a line",
			updated: numbered("alpha", "bravo", "CHANGED", "delta", "echo", "foxtrot"),
			expect:  map[int]string{1: "alpha", 4: "delta", 6: "foxtrot"},
			deleted: []int{3},
		},
		{
			name:    "insert and delete together",
			updated: numbered("pre", "alpha", "charlie", "delta", "echo", "extra", "foxtrot"),
			expect:  map[int]string{1: "alpha", 3: "charlie", 6: "foxtrot"},
			deleted: []int{2},
		},
		{
			name:    "append at the end",
			updated: numbered("alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"),
			expect:  map[int]string{1: "alpha", 6: "foxtrot"},
		},
		{
			name:    "no change",
			updated: original,
			expect:  map[int]string{1: "alpha", 4: "delta", 6: "foxtrot"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(tc.updated), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			diff, err := svc.DiffHunks(ctx, root, base, repos.RefWorktree, "f.txt")
			if err != nil {
				t.Fatalf("diff hunks: %v", err)
			}
			m := gitmap.Parse(diff)

			for oldLine, wantText := range tc.expect {
				mapped, ok := m.MapLine(oldLine)
				if !ok {
					t.Errorf("line %d (%q) reported deleted\ndiff:\n%s",
						oldLine, lineAt(t, original, oldLine), diff)
					continue
				}
				if got := lineAt(t, tc.updated, mapped); got != wantText {
					t.Errorf("line %d mapped to %d which holds %q, want %q\ndiff:\n%s",
						oldLine, mapped, got, wantText, diff)
				}
			}
			for _, oldLine := range tc.deleted {
				if mapped, ok := m.MapLine(oldLine); ok {
					t.Errorf("line %d (%q) should be deleted, mapped to %d (%q)\ndiff:\n%s",
						oldLine, lineAt(t, original, oldLine), mapped,
						lineAt(t, tc.updated, mapped), diff)
				}
			}
		})
	}
}

func TestDiffHunksRejectsWorktreeAsBase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc := repos.NewService()
	if _, err := svc.DiffHunks(ctx, root, repos.RefWorktree, "", "f.txt"); err == nil {
		t.Error("a worktree diff base should be rejected")
	}
}
