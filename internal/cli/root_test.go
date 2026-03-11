package cli_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"anchordb/internal/cli"
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
