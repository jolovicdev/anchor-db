package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"anchordb/internal/app"
	"anchordb/internal/domain"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Service interface {
	RegisterRepo(context.Context, string, string) (domain.Repo, error)
	ListRepos(context.Context) ([]domain.Repo, error)
	GetRepo(context.Context, string) (domain.Repo, error)
	SyncRepo(context.Context, string) (domain.Repo, error)
	RemoveRepo(context.Context, string) error
	Context(context.Context, app.ContextRequest) (app.ContextResponse, error)
	CreateAnchor(context.Context, app.CreateAnchorInput) (domain.Anchor, error)
	UpdateAnchor(context.Context, app.UpdateAnchorInput) (domain.Anchor, error)
	CloseAnchor(context.Context, string) (domain.Anchor, error)
	ReopenAnchor(context.Context, string) (domain.Anchor, error)
	ResolveAnchor(context.Context, string) (domain.Anchor, error)
	ListAnchors(context.Context, domain.AnchorFilter) ([]domain.Anchor, error)
	GetAnchor(context.Context, string) (domain.Anchor, error)
	CreateComment(context.Context, string, string, string, string) (domain.Comment, error)
	ListComments(context.Context, string) ([]domain.Comment, error)
	FileView(context.Context, string, string, string) (app.FileView, error)
	Search(context.Context, domain.SearchQuery) ([]domain.SearchHit, error)
}

type API struct {
	service Service
}

func New(service Service) *mcp.Server {
	api := &API{service: service}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "anchordb-mcp",
		Title:   "AnchorDB MCP",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use AnchorDB to read file-linked context and write durable code notes.",
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "repo_add",
		Description: "Register a local Git repository with AnchorDB.",
	}, api.repoAdd)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_repos",
		Description: "List repos registered in AnchorDB.",
	}, api.anchorRepos)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "repo_get",
		Description: "Read a single registered repo by ID.",
	}, api.repoGet)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "repo_sync",
		Description: "Refresh a repo's HEAD metadata and re-resolve active anchors.",
	}, api.repoSync)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "repo_remove",
		Description: "Remove a repo and all of its anchors from AnchorDB.",
	}, api.repoRemove)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_context",
		Description: "Read AnchorDB context for a repo file or symbol before editing.",
	}, api.anchorContext)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_create",
		Description: "Create a new AnchorDB anchor attached to a file range.",
	}, api.anchorCreate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_update",
		Description: "Update anchor metadata such as kind, title, body, author, or tags.",
	}, api.anchorUpdate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_close",
		Description: "Archive an anchor without deleting it.",
	}, api.anchorClose)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_reopen",
		Description: "Reopen an archived anchor.",
	}, api.anchorReopen)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_resolve",
		Description: "Re-run anchor resolution against the current file contents.",
	}, api.anchorResolve)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_comment",
		Description: "Add a threaded comment to an existing AnchorDB anchor.",
	}, api.anchorComment)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_search",
		Description: "Search AnchorDB anchors by repo, path, symbol, status, and pagination.",
	}, api.anchorSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_text_search",
		Description: "Full-text search over anchor bodies and comments.",
	}, api.anchorTextSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_get",
		Description: "Read a single AnchorDB anchor by ID.",
	}, api.anchorGet)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_comments",
		Description: "List threaded comments for a single AnchorDB anchor.",
	}, api.anchorComments)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "anchor_file_view",
		Description: "Read file view data with anchors, comments, and diff metadata.",
	}, api.anchorFileView)

	server.AddResource(&mcp.Resource{
		Name:        "repos",
		Title:       "AnchorDB Repos",
		Description: "List of repos registered in AnchorDB.",
		MIMEType:    "application/json",
		URI:         "anchordb://repos",
	}, api.readRepos)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "repo",
		Title:       "Repo Detail",
		Description: "Repo metadata for a single registered repo.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://repo/{repo_id}",
	}, api.readRepo)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "context",
		Title:       "File Context",
		Description: "Context lookup for a repo file or symbol.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://context/{repo_id}{?ref,path,symbol}",
	}, api.readContext)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "search",
		Title:       "Full Text Search",
		Description: "Full-text search results over anchors and comments.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://search{?query,repo_id,path,symbol,kind,limit,offset}",
	}, api.readSearch)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "anchors",
		Title:       "Repo Anchors",
		Description: "Anchors for a repo with optional path/symbol/status filters.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://anchors/{repo_id}{?path,symbol,status,limit,offset}",
	}, api.readAnchors)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "file-view",
		Title:       "File View",
		Description: "Code file view with anchors, comments, and diff metadata.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://file/{repo_id}{?ref,path}",
	}, api.readFileView)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "anchor",
		Title:       "Anchor Detail",
		Description: "Full AnchorDB anchor record by anchor ID.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://anchor/{anchor_id}",
	}, api.readAnchor)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "comments",
		Title:       "Anchor Comments",
		Description: "Threaded comments for a single AnchorDB anchor.",
		MIMEType:    "application/json",
		URITemplate: "anchordb://comments/{anchor_id}",
	}, api.readComments)

	return server
}

