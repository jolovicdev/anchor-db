package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func Run(ctx context.Context, stdout, stderr io.Writer, args []string, baseURL string) int {
	if baseURL == "" {
		baseURL = os.Getenv("ANCHOR_DB_URL")
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:7740"
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl <repo|anchor> ...")
		return 1
	}
	client := &http.Client{}
	switch args[0] {
	case "repo":
		return runRepo(ctx, client, stdout, stderr, args[1:], baseURL)
	case "anchor":
		return runAnchor(ctx, client, stdout, stderr, args[1:], baseURL)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown command")
		return 1
	}
}

func runRepo(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl repo add --name <name> --path <path>")
		return 1
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("repo add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "")
		path := fs.String("path", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		payload := map[string]string{"name": *name, "path": *path}
		return doJSONRequest(ctx, client, stdout, stderr, http.MethodPost, baseURL+"/v1/repos", payload)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown repo command")
		return 1
	}
}

func runAnchor(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl anchor <list|create>")
		return 1
	}
	switch args[0] {
	case "list":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/anchors", nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		resp, err := client.Do(req)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		_, _ = stdout.Write(body)
		return 0
	case "create":
		fs := flag.NewFlagSet("anchor create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		repoID := fs.String("repo-id", "", "")
		ref := fs.String("ref", "WORKTREE", "")
		path := fs.String("path", "", "")
		startLine := fs.Int("start-line", 0, "")
		startCol := fs.Int("start-col", 1, "")
		endLine := fs.Int("end-line", 0, "")
		endCol := fs.Int("end-col", 1, "")
		kind := fs.String("kind", "warning", "")
		title := fs.String("title", "", "")
		body := fs.String("body", "", "")
		author := fs.String("author", "human://local", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		payload := map[string]any{
			"repo_id":    *repoID,
			"ref":        *ref,
			"path":       *path,
			"start_line": *startLine,
			"start_col":  *startCol,
			"end_line":   *endLine,
			"end_col":    *endCol,
			"kind":       *kind,
			"title":      *title,
			"body":       *body,
			"author":     *author,
		}
		return doJSONRequest(ctx, client, stdout, stderr, http.MethodPost, baseURL+"/v1/anchors", payload)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown anchor command")
		return 1
	}
}

func doJSONRequest(ctx context.Context, client *http.Client, stdout, stderr io.Writer, method, url string, payload any) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(encoded))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if resp.StatusCode >= 400 {
		_, _ = fmt.Fprintln(stderr, string(body))
		return 1
	}
	_, _ = stdout.Write(body)
	return 0
}
