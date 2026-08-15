package cli_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/cli"
)

func TestRunListsAnchors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/anchors" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":"anchor-1","title":"warning"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "list"}, server.URL)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout output")
	}
}

func TestRunRepoListContextAndCommentCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/repos" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"repo-1","name":"demo"}]`))
		case r.URL.Path == "/v1/context" && r.Method == http.MethodGet:
			if r.URL.Query().Get("repo_id") != "repo-1" || r.URL.Query().Get("path") != "sample.go" {
				t.Fatalf("unexpected context query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"repo":{"id":"repo-1"},"anchors":[{"id":"anchor-1"}]}`))
		case r.URL.Path == "/v1/anchors/anchor-1/comments" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"comment-1","body":"note"}]`))
		case r.URL.Path == "/v1/anchors/anchor-1/comments" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"comment-2","body":"reply"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"repo", "list"}, server.URL); code != 0 {
		t.Fatalf("repo list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"repo-1\"") {
		t.Fatalf("expected repo list output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"context", "--repo-id", "repo-1", "--path", "sample.go"}, server.URL); code != 0 {
		t.Fatalf("context failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"anchor-1\"") {
		t.Fatalf("expected context output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"comment", "list", "--anchor-id", "anchor-1"}, server.URL); code != 0 {
		t.Fatalf("comment list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"comment-1\"") {
		t.Fatalf("expected comments output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"comment", "add", "--anchor-id", "anchor-1", "--author", "human://test", "--body", "reply"}, server.URL); code != 0 {
		t.Fatalf("comment add failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"comment-2\"") {
		t.Fatalf("expected comment add output, got %s", stdout.String())
	}
}

func TestRunAnchorListFiltersAndGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/anchors" && r.Method == http.MethodGet:
			if r.URL.Query().Get("repo_id") != "repo-1" || r.URL.Query().Get("path") != "sample.go" || r.URL.Query().Get("limit") != "10" {
				t.Fatalf("unexpected anchor list query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"anchor-1","title":"warning"}]`))
		case r.URL.Path == "/v1/anchors/anchor-1" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"anchor-1","title":"warning"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "list", "--repo-id", "repo-1", "--path", "sample.go", "--limit", "10"}, server.URL); code != 0 {
		t.Fatalf("anchor list failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"anchor-1\"") {
		t.Fatalf("expected anchor list output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "get", "--id", "anchor-1"}, server.URL); code != 0 {
		t.Fatalf("anchor get failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"warning\"") {
		t.Fatalf("expected anchor get output, got %s", stdout.String())
	}
}

func TestRunSearchCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/search" && r.Method == http.MethodGet {
			if r.URL.Query().Get("query") != "retry" || r.URL.Query().Get("repo_id") != "repo-1" {
				t.Fatalf("unexpected search query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"document_type":"anchor","anchor_id":"anchor-1","snippet":"retry note"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(context.Background(), &stdout, &stderr, []string{"search", "--query", "retry", "--repo-id", "repo-1"}, server.URL)
	if code != 0 {
		t.Fatalf("search failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"retry note\"") {
		t.Fatalf("expected search output, got %s", stdout.String())
	}
}

func TestRunRepoAndAnchorLifecycleCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/repos" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"repo-1","name":"demo"}`))
		case r.URL.Path == "/v1/repos/repo-1" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"repo-1","name":"demo"}`))
		case r.URL.Path == "/v1/repos/repo-1/sync" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"repo-1","default_ref":"abc123"}`))
		case r.URL.Path == "/v1/repos/repo-1" && r.Method == http.MethodDelete:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":true,"id":"repo-1"}`))
		case r.URL.Path == "/v1/anchors/anchor-1" && r.Method == http.MethodPatch:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"anchor-1","title":"updated","status":"active"}`))
		case r.URL.Path == "/v1/anchors/anchor-1/close" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"anchor-1","status":"archived"}`))
		case r.URL.Path == "/v1/anchors/anchor-1/reopen" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"anchor-1","status":"active"}`))
		case r.URL.Path == "/v1/anchors/anchor-1/resolve" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"anchor-1","status":"active"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"repo", "add", "--name", "demo", "--path", "/tmp/demo"}, server.URL); code != 0 {
		t.Fatalf("repo add failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"repo-1\"") {
		t.Fatalf("expected repo add output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"repo", "get", "--id", "repo-1"}, server.URL); code != 0 {
		t.Fatalf("repo get failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"repo-1\"") {
		t.Fatalf("expected repo get output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"repo", "sync", "--id", "repo-1"}, server.URL); code != 0 {
		t.Fatalf("repo sync failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"abc123\"") {
		t.Fatalf("expected repo sync output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"repo", "remove", "--id", "repo-1"}, server.URL); code != 0 {
		t.Fatalf("repo remove failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"deleted\"") {
		t.Fatalf("expected repo remove output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "update", "--id", "anchor-1", "--kind", "todo", "--title", "updated", "--body", "body", "--author", "human://test"}, server.URL); code != 0 {
		t.Fatalf("anchor update failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"updated\"") {
		t.Fatalf("expected anchor update output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "close", "--id", "anchor-1"}, server.URL); code != 0 {
		t.Fatalf("anchor close failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"archived\"") {
		t.Fatalf("expected anchor close output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "reopen", "--id", "anchor-1"}, server.URL); code != 0 {
		t.Fatalf("anchor reopen failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"active\"") {
		t.Fatalf("expected anchor reopen output, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "resolve", "--id", "anchor-1"}, server.URL); code != 0 {
		t.Fatalf("anchor resolve failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"active\"") {
		t.Fatalf("expected anchor resolve output, got %s", stdout.String())
	}
}

func TestRunFormatsJSONAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"repo_id is required"}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(context.Background(), &stdout, &stderr, []string{"anchor", "list"}, server.URL)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if strings.TrimSpace(stderr.String()) != "error: repo_id is required" {
		t.Fatalf("unexpected stderr output: %q", stderr.String())
	}
}
