package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"anchordb/internal/api"
	"anchordb/internal/app"
	sqlitestore "anchordb/internal/store/sqlite"
)

func TestServerCreatesRepoAndAnchorsAndReturnsContext(t *testing.T) {
	repoRoot := initRepo(t)
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

	repoPayload := map[string]string{
		"name": "demo",
		"path": repoRoot,
	}
	repoBody, _ := json.Marshal(repoPayload)
	repoResp, err := http.Post(server.URL+"/v1/repos", "application/json", bytes.NewReader(repoBody))
	if err != nil {
		t.Fatalf("post repo: %v", err)
	}
	defer repoResp.Body.Close()

	var repoResult struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(repoResp.Body).Decode(&repoResult); err != nil {
		t.Fatalf("decode repo: %v", err)
	}
	if repoResult.ID == "" {
		t.Fatalf("expected repo id")
	}

	anchorPayload := map[string]any{
		"repo_id":    repoResult.ID,
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
	anchorBody, _ := json.Marshal(anchorPayload)
	anchorResp, err := http.Post(server.URL+"/v1/anchors", "application/json", bytes.NewReader(anchorBody))
	if err != nil {
		t.Fatalf("post anchor: %v", err)
	}
	defer anchorResp.Body.Close()
	if anchorResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected created, got %d", anchorResp.StatusCode)
	}

	ctxResp, err := http.Get(server.URL + "/v1/context?repo_id=" + repoResult.ID + "&ref=WORKTREE&path=sample.go&symbol=Add")
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	defer ctxResp.Body.Close()

	var ctxResult struct {
		Anchors []map[string]any `json:"anchors"`
	}
	if err := json.NewDecoder(ctxResp.Body).Decode(&ctxResult); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if len(ctxResult.Anchors) != 1 {
		t.Fatalf("expected one anchor, got %d", len(ctxResult.Anchors))
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\treturn a + b\n}\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
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

var _ = context.Background
