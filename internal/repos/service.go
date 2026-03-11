package repos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Head(ctx context.Context, root string) (string, error) {
	output, err := s.git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s *Service) ReadFile(ctx context.Context, root, ref, path string) ([]byte, error) {
	if ref == "" || ref == "WORKTREE" {
		return os.ReadFile(filepath.Join(root, path))
	}
	output, err := s.git(ctx, root, "show", fmt.Sprintf("%s:%s", ref, path))
	if err != nil {
		return nil, err
	}
	return []byte(output), nil
}

func (s *Service) ListFiles(ctx context.Context, root, ref string) ([]string, error) {
	var output string
	var err error
	if ref == "" || ref == "WORKTREE" {
		output, err = s.git(ctx, root, "ls-files")
	} else {
		output, err = s.git(ctx, root, "ls-tree", "-r", "--name-only", ref)
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

func (s *Service) DiffFile(ctx context.Context, root, ref, path string) (string, error) {
	switch ref {
	case "", "WORKTREE":
		return s.git(ctx, root, "diff", "--", path)
	default:
		return "", nil
	}
}

func (s *Service) LanguageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
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
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return string(output), nil
}
