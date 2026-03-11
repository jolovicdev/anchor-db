package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/api"
	"github.com/jolovicdev/anchor-db/internal/app"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func TestServerRendersFileViewAndComments(t *testing.T) {
	repoRoot := initViewerRepo(t)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	svc, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	server := httptest.NewServer(api.NewServer(svc))
	defer server.Close()

	repoID := createRepo(t, server.URL, repoRoot)
	anchorID := createAnchor(t, server.URL, repoID)

	if err := os.WriteFile(filepath.Join(repoRoot, "sample.go"), []byte("package sample\n\nimport \"fmt\"\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\tfmt.Println(a, b)\n\treturn a + b\n}\n"), 0o644); err != nil {
		t.Fatalf("rewrite sample: %v", err)
	}

	commentPayload := map[string]any{
		"author": "human://reviewer",
		"body":   "please preserve idempotency",
	}
	body, _ := json.Marshal(commentPayload)
	resp, err := http.Post(server.URL+"/v1/anchors/"+anchorID+"/comments", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post comment: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected created comment, got %d", resp.StatusCode)
	}

	viewResp, err := http.Get(server.URL + "/view?repo_id=" + repoID + "&ref=WORKTREE&path=sample.go")
	if err != nil {
		t.Fatalf("get view: %v", err)
	}
	defer viewResp.Body.Close()
	htmlBytes, err := io.ReadAll(viewResp.Body)
	if err != nil {
		t.Fatalf("read html: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "retry invariant") {
		t.Fatalf("expected anchor title in view")
	}
	if !strings.Contains(html, "please preserve idempotency") {
		t.Fatalf("expected comment body in view")
	}
	if !strings.Contains(html, "func Add") {
		t.Fatalf("expected code in view")
	}
	if !strings.Contains(html, "code-line highlighted") {
		t.Fatalf("expected highlighted code lines in view")
	}
	if !strings.Contains(html, "Git Diff") {
		t.Fatalf("expected diff panel in view")
	}
	if !strings.Contains(html, "diff --git a/sample.go b/sample.go") {
		t.Fatalf("expected file diff content in view")
	}
}

func initViewerRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runViewerGit(t, root, "init")
	runViewerGit(t, root, "config", "user.email", "test@example.com")
	runViewerGit(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\treturn a + b\n}\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	runViewerGit(t, root, "add", ".")
	runViewerGit(t, root, "commit", "-m", "init")
	return root
}

func createRepo(t *testing.T, baseURL, path string) string {
	t.Helper()
	payload := map[string]string{"name": "demo", "path": path}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/v1/repos", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode repo: %v", err)
	}
	return result.ID
}

func createAnchor(t *testing.T, baseURL, repoID string) string {
	t.Helper()
	payload := map[string]any{
		"repo_id":    repoID,
		"ref":        "WORKTREE",
		"path":       "sample.go",
		"start_line": 5,
		"start_col":  1,
		"end_line":   7,
		"end_col":    2,
		"kind":       "warning",
		"title":      "retry invariant",
		"body":       "do not break idempotency",
		"author":     "agent://planner",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/v1/anchors", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode anchor: %v", err)
	}
	return result.ID
}

func runViewerGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
