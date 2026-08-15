package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// RefWorktree selects the on-disk working tree rather than a committed ref.
const RefWorktree = "WORKTREE"

var (
	// ErrPathOutsideRepo guards every filesystem read against paths supplied by
	// HTTP, MCP, and CLI callers, none of which are trusted to stay in the repo.
	ErrPathOutsideRepo = errors.New("path escapes repository root")
	ErrInvalidRef      = errors.New("invalid git ref")
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

// RelPath normalises a caller-supplied path and rejects anything that would
// leave the repository, including absolute paths, ".." traversal, and the .git
// directory. Symlinks that point outside the repo are handled separately by
// reading through os.Root.
func RelPath(candidate string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") {
		return "", ErrPathOutsideRepo
	}
	cleaned := path.Clean(filepath.ToSlash(trimmed))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrPathOutsideRepo
	}
	if cleaned == ".git" || strings.HasPrefix(cleaned, ".git/") {
		return "", ErrPathOutsideRepo
	}
	return cleaned, nil
}

// ValidateRef rejects refs that git would parse as command-line options. A ref
// such as "--output=/path" turns `git show` into an arbitrary file write, so
// this check runs before any ref reaches a git invocation.
func ValidateRef(ref string) error {
	if ref == "" || ref == RefWorktree {
		return nil
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: must not start with '-'", ErrInvalidRef)
	}
	// A ref is interpolated into "<ref>:<path>", and git ref syntax forbids
	// these characters anyway.
	if strings.ContainsAny(ref, ":\x00\n\r\t ") {
		return fmt.Errorf("%w: contains disallowed characters", ErrInvalidRef)
	}
	return nil
}

func (s *Service) Head(ctx context.Context, root string) (string, error) {
	output, err := s.git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s *Service) ReadFile(ctx context.Context, root, ref, filePath string) ([]byte, error) {
	rel, err := RelPath(filePath)
	if err != nil {
		return nil, err
	}
	if err := ValidateRef(ref); err != nil {
		return nil, err
	}
	if ref == "" || ref == RefWorktree {
		// os.Root confines the read to the repository even if an attacker-
		// controlled path resolves through a symlink pointing elsewhere.
		dir, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
		file, err := dir.Open(rel)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(file)
	}
	output, err := s.git(ctx, root, "show", fmt.Sprintf("%s:%s", ref, rel))
	if err != nil {
		return nil, err
	}
	return []byte(output), nil
}

func (s *Service) ListFiles(ctx context.Context, root, ref string) ([]string, error) {
	if err := ValidateRef(ref); err != nil {
		return nil, err
	}
	var output string
	var err error
	if ref == "" || ref == RefWorktree {
		output, err = s.git(ctx, root, "ls-files")
	} else {
		output, err = s.git(ctx, root, "ls-tree", "-r", "--name-only", ref, "--")
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var files []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		files = append(files, line)
	}
	sort.Strings(files)
	return files, nil
}

func (s *Service) DiffFile(ctx context.Context, root, ref, filePath string) (string, error) {
	rel, err := RelPath(filePath)
	if err != nil {
		return "", err
	}
	switch ref {
	case "", RefWorktree:
		return s.git(ctx, root, "diff", "--", rel)
	default:
		return "", nil
	}
}

// DiffHunks returns a zero-context unified diff for one path between fromRef
// and toRef. An empty or WORKTREE toRef diffs against the working tree.
//
// -U0 is deliberate: with no context lines every hunk is purely added and
// removed lines, which is what makes the resulting line mapping unambiguous.
func (s *Service) DiffHunks(ctx context.Context, root, fromRef, toRef, filePath string) (string, error) {
	rel, err := RelPath(filePath)
	if err != nil {
		return "", err
	}
	if err := ValidateRef(fromRef); err != nil {
		return "", err
	}
	if err := ValidateRef(toRef); err != nil {
		return "", err
	}
	if fromRef == "" || fromRef == RefWorktree {
		return "", fmt.Errorf("%w: a diff base must be a commit", ErrInvalidRef)
	}
	args := []string{"diff", "-U0", fromRef}
	if toRef != "" && toRef != RefWorktree {
		args = append(args, toRef)
	}
	args = append(args, "--", rel)
	return s.git(ctx, root, args...)
}

// ResolveCommit expands a ref into a full commit SHA, so a binding records an
// immutable point rather than a branch name that will move.
func (s *Service) ResolveCommit(ctx context.Context, root, ref string) (string, error) {
	if err := ValidateRef(ref); err != nil {
		return "", err
	}
	target := ref
	if target == "" || target == RefWorktree {
		target = "HEAD"
	}
	output, err := s.git(ctx, root, "rev-parse", target+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s *Service) LanguageForPath(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".rs":
		return "rust"
	default:
		return "text"
	}
}

func (s *Service) git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		message := fmt.Sprintf("git %v: %v", args, err)
		if text := strings.TrimSpace(stderr.String()); text != "" {
			message += ": stderr: " + text
		}
		if text := strings.TrimSpace(stdout.String()); text != "" {
			message += ": stdout: " + text
		}
		return "", fmt.Errorf("%s", message)
	}
	return stdout.String(), nil
}
