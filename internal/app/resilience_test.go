package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/app"
	"github.com/jolovicdev/anchor-db/internal/domain"
	sqlitestore "github.com/jolovicdev/anchor-db/internal/store/sqlite"
)

func newTestService(t *testing.T) (*app.Service, string) {
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
	return svc, repoRoot
}

func sampleAnchor(t *testing.T, svc *app.Service, repoID string, overrides func(*app.CreateAnchorInput)) (domain.Anchor, error) {
	t.Helper()
	input := app.CreateAnchorInput{
		RepoID:    repoID,
		Ref:       "WORKTREE",
		Path:      "sample.go",
		StartLine: 5,
		StartCol:  1,
		EndLine:   7,
		EndCol:    2,
		Kind:      "warning",
		Title:     "Retry invariant",
		Body:      "Do not break idempotency here.",
		Author:    "agent://planner",
	}
	if overrides != nil {
		overrides(&input)
	}
	return svc.CreateAnchor(context.Background(), input)
}

// Deleting a file used to abort SyncRepo for the entire repo. Its anchors
// should go stale while every other anchor still resolves.
func TestSyncMarksAnchorsStaleWhenFileIsDeleted(t *testing.T) {
	ctx := context.Background()
	svc, repoRoot := newTestService(t)
	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}

	other := filepath.Join(repoRoot, "other.go")
	if err := os.WriteFile(other, []byte("package sample\n\nfunc Keep() int {\n\treturn 3\n}\n"), 0o644); err != nil {
		t.Fatalf("write other.go: %v", err)
	}

	doomed, err := sampleAnchor(t, svc, repo.ID, nil)
	if err != nil {
		t.Fatalf("create doomed anchor: %v", err)
	}
	survivor, err := sampleAnchor(t, svc, repo.ID, func(in *app.CreateAnchorInput) {
		in.Path = "other.go"
		in.StartLine, in.StartCol, in.EndLine, in.EndCol = 3, 1, 5, 2
		in.Title = "Keeps working"
	})
	if err != nil {
		t.Fatalf("create survivor anchor: %v", err)
	}

	if err := os.Remove(filepath.Join(repoRoot, "sample.go")); err != nil {
		t.Fatalf("remove sample.go: %v", err)
	}

	if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
		t.Fatalf("SyncRepo after deleting a file: %v", err)
	}

	stale, err := svc.GetAnchor(ctx, doomed.ID)
	if err != nil {
		t.Fatalf("get doomed anchor: %v", err)
	}
	if stale.Status != domain.AnchorStatusStale {
		t.Errorf("anchor on deleted file: status = %q, want %q", stale.Status, domain.AnchorStatusStale)
	}
	// The last known position is preserved so a human can still find it.
	if stale.Binding.Path != "sample.go" || stale.Binding.StartLine != 5 {
		t.Errorf("stale anchor lost its recorded position: %+v", stale.Binding)
	}

	kept, err := svc.GetAnchor(ctx, survivor.ID)
	if err != nil {
		t.Fatalf("get survivor anchor: %v", err)
	}
	if kept.Status != domain.AnchorStatusActive {
		t.Errorf("unrelated anchor: status = %q, want %q", kept.Status, domain.AnchorStatusActive)
	}
}

// A repo whose root has gone away entirely says nothing about individual
// anchors, so it must surface as an error rather than staling everything.
func TestSyncDoesNotStaleAnchorsWhenRepoRootIsMissing(t *testing.T) {
	ctx := context.Background()
	svc, repoRoot := newTestService(t)
	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}
	anchor, err := sampleAnchor(t, svc, repo.ID, nil)
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	if err := os.RemoveAll(repoRoot); err != nil {
		t.Fatalf("remove repo root: %v", err)
	}

	if _, err := svc.SyncRepo(ctx, repo.ID); err == nil {
		t.Error("SyncRepo with a missing repo root returned no error")
	}
	got, err := svc.GetAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("get anchor: %v", err)
	}
	if got.Status != domain.AnchorStatusActive {
		t.Errorf("anchor status = %q, want %q kept while the repo root is unavailable",
			got.Status, domain.AnchorStatusActive)
	}
}

func TestCreateAnchorRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	svc, repoRoot := newTestService(t)
	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}

	cases := map[string]func(*app.CreateAnchorInput){
		// A zero-width span produced an anchor with no selected text, which
		// could never be resolved again.
		"zero width span": func(in *app.CreateAnchorInput) {
			in.StartLine, in.StartCol, in.EndLine, in.EndCol = 1, 1, 1, 1
		},
		"empty title":         func(in *app.CreateAnchorInput) { in.Title = "  " },
		"empty body":          func(in *app.CreateAnchorInput) { in.Body = "" },
		"empty author":        func(in *app.CreateAnchorInput) { in.Author = "" },
		"end before start":    func(in *app.CreateAnchorInput) { in.EndLine = 2 },
		"non positive line":   func(in *app.CreateAnchorInput) { in.StartLine = 0 },
		"non positive column": func(in *app.CreateAnchorInput) { in.StartCol = 0 },
		"missing path":        func(in *app.CreateAnchorInput) { in.Path = "" },
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := sampleAnchor(t, svc, repo.ID, override); err == nil {
				t.Fatalf("CreateAnchor succeeded, want rejection")
			}
		})
	}
}

// Anchors created before validation existed still live in user databases, so
// resolution has to tolerate them rather than failing the whole pass.
func TestResolveToleratesAnchorWithoutSelectedText(t *testing.T) {
	ctx := context.Background()
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
	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}

	// Write the legacy-shaped record straight through the store, bypassing the
	// service validation that now prevents creating one.
	legacy, err := store.CreateAnchor(ctx, domain.Anchor{
		ID:     domain.NewID("anchor"),
		RepoID: repo.ID,
		Kind:   domain.AnchorKindWarning,
		Status: domain.AnchorStatusActive,
		Title:  "legacy",
		Body:   "legacy",
		Author: "legacy",
		Binding: domain.Binding{
			Type: domain.BindingTypeSpan, Ref: "WORKTREE", Path: "sample.go",
			StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 1,
		},
	})
	if err != nil {
		t.Fatalf("seed legacy anchor: %v", err)
	}

	if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
		t.Fatalf("SyncRepo with a legacy anchor: %v", err)
	}
	got, err := svc.GetAnchor(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("get legacy anchor: %v", err)
	}
	if got.Status != domain.AnchorStatusStale {
		t.Errorf("legacy anchor: status = %q, want %q", got.Status, domain.AnchorStatusStale)
	}
}

// Resolution runs on a timer; an unchanged anchor must not append an event
// every pass or the history grows without bound.
func TestRepeatedSyncDoesNotAccumulateEvents(t *testing.T) {
	ctx := context.Background()
	svc, repoRoot := newTestService(t)
	repo, err := svc.RegisterRepo(ctx, "demo", repoRoot)
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}
	anchor, err := sampleAnchor(t, svc, repo.ID, nil)
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	for range 5 {
		if _, err := svc.SyncRepo(ctx, repo.ID); err != nil {
			t.Fatalf("sync: %v", err)
		}
	}

	events, err := svc.ListAnchorEvents(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		kinds := make([]string, 0, len(events))
		for _, event := range events {
			kinds = append(kinds, string(event.Type))
		}
		t.Errorf("after 5 no-op syncs: %d events (%s), want only the creation event",
			len(events), strings.Join(kinds, ","))
	}
}
