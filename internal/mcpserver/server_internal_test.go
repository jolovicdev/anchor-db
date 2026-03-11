package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testService struct{}

func (testService) RegisterRepo(context.Context, string, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo", RootPath: "/tmp/demo"}, nil
}

func (testService) ListRepos(context.Context) ([]domain.Repo, error) {
	return []domain.Repo{{ID: "repo-1", Name: "demo"}}, nil
}

func (testService) GetRepo(context.Context, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo"}, nil
}

func (testService) SyncRepo(context.Context, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo", DefaultRef: "abc123"}, nil
}

func (testService) RemoveRepo(context.Context, string) error {
	return nil
}

func (testService) Context(context.Context, app.ContextRequest) (app.ContextResponse, error) {
	return app.ContextResponse{
		Repo:    domain.Repo{ID: "repo-1", Name: "demo"},
		Anchors: []domain.Anchor{{ID: "anchor-1", Title: "warning"}},
	}, nil
}

func (testService) CreateAnchor(context.Context, app.CreateAnchorInput) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "warning"}, nil
}

func (testService) UpdateAnchor(context.Context, app.UpdateAnchorInput) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "updated", Status: domain.AnchorStatusActive}, nil
}

func (testService) CloseAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusArchived}, nil
}

func (testService) ReopenAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusActive}, nil
}

func (testService) ResolveAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusActive}, nil
}

func (testService) ListAnchors(context.Context, domain.AnchorFilter) ([]domain.Anchor, error) {
	return []domain.Anchor{{ID: "anchor-1", Title: "warning"}}, nil
}

func (testService) GetAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "warning"}, nil
}

func (testService) CreateComment(context.Context, string, string, string, string) (domain.Comment, error) {
	return domain.Comment{ID: "comment-1", Body: "note"}, nil
}

func (testService) ListComments(context.Context, string) ([]domain.Comment, error) {
	return []domain.Comment{{ID: "comment-1", Body: "note"}}, nil
}

func (testService) FileView(context.Context, string, string, string) (app.FileView, error) {
	return app.FileView{Repo: domain.Repo{ID: "repo-1"}, Path: "main.go", Content: "package main\n"}, nil
}

func (testService) Search(context.Context, domain.SearchQuery) ([]domain.SearchHit, error) {
	return []domain.SearchHit{{DocumentType: "anchor", AnchorID: "anchor-1", Snippet: "retry note"}}, nil
}

func TestReadContextResource(t *testing.T) {
	api := &API{service: testService{}}
	result, err := api.readContext(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "anchordb://context/repo-1?ref=WORKTREE&path=main.go"},
	})
	if err != nil {
		t.Fatalf("read context: %v", err)
	}
	if len(result.Contents) != 1 || !strings.Contains(result.Contents[0].Text, "\"anchor-1\"") {
		t.Fatalf("unexpected context resource: %#v", result.Contents)
	}
}

func TestReadReposToolHelpers(t *testing.T) {
	api := &API{service: testService{}}
	_, repos, err := api.anchorRepos(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("anchor repos: %v", err)
	}
	if len(repos.Repos) != 1 || repos.Repos[0].ID != "repo-1" {
		t.Fatalf("unexpected repos output: %#v", repos)
	}

	_, anchor, err := api.anchorGet(context.Background(), nil, anchorIDInput{AnchorID: "anchor-1"})
	if err != nil {
		t.Fatalf("anchor get: %v", err)
	}
	if anchor.ID != "anchor-1" {
		t.Fatalf("unexpected anchor output: %#v", anchor)
	}

	_, comments, err := api.anchorComments(context.Background(), nil, anchorIDInput{AnchorID: "anchor-1"})
	if err != nil {
		t.Fatalf("anchor comments: %v", err)
	}
	if len(comments.Comments) != 1 {
		t.Fatalf("unexpected comments output: %#v", comments)
	}

	_, view, err := api.anchorFileView(context.Background(), nil, fileViewInput{RepoID: "repo-1", Ref: "WORKTREE", Path: "main.go"})
	if err != nil {
		t.Fatalf("anchor file view: %v", err)
	}
	if view.Path != "main.go" {
		t.Fatalf("unexpected file view output: %#v", view)
	}

	_, textSearch, err := api.anchorTextSearch(context.Background(), nil, textSearchInput{Query: "retry"})
	if err != nil {
		t.Fatalf("anchor text search: %v", err)
	}
	if len(textSearch.Hits) != 1 {
		t.Fatalf("unexpected text search output: %#v", textSearch)
	}
}

func TestReadSearchResource(t *testing.T) {
	api := &API{service: testService{}}
	result, err := api.readSearch(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "anchordb://search?query=retry&repo_id=repo-1"},
	})
	if err != nil {
		t.Fatalf("read search: %v", err)
	}
	if len(result.Contents) != 1 || !strings.Contains(result.Contents[0].Text, "\"retry note\"") {
		t.Fatalf("unexpected search resource: %#v", result.Contents)
	}
}

func TestRepoAndAnchorLifecycleTools(t *testing.T) {
	api := &API{service: testService{}}

	_, repo, err := api.repoAdd(context.Background(), nil, repoCreateInput{Name: "demo", Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("repo add: %v", err)
	}
	if repo.ID != "repo-1" {
		t.Fatalf("unexpected repo add output: %#v", repo)
	}

	_, repo, err = api.repoGet(context.Background(), nil, repoIDInput{RepoID: "repo-1"})
	if err != nil {
		t.Fatalf("repo get: %v", err)
	}
	if repo.ID != "repo-1" {
		t.Fatalf("unexpected repo get output: %#v", repo)
	}

	_, repo, err = api.repoSync(context.Background(), nil, repoIDInput{RepoID: "repo-1"})
	if err != nil {
		t.Fatalf("repo sync: %v", err)
	}
	if repo.DefaultRef != "abc123" {
		t.Fatalf("unexpected repo sync output: %#v", repo)
	}

	if _, removed, err := api.repoRemove(context.Background(), nil, repoIDInput{RepoID: "repo-1"}); err != nil {
		t.Fatalf("repo remove: %v", err)
	} else if !removed.Deleted {
		t.Fatalf("expected repo remove to report deleted")
	}

	_, anchor, err := api.anchorUpdate(context.Background(), nil, anchorUpdateInput{
		AnchorID: "anchor-1",
		Kind:     "todo",
		Title:    "updated",
		Body:     "body",
		Author:   "agent://codex",
	})
	if err != nil {
		t.Fatalf("anchor update: %v", err)
	}
	if anchor.Title != "updated" {
		t.Fatalf("unexpected anchor update output: %#v", anchor)
	}

	_, anchor, err = api.anchorClose(context.Background(), nil, anchorIDInput{AnchorID: "anchor-1"})
	if err != nil {
		t.Fatalf("anchor close: %v", err)
	}
	if anchor.Status != domain.AnchorStatusArchived {
		t.Fatalf("unexpected anchor close output: %#v", anchor)
	}

	_, anchor, err = api.anchorReopen(context.Background(), nil, anchorIDInput{AnchorID: "anchor-1"})
	if err != nil {
		t.Fatalf("anchor reopen: %v", err)
	}
	if anchor.Status != domain.AnchorStatusActive {
		t.Fatalf("unexpected anchor reopen output: %#v", anchor)
	}

	_, anchor, err = api.anchorResolve(context.Background(), nil, anchorIDInput{AnchorID: "anchor-1"})
	if err != nil {
		t.Fatalf("anchor resolve: %v", err)
	}
	if anchor.Status != domain.AnchorStatusActive {
		t.Fatalf("unexpected anchor resolve output: %#v", anchor)
	}
}
