package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/api"
	"github.com/jolovicdev/anchor-db/internal/app"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func newTestServer(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	repoRoot := initRepo(t)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	svc, err := app.NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	server := httptest.NewServer(api.NewServer(svc))
	t.Cleanup(server.Close)

	body, _ := json.Marshal(map[string]string{"name": "demo", "path": repoRoot})
	resp, err := http.Post(server.URL+"/v1/repos", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post repo: %v", err)
	}
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode repo: %v", err)
	}
	return server, created.ID, repoRoot
}

// The file viewer takes a caller-supplied path. It must not serve files from
// outside the registered repository.
func TestViewRejectsPathTraversal(t *testing.T) {
	server, repoID, repoRoot := newTestServer(t)

	secret := filepath.Join(filepath.Dir(repoRoot), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-CANARY"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	for _, path := range []string{
		"../secret.txt",
		"../../secret.txt",
		"sub/../../secret.txt",
		"/etc/passwd",
		".git/config",
	} {
		endpoint := server.URL + "/view?" + url.Values{
			"repo_id": {repoID},
			"path":    {path},
		}.Encode()
		resp, err := http.Get(endpoint)
		if err != nil {
			t.Fatalf("get %q: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			t.Errorf("/view?path=%q returned 200, want an error status", path)
		}
		if strings.Contains(string(body), "TOP-SECRET-CANARY") {
			t.Errorf("/view?path=%q leaked file contents outside the repo", path)
		}
	}
}

func TestViewStillServesRepoFiles(t *testing.T) {
	server, repoID, _ := newTestServer(t)

	endpoint := server.URL + "/view?" + url.Values{
		"repo_id": {repoID},
		"path":    {"sample.go"},
	}.Encode()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

// A ref beginning with "-" reaches git as a command-line option.
func TestViewRejectsOptionLikeRef(t *testing.T) {
	server, repoID, _ := newTestServer(t)
	victimDir := t.TempDir()

	endpoint := server.URL + "/view?" + url.Values{
		"repo_id": {repoID},
		"path":    {"sample.go"},
		"ref":     {"--output=" + filepath.Join(victimDir, "pwned.txt")},
	}.Encode()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200, want an error status")
	}

	entries, err := os.ReadDir(victimDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("git wrote %d unexpected file(s) into %s", len(entries), victimDir)
	}
}

func TestCreateAnchorRejectsPathTraversal(t *testing.T) {
	server, repoID, repoRoot := newTestServer(t)

	secret := filepath.Join(filepath.Dir(repoRoot), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-CANARY\nsecond line\nthird line\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"repo_id":    repoID,
		"ref":        "WORKTREE",
		"path":       "../secret.txt",
		"start_line": 1, "start_col": 1, "end_line": 2, "end_col": 1,
		"kind": "warning", "title": "t", "body": "b", "author": "a",
	})
	resp, err := http.Post(server.URL+"/v1/anchors", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post anchor: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusCreated {
		t.Errorf("anchor creation on a traversal path succeeded: %s", body)
	}
	if strings.Contains(string(body), "TOP-SECRET-CANARY") {
		t.Errorf("response captured file contents from outside the repo: %s", body)
	}
}

// An oversized body must be rejected rather than buffered without bound.
func TestOversizedRequestBodyIsRejected(t *testing.T) {
	server, _, _ := newTestServer(t)

	huge := make([]byte, 4<<20)
	for i := range huge {
		huge[i] = 'a'
	}
	payload, _ := json.Marshal(map[string]string{"name": string(huge), "path": "/tmp"})

	resp, err := http.Post(server.URL+"/v1/repos", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Errorf("oversized body accepted, want rejection")
	}
}