type contextInput struct {
	RepoID string `json:"repo_id" jsonschema:"AnchorDB repo ID."`
	Ref    string `json:"ref" jsonschema:"Git ref, branch, or WORKTREE."`
	Path   string `json:"path" jsonschema:"Repo-relative file path."`
	Symbol string `json:"symbol,omitempty" jsonschema:"Optional symbol path."`
}

type repoCreateInput struct {
	Name string `json:"name" jsonschema:"Optional display name for the repo."`
	Path string `json:"path" jsonschema:"Absolute or relative path to the local Git repo."`
}

type repoIDInput struct {
	RepoID string `json:"repo_id" jsonschema:"AnchorDB repo ID."`
}

type reposOutput struct {
	Repos []domain.Repo `json:"repos"`
}

type createAnchorInput struct {
	RepoID    string   `json:"repo_id" jsonschema:"AnchorDB repo ID."`
	Ref       string   `json:"ref" jsonschema:"Git ref, branch, or WORKTREE."`
	Path      string   `json:"path" jsonschema:"Repo-relative file path."`
	StartLine int      `json:"start_line" jsonschema:"1-based starting line."`
	StartCol  int      `json:"start_col" jsonschema:"1-based starting column."`
	EndLine   int      `json:"end_line" jsonschema:"1-based ending line."`
	EndCol    int      `json:"end_col" jsonschema:"1-based ending column."`
	Kind      string   `json:"kind" jsonschema:"warning|todo|handoff|rationale|invariant|question."`
	Title     string   `json:"title" jsonschema:"Short anchor title."`
	Body      string   `json:"body" jsonschema:"Main anchor body."`
	Author    string   `json:"author" jsonschema:"Actor identity, e.g. agent://codex."`
	Tags      []string `json:"tags,omitempty" jsonschema:"Optional tag list."`
	Symbol    string   `json:"symbol,omitempty" jsonschema:"Optional explicit symbol path."`
}

type anchorIDInput struct {
	AnchorID string `json:"anchor_id" jsonschema:"AnchorDB anchor ID."`
}

type anchorUpdateInput struct {
	AnchorID string   `json:"anchor_id" jsonschema:"AnchorDB anchor ID."`
	Kind     string   `json:"kind,omitempty" jsonschema:"Optional new kind."`
	Title    string   `json:"title,omitempty" jsonschema:"Optional new title."`
	Body     string   `json:"body,omitempty" jsonschema:"Optional new body."`
	Author   string   `json:"author,omitempty" jsonschema:"Optional new author."`
	Tags     []string `json:"tags,omitempty" jsonschema:"Optional replacement tag list."`
}

type commentInput struct {
	AnchorID string `json:"anchor_id" jsonschema:"Anchor ID to comment on."`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Optional parent comment ID."`
	Author   string `json:"author" jsonschema:"Actor identity."`
	Body     string `json:"body" jsonschema:"Comment body."`
}

type searchInput struct {
	RepoID string `json:"repo_id,omitempty" jsonschema:"Optional repo ID."`
	Path   string `json:"path,omitempty" jsonschema:"Optional repo-relative file path."`
	Symbol string `json:"symbol,omitempty" jsonschema:"Optional symbol path."`
	Status string `json:"status,omitempty" jsonschema:"Optional anchor status."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional page size."`
	Offset int    `json:"offset,omitempty" jsonschema:"Optional offset."`
}

type searchOutput struct {
	Anchors []domain.Anchor `json:"anchors"`
}

type textSearchInput struct {
	Query  string `json:"query" jsonschema:"Search query text."`
	RepoID string `json:"repo_id,omitempty" jsonschema:"Optional repo ID."`
	Path   string `json:"path,omitempty" jsonschema:"Optional repo-relative file path."`
	Symbol string `json:"symbol,omitempty" jsonschema:"Optional symbol path."`
	Kind   string `json:"kind,omitempty" jsonschema:"Optional anchor kind."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional page size."`
	Offset int    `json:"offset,omitempty" jsonschema:"Optional offset."`
}

type textSearchOutput struct {
	Hits []domain.SearchHit `json:"hits"`
}

