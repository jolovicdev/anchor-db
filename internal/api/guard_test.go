package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// A page the user happens to be visiting can post a cross-origin form to a
// loopback service without any preflight, and enctype="text/plain" lets it
// shape a body that parses as JSON. Nothing about such a request is
// distinguishable from a legitimate one except its Origin and content type.
func TestCrossOriginFormPostIsRejected(t *testing.T) {
	server, _, repoRoot := newTestServer(t)

	// A real repository path, so the request would succeed on its merits and the
	// only thing that can stop it is the guard.
	body := strings.NewReader(`{"name":"evil","path":"` + repoRoot + `"}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/repos", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// Exactly what a form post from a hostile page looks like.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Fatalf("a cross-origin form post was accepted (status %d)", resp.StatusCode)
	}
}

// Even with a JSON content type, a request announcing a foreign origin is not
// something a local service should act on.
func TestCrossOriginJSONIsRejected(t *testing.T) {
	server, _, _ := newTestServer(t)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/repos",
		strings.NewReader(`{"name":"evil","path":"/tmp"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin JSON post got %d, want 403", resp.StatusCode)
	}
}

// DNS rebinding points an attacker-controlled name at 127.0.0.1, which makes
// the request same-origin and reaches methods a form cannot send. The browser
// still sends that hostname in Host, which is what gives it away.
func TestRebindingHostIsRejected(t *testing.T) {
	server, _, _ := newTestServer(t)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, err := http.NewRequest(method, server.URL+"/v1/repos", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Host = "attacker.example"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s with a rebound Host got %d, want 403", method, resp.StatusCode)
		}
	}
}

// The guard must not get in the way of the viewer or of ordinary API use.
func TestLoopbackRequestsStillWork(t *testing.T) {
	server, _, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/v1/repos")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("plain loopback GET got %d, want 200", resp.StatusCode)
	}

	// A same-origin browser fetch carries an Origin that matches.
	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/repos", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Origin", server.URL)
	same, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer same.Body.Close()
	if same.StatusCode != http.StatusOK {
		t.Errorf("same-origin GET got %d, want 200", same.StatusCode)
	}
}

func TestWritesRequireJSONContentType(t *testing.T) {
	server, _, _ := newTestServer(t)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/repos",
		strings.NewReader(`{"name":"x","path":"/tmp"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form content type got %d, want 415", resp.StatusCode)
	}
}
