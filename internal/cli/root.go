package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	case "context":
		return runContext(ctx, client, stdout, stderr, args[1:], baseURL)
	case "comment":
		return runComment(ctx, client, stdout, stderr, args[1:], baseURL)
	case "search":
		return runSearch(ctx, client, stdout, stderr, args[1:], baseURL)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown command")
		return 1
	}
}

func runRepo(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl repo <add|list|get|sync|remove>")
		return 1
	}
	switch args[0] {
	case "list":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/repos", nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
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
	case "get":
		fs := flag.NewFlagSet("repo get", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/repos/"+*id, nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
	case "sync":
		fs := flag.NewFlagSet("repo sync", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/repos/"+*id+"/sync", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		req.Header.Set("Content-Type", "application/json")
		return doRequest(client, stdout, stderr, req)
	case "remove":
		fs := flag.NewFlagSet("repo remove", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/v1/repos/"+*id, nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown repo command")
		return 1
	}
}

func runAnchor(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl anchor <list|get|create|update|close|reopen|resolve>")
		return 1
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("anchor list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		repoID := fs.String("repo-id", "", "")
		path := fs.String("path", "", "")
		symbol := fs.String("symbol", "", "")
		status := fs.String("status", "", "")
		limit := fs.Int("limit", 0, "")
		offset := fs.Int("offset", 0, "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		query := url.Values{}
		if *repoID != "" {
			query.Set("repo_id", *repoID)
		}
		if *path != "" {
			query.Set("path", *path)
		}
		if *symbol != "" {
			query.Set("symbol", *symbol)
		}
		if *status != "" {
			query.Set("status", *status)
		}
		if *limit > 0 {
			query.Set("limit", fmt.Sprintf("%d", *limit))
		}
		if *offset > 0 {
			query.Set("offset", fmt.Sprintf("%d", *offset))
		}
		endpoint := baseURL + "/v1/anchors"
		if encoded := query.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
	case "get":
		fs := flag.NewFlagSet("anchor get", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/anchors/"+*id, nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
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
		symbol := fs.String("symbol", "", "")
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
			"symbol":     *symbol,
		}
		return doJSONRequest(ctx, client, stdout, stderr, http.MethodPost, baseURL+"/v1/anchors", payload)
	case "update":
		fs := flag.NewFlagSet("anchor update", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		kind := fs.String("kind", "", "")
		title := fs.String("title", "", "")
		body := fs.String("body", "", "")
		author := fs.String("author", "", "")
		tags := fs.String("tags", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		payload := map[string]any{}
		if *kind != "" {
			payload["kind"] = *kind
		}
		if *title != "" {
			payload["title"] = *title
		}
		if *body != "" {
			payload["body"] = *body
		}
		if *author != "" {
			payload["author"] = *author
		}
		if *tags != "" {
			payload["tags"] = splitCSV(*tags)
		}
		return doJSONRequest(ctx, client, stdout, stderr, http.MethodPatch, baseURL+"/v1/anchors/"+*id, payload)
	case "close", "reopen", "resolve":
		fs := flag.NewFlagSet("anchor "+args[0], flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/anchors/"+*id+"/"+args[0], bytes.NewReader([]byte(`{}`)))
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		req.Header.Set("Content-Type", "application/json")
		return doRequest(client, stdout, stderr, req)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown anchor command")
		return 1
	}
}

func runContext(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoID := fs.String("repo-id", "", "")
	ref := fs.String("ref", "WORKTREE", "")
	path := fs.String("path", "", "")
	symbol := fs.String("symbol", "", "")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	query := url.Values{}
	query.Set("repo_id", *repoID)
	query.Set("ref", *ref)
	query.Set("path", *path)
	if *symbol != "" {
		query.Set("symbol", *symbol)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/context?"+query.Encode(), nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return doRequest(client, stdout, stderr, req)
}

func runComment(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: anchorctl comment <list|add>")
		return 1
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("comment list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		anchorID := fs.String("anchor-id", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/anchors/"+*anchorID+"/comments", nil)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return doRequest(client, stdout, stderr, req)
	case "add":
		fs := flag.NewFlagSet("comment add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		anchorID := fs.String("anchor-id", "", "")
		parentID := fs.String("parent-id", "", "")
		author := fs.String("author", "human://local", "")
		body := fs.String("body", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		payload := map[string]any{
			"parent_id": *parentID,
			"author":    *author,
			"body":      *body,
		}
		return doJSONRequest(ctx, client, stdout, stderr, http.MethodPost, baseURL+"/v1/anchors/"+*anchorID+"/comments", payload)
	default:
		_, _ = fmt.Fprintln(stderr, "unknown comment command")
		return 1
	}
}

func runSearch(ctx context.Context, client *http.Client, stdout, stderr io.Writer, args []string, baseURL string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	queryText := fs.String("query", "", "")
	repoID := fs.String("repo-id", "", "")
	path := fs.String("path", "", "")
	symbol := fs.String("symbol", "", "")
	kind := fs.String("kind", "", "")
	limit := fs.Int("limit", 0, "")
	offset := fs.Int("offset", 0, "")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	query := url.Values{}
	query.Set("query", *queryText)
	if *repoID != "" {
		query.Set("repo_id", *repoID)
	}
	if *path != "" {
		query.Set("path", *path)
	}
	if *symbol != "" {
		query.Set("symbol", *symbol)
	}
	if *kind != "" {
		query.Set("kind", *kind)
	}
	if *limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", *limit))
	}
	if *offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", *offset))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/search?"+query.Encode(), nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return doRequest(client, stdout, stderr, req)
}

func doRequest(client *http.Client, stdout, stderr io.Writer, req *http.Request) int {
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
		message := apiError(body)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		if message == "" {
			message = resp.Status
		}
		_, _ = fmt.Fprintf(stderr, "error: %s\n", message)
		return 1
	}
	formatted := prettyJSON(body)
	_, _ = stdout.Write(formatted)
	return 0
}

func apiError(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Error)
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
	return doRequest(client, stdout, stderr, req)
}

func prettyJSON(input []byte) []byte {
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return input
	}
	output, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return input
	}
	output = append(output, '\n')
	return output
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
