package mcpserver_test

import (
	"context"
	"strings"
	"testing"

	"anchordb/internal/app"
	"anchordb/internal/domain"
	"anchordb/internal/mcpserver"
)

type fakeService struct{}

func (fakeService) RegisterRepo(context.Context, string, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo", RootPath: "/tmp/demo", DefaultRef: "HEAD"}, nil
}

func (fakeService) ListRepos(context.Context) ([]domain.Repo, error) {
	return []domain.Repo{{ID: "repo-1", Name: "demo", RootPath: "/tmp/demo", DefaultRef: "HEAD"}}, nil
}

func (fakeService) GetRepo(context.Context, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo", RootPath: "/tmp/demo", DefaultRef: "HEAD"}, nil
}

func (fakeService) SyncRepo(context.Context, string) (domain.Repo, error) {
	return domain.Repo{ID: "repo-1", Name: "demo", RootPath: "/tmp/demo", DefaultRef: "abc123"}, nil
}

func (fakeService) RemoveRepo(context.Context, string) error {
	return nil
}

func (fakeService) Context(context.Context, app.ContextRequest) (app.ContextResponse, error) {
	return app.ContextResponse{
		Repo: domain.Repo{ID: "repo-1", Name: "demo"},
		Anchors: []domain.Anchor{
			{ID: "anchor-1", Title: "warning", Kind: domain.AnchorKindWarning},
		},
	}, nil
}

func (fakeService) CreateAnchor(context.Context, app.CreateAnchorInput) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "warning"}, nil
}

func (fakeService) UpdateAnchor(context.Context, app.UpdateAnchorInput) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "updated", Status: domain.AnchorStatusActive}, nil
}

func (fakeService) CloseAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusArchived}, nil
}

func (fakeService) ReopenAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusActive}, nil
}

func (fakeService) ResolveAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Status: domain.AnchorStatusActive}, nil
}

func (fakeService) ListAnchors(context.Context, domain.AnchorFilter) ([]domain.Anchor, error) {
	return []domain.Anchor{{ID: "anchor-1", Title: "warning"}}, nil
}

func (fakeService) GetAnchor(context.Context, string) (domain.Anchor, error) {
	return domain.Anchor{ID: "anchor-1", Title: "warning"}, nil
}

func (fakeService) CreateComment(context.Context, string, string, string, string) (domain.Comment, error) {
	return domain.Comment{ID: "comment-1", AnchorID: "anchor-1", Body: "comment"}, nil
}

func (fakeService) ListComments(context.Context, string) ([]domain.Comment, error) {
	return []domain.Comment{{ID: "comment-1", AnchorID: "anchor-1", Body: "comment"}}, nil
}

func (fakeService) FileView(context.Context, string, string, string) (app.FileView, error) {
	return app.FileView{
		Repo:    domain.Repo{ID: "repo-1", Name: "demo"},
		Path:    "main.go",
		Content: "package main\n",
	}, nil
}

func (fakeService) Search(context.Context, domain.SearchQuery) ([]domain.SearchHit, error) {
	return []domain.SearchHit{{DocumentType: "anchor", AnchorID: "anchor-1", Snippet: "retry note"}}, nil
}

func TestExampleConfigContainsCommandAndDB(t *testing.T) {
	config := mcpserver.ExampleConfig("/usr/local/bin/anchordb-mcp", "/tmp/anchor.db")
	if !strings.Contains(config, "anchordb-mcp") {
		t.Fatalf("expected binary path in config")
	}
	if !strings.Contains(config, "/tmp/anchor.db") {
		t.Fatalf("expected db path in config")
	}
}

func TestNewReturnsServer(t *testing.T) {
	server := mcpserver.New(fakeService{})
	if server == nil {
		t.Fatalf("expected mcp server")
	}
}
