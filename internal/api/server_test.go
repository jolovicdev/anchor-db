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

	"github.com/jolovicdev/anchor-db/internal/api"
	"github.com/jolovicdev/anchor-db/internal/app"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
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

func TestServerValidatesBadRequestsAndServesHealth(t *testing.T) {
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

	healthResp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected health 200, got %d", healthResp.StatusCode)
	}

	repoBody := bytes.NewReader([]byte(`{"name":"demo"}`))
	repoResp, err := http.Post(server.URL+"/v1/repos", "application/json", repoBody)
	if err != nil {
		t.Fatalf("post invalid repo: %v", err)
	}
	defer repoResp.Body.Close()
	if repoResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing path, got %d", repoResp.StatusCode)
	}

	repoPayload := map[string]string{"name": "demo", "path": repoRoot}
	encodedRepo, _ := json.Marshal(repoPayload)
	createRepoResp, err := http.Post(server.URL+"/v1/repos", "application/json", bytes.NewReader(encodedRepo))
	if err != nil {
		t.Fatalf("post repo: %v", err)
	}
	defer createRepoResp.Body.Close()
	var repoResult struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createRepoResp.Body).Decode(&repoResult); err != nil {
		t.Fatalf("decode repo: %v", err)
	}

	anchorPayload := map[string]any{
		"repo_id":    repoResult.ID,
		"ref":        "WORKTREE",
		"path":       "sample.go",
		"start_line": 0,
		"start_col":  1,
		"end_line":   7,
		"end_col":    2,
		"kind":       "warning",
		"title":      "",
		"body":       "",
		"author":     "",
	}
	anchorBody, _ := json.Marshal(anchorPayload)
	anchorResp, err := http.Post(server.URL+"/v1/anchors", "application/json", bytes.NewReader(anchorBody))
	if err != nil {
		t.Fatalf("post invalid anchor: %v", err)
	}
	defer anchorResp.Body.Close()
	if anchorResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request for invalid anchor, got %d", anchorResp.StatusCode)
	}

	searchResp, err := http.Get(server.URL + "/v1/search")
	if err != nil {
		t.Fatalf("get invalid search: %v", err)
	}
	defer searchResp.Body.Close()
	if searchResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing search query, got %d", searchResp.StatusCode)
	}
}

func TestServerRepoAndAnchorLifecycle(t *testing.T) {
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

	repoPayload := map[string]string{"name": "demo", "path": repoRoot}
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

	getRepoResp, err := http.Get(server.URL + "/v1/repos/" + repoResult.ID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	defer getRepoResp.Body.Close()
	if getRepoResp.StatusCode != http.StatusOK {
		t.Fatalf("expected repo get 200, got %d", getRepoResp.StatusCode)
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

	var anchorResult struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Status  string `json:"status"`
		Binding struct {
			StartLine int `json:"start_line"`
		} `json:"binding"`
	}
	if err := json.NewDecoder(anchorResp.Body).Decode(&anchorResult); err != nil {
		t.Fatalf("decode anchor: %v", err)
	}

	updateBody := bytes.NewReader([]byte(`{"kind":"handoff","title":"next step","body":"check retry path","author":"agent://worker","tags":["billing","handoff"]}`))
	updateReq, err := http.NewRequest(http.MethodPatch, server.URL+"/v1/anchors/"+anchorResult.ID, updateBody)
	if err != nil {
		t.Fatalf("new patch request: %v", err)
	}
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("patch anchor: %v", err)
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected patch 200, got %d", updateResp.StatusCode)
	}

	var updatedAnchor struct {
		Kind  string   `json:"kind"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := json.NewDecoder(updateResp.Body).Decode(&updatedAnchor); err != nil {
		t.Fatalf("decode updated anchor: %v", err)
	}
	if updatedAnchor.Kind != "handoff" || updatedAnchor.Title != "next step" || len(updatedAnchor.Tags) != 2 {
		t.Fatalf("unexpected updated anchor: %#v", updatedAnchor)
	}

	closeResp, err := http.Post(server.URL+"/v1/anchors/"+anchorResult.ID+"/close", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("close anchor: %v", err)
	}
	defer closeResp.Body.Close()
	if closeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected close 200, got %d", closeResp.StatusCode)
	}

	var closedAnchor struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(closeResp.Body).Decode(&closedAnchor); err != nil {
		t.Fatalf("decode closed anchor: %v", err)
	}
	if closedAnchor.Status != "archived" {
		t.Fatalf("expected archived status, got %s", closedAnchor.Status)
	}

	reopenResp, err := http.Post(server.URL+"/v1/anchors/"+anchorResult.ID+"/reopen", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("reopen anchor: %v", err)
	}
	defer reopenResp.Body.Close()
	if reopenResp.StatusCode != http.StatusOK {
		t.Fatalf("expected reopen 200, got %d", reopenResp.StatusCode)
	}

	updated := []byte("package sample\n\nimport \"fmt\"\n\ntype Worker struct{}\n\nfunc Add(a int, b int) int {\n\tfmt.Println(a, b)\n\treturn a + b\n}\n")
	if err := os.WriteFile(filepath.Join(repoRoot, "sample.go"), updated, 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}

	resolveResp, err := http.Post(server.URL+"/v1/anchors/"+anchorResult.ID+"/resolve", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	defer resolveResp.Body.Close()
	if resolveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected resolve 200, got %d", resolveResp.StatusCode)
	}

	var resolvedAnchor struct {
		Status  string `json:"status"`
		Binding struct {
			StartLine int `json:"start_line"`
		} `json:"binding"`
	}
	if err := json.NewDecoder(resolveResp.Body).Decode(&resolvedAnchor); err != nil {
		t.Fatalf("decode resolved anchor: %v", err)
	}
	if resolvedAnchor.Status != "active" {
		t.Fatalf("expected active after resolve, got %s", resolvedAnchor.Status)
	}
	if resolvedAnchor.Binding.StartLine <= anchorResult.Binding.StartLine {
		t.Fatalf("expected anchor to move after resolve")
	}

	syncResp, err := http.Post(server.URL+"/v1/repos/"+repoResult.ID+"/sync", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("sync repo: %v", err)
	}
	defer syncResp.Body.Close()
	if syncResp.StatusCode != http.StatusOK {
		t.Fatalf("expected sync 200, got %d", syncResp.StatusCode)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, server.URL+"/v1/repos/"+repoResult.ID, nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected delete 200, got %d", deleteResp.StatusCode)
	}

	missingRepoResp, err := http.Get(server.URL + "/v1/repos/" + repoResult.ID)
	if err != nil {
		t.Fatalf("get deleted repo: %v", err)
	}
	defer missingRepoResp.Body.Close()
	if missingRepoResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected deleted repo get to fail, got %d", missingRepoResp.StatusCode)
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