type commentsOutput struct {
	Comments []domain.Comment `json:"comments"`
}

type fileViewInput struct {
	RepoID string `json:"repo_id" jsonschema:"AnchorDB repo ID."`
	Ref    string `json:"ref" jsonschema:"Git ref, branch, or WORKTREE."`
	Path   string `json:"path" jsonschema:"Repo-relative file path."`
}

type deletedOutput struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

func (a *API) repoAdd(_ context.Context, _ *mcp.CallToolRequest, input repoCreateInput) (*mcp.CallToolResult, domain.Repo, error) {
	repo, err := a.service.RegisterRepo(context.Background(), input.Name, input.Path)
	return nil, repo, err
}

func (a *API) anchorContext(_ context.Context, _ *mcp.CallToolRequest, input contextInput) (*mcp.CallToolResult, app.ContextResponse, error) {
	result, err := a.service.Context(context.Background(), app.ContextRequest{
		RepoID: input.RepoID,
		Ref:    input.Ref,
		Path:   input.Path,
		Symbol: input.Symbol,
	})
	return nil, result, err
}

func (a *API) anchorRepos(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, reposOutput, error) {
	repos, err := a.service.ListRepos(context.Background())
	return nil, reposOutput{Repos: repos}, err
}

func (a *API) repoGet(_ context.Context, _ *mcp.CallToolRequest, input repoIDInput) (*mcp.CallToolResult, domain.Repo, error) {
	repo, err := a.service.GetRepo(context.Background(), input.RepoID)
	return nil, repo, err
}

func (a *API) repoSync(_ context.Context, _ *mcp.CallToolRequest, input repoIDInput) (*mcp.CallToolResult, domain.Repo, error) {
	repo, err := a.service.SyncRepo(context.Background(), input.RepoID)
	return nil, repo, err
}

func (a *API) repoRemove(_ context.Context, _ *mcp.CallToolRequest, input repoIDInput) (*mcp.CallToolResult, deletedOutput, error) {
	err := a.service.RemoveRepo(context.Background(), input.RepoID)
	return nil, deletedOutput{Deleted: err == nil, ID: input.RepoID}, err
}

func (a *API) anchorCreate(_ context.Context, _ *mcp.CallToolRequest, input createAnchorInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.CreateAnchor(context.Background(), app.CreateAnchorInput{
		RepoID:    input.RepoID,
		Ref:       input.Ref,
		Path:      input.Path,
		StartLine: input.StartLine,
		StartCol:  input.StartCol,
		EndLine:   input.EndLine,
		EndCol:    input.EndCol,
		Kind:      input.Kind,
		Title:     input.Title,
		Body:      input.Body,
		Author:    input.Author,
		Tags:      input.Tags,
		Symbol:    input.Symbol,
	})
	return nil, anchor, err
}

func (a *API) anchorUpdate(_ context.Context, _ *mcp.CallToolRequest, input anchorUpdateInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.UpdateAnchor(context.Background(), app.UpdateAnchorInput{
		ID:          input.AnchorID,
		Kind:        input.Kind,
		Title:       input.Title,
		Body:        input.Body,
		Author:      input.Author,
		Tags:        input.Tags,
		ReplaceTags: input.Tags != nil,
	})
	return nil, anchor, err
}

func (a *API) anchorClose(_ context.Context, _ *mcp.CallToolRequest, input anchorIDInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.CloseAnchor(context.Background(), input.AnchorID)
	return nil, anchor, err
}

func (a *API) anchorReopen(_ context.Context, _ *mcp.CallToolRequest, input anchorIDInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.ReopenAnchor(context.Background(), input.AnchorID)
	return nil, anchor, err
}

func (a *API) anchorResolve(_ context.Context, _ *mcp.CallToolRequest, input anchorIDInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.ResolveAnchor(context.Background(), input.AnchorID)
	return nil, anchor, err
}

func (a *API) anchorComment(_ context.Context, _ *mcp.CallToolRequest, input commentInput) (*mcp.CallToolResult, domain.Comment, error) {
	comment, err := a.service.CreateComment(context.Background(), input.AnchorID, input.ParentID, input.Author, input.Body)
	return nil, comment, err
}

func (a *API) anchorSearch(_ context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, searchOutput, error) {
	filter := domain.AnchorFilter{
		RepoID:     input.RepoID,
		Path:       input.Path,
		SymbolPath: input.Symbol,
		Limit:      input.Limit,
		Offset:     input.Offset,
	}
	if input.Status != "" {
		filter.Status = domain.AnchorStatus(input.Status)
	}
	anchors, err := a.service.ListAnchors(context.Background(), filter)
	return nil, searchOutput{Anchors: anchors}, err
}

