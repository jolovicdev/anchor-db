package repos

import (
	"bytes"
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