func (a *API) anchorTextSearch(_ context.Context, _ *mcp.CallToolRequest, input textSearchInput) (*mcp.CallToolResult, textSearchOutput, error) {
	query := domain.SearchQuery{
		Query:      input.Query,
		RepoID:     input.RepoID,
		Path:       input.Path,
		SymbolPath: input.Symbol,
		Limit:      input.Limit,
		Offset:     input.Offset,
	}
	if input.Kind != "" {
		query.Kind = domain.AnchorKind(input.Kind)
	}
	hits, err := a.service.Search(context.Background(), query)
	return nil, textSearchOutput{Hits: hits}, err
}

func (a *API) anchorGet(_ context.Context, _ *mcp.CallToolRequest, input anchorIDInput) (*mcp.CallToolResult, domain.Anchor, error) {
	anchor, err := a.service.GetAnchor(context.Background(), input.AnchorID)
	return nil, anchor, err
}

func (a *API) anchorComments(_ context.Context, _ *mcp.CallToolRequest, input anchorIDInput) (*mcp.CallToolResult, commentsOutput, error) {
	comments, err := a.service.ListComments(context.Background(), input.AnchorID)
	return nil, commentsOutput{Comments: comments}, err
}

func (a *API) anchorFileView(_ context.Context, _ *mcp.CallToolRequest, input fileViewInput) (*mcp.CallToolResult, app.FileView, error) {
	view, err := a.service.FileView(context.Background(), input.RepoID, input.Ref, input.Path)
	return nil, view, err
}

func (a *API) readRepos(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	repos, err := a.service.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	return jsonResource("anchordb://repos", repos)
}

func (a *API) readRepo(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	repoID := strings.TrimPrefix(parsed.Path, "/")
	repo, err := a.service.GetRepo(ctx, repoID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}
		return nil, err
	}
	return jsonResource(request.Params.URI, repo)
}

func (a *API) readContext(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	repoID := strings.TrimPrefix(parsed.Path, "/")
	result, err := a.service.Context(ctx, app.ContextRequest{
		RepoID: repoID,
		Ref:    parsed.Query().Get("ref"),
		Path:   parsed.Query().Get("path"),
		Symbol: parsed.Query().Get("symbol"),
	})
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, result)
}

func (a *API) readAnchors(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	repoID := strings.TrimPrefix(parsed.Path, "/")
	filter := domain.AnchorFilter{
		RepoID:     repoID,
		Path:       parsed.Query().Get("path"),
		SymbolPath: parsed.Query().Get("symbol"),
	}
	if status := parsed.Query().Get("status"); status != "" {
		filter.Status = domain.AnchorStatus(status)
	}
	filter.Limit = atoi(parsed.Query().Get("limit"))
	filter.Offset = atoi(parsed.Query().Get("offset"))
	anchors, err := a.service.ListAnchors(ctx, filter)
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, anchors)
}

func (a *API) readSearch(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	query := domain.SearchQuery{
		Query:      parsed.Query().Get("query"),
		RepoID:     parsed.Query().Get("repo_id"),
		Path:       parsed.Query().Get("path"),
		SymbolPath: parsed.Query().Get("symbol"),
		Limit:      atoi(parsed.Query().Get("limit")),
		Offset:     atoi(parsed.Query().Get("offset")),
	}
	if kind := parsed.Query().Get("kind"); kind != "" {
		query.Kind = domain.AnchorKind(kind)
	}
	hits, err := a.service.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, hits)
}

func (a *API) readFileView(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	repoID := strings.TrimPrefix(parsed.Path, "/")
	view, err := a.service.FileView(ctx, repoID, parsed.Query().Get("ref"), parsed.Query().Get("path"))
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, view)
}

func (a *API) readAnchor(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	anchorID := strings.TrimPrefix(parsed.Path, "/")
	anchor, err := a.service.GetAnchor(ctx, anchorID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, mcp.ResourceNotFoundError(request.Params.URI)
		}
		return nil, err
	}
	return jsonResource(request.Params.URI, anchor)
}

func (a *API) readComments(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	anchorID := strings.TrimPrefix(parsed.Path, "/")
	comments, err := a.service.ListComments(ctx, anchorID)
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, comments)
}

func jsonResource(uri string, value any) (*mcp.ReadResourceResult, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(encoded),
			},
		},
	}, nil
}

func atoi(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func ExampleConfig(binaryPath, dbPath string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "anchordb": {
      "command": %q,
      "args": ["--db", %q]
    }
  }
}`, binaryPath, dbPath)
}
