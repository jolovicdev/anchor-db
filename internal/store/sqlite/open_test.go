package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

func TestOpenConfiguresPragmasAndSchemaVersion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRowContext(context.Background(), `pragma journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRowContext(context.Background(), `pragma busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("expected busy_timeout >= 5000, got %d", busyTimeout)
	}

	var schemaVersion int
	if err := store.db.QueryRowContext(context.Background(), `pragma user_version`).Scan(&schemaVersion); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if schemaVersion == 0 {
		t.Fatalf("expected schema version to be set")
	}

	if store.db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("expected max open connections 1, got %d", store.db.Stats().MaxOpenConnections)
	}
}

func TestOpenMigratesLegacySchemaToCascadeRepoDeletes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "anchors.db")
	createLegacyStore(t, path)

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `delete from repos where id = ?`, "repo-1"); err != nil {
		t.Fatalf("delete repo: %v", err)
	}

	assertCount(t, store.db, `select count(*) from repos`, 0)
	assertCount(t, store.db, `select count(*) from anchors`, 0)
	assertCount(t, store.db, `select count(*) from anchor_events`, 0)
	assertCount(t, store.db, `select count(*) from comments`, 0)
	assertCount(t, store.db, `select count(*) from search_index`, 0)
}

func TestSearchIndexNeedsRebuildReportsHealthyCoverage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	seedSearchData(t, ctx, store)

	needsRebuild, err := store.searchIndexNeedsRebuild(ctx)
	if err != nil {
		t.Fatalf("check search coverage: %v", err)
	}
	if needsRebuild {
		t.Fatalf("expected healthy search coverage to skip rebuild")
	}
}

func TestSearchIndexNeedsRebuildDetectsMissingDocuments(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	anchor, _ := seedSearchData(t, ctx, store)

	if _, err := store.db.ExecContext(ctx, `delete from search_index where doc_id = ?`, "anchor:"+anchor.ID); err != nil {
		t.Fatalf("delete search row: %v", err)
	}

	needsRebuild, err := store.searchIndexNeedsRebuild(ctx)
	if err != nil {
		t.Fatalf("check search coverage: %v", err)
	}
	if !needsRebuild {
		t.Fatalf("expected missing search document to require rebuild")
	}
}

func createLegacyStore(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()

	schema := `
	create table repos (
		id text primary key,
		name text not null,
		root_path text not null,
		default_ref text not null,
		created_at timestamp not null,
		updated_at timestamp not null
	);

	create table anchors (
		id text primary key,
		repo_id text not null,
		kind text not null,
		status text not null,
		title text not null,
		body text not null,
		author text not null,
		source_ref text not null,
		tags_json text not null,
		binding_json text not null,
		created_at timestamp not null,
		updated_at timestamp not null
	);

	create table anchor_events (
		id text primary key,
		anchor_id text not null,
		type text not null,
		reason text not null,
		confidence real not null,
		from_binding_json text,
		to_binding_json text,
		created_at timestamp not null
	);

	create table comments (
		id text primary key,
		anchor_id text not null,
		parent_id text,
		author text not null,
		body text not null,
		created_at timestamp not null
	);

	create virtual table search_index using fts5(
		doc_type unindexed,
		doc_id unindexed,
		anchor_id unindexed,
		comment_id unindexed,
		repo_id unindexed,
		path unindexed,
		symbol unindexed,
		kind unindexed,
		title,
		body
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := db.Exec(`
		insert into repos (id, name, root_path, default_ref, created_at, updated_at)
		values ('repo-1', 'demo', '/tmp/demo', 'HEAD', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		insert into anchors (id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at)
		values ('anchor-1', 'repo-1', 'warning', 'active', 'title', 'body', 'agent://test', 'HEAD', '[]', '{"type":"span","ref":"HEAD","path":"sample.go","language":"go","start_line":1,"start_col":1,"end_line":1,"end_col":8,"selected_text":"package","selected_text_hash":"hash","confidence":1}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		insert into anchor_events (id, anchor_id, type, reason, confidence, created_at)
		values ('event-1', 'anchor-1', 'created', '', 1, '2026-01-01T00:00:00Z');
		insert into comments (id, anchor_id, parent_id, author, body, created_at)
		values ('comment-1', 'anchor-1', null, 'human://test', 'note', '2026-01-01T00:00:00Z');
		insert into search_index (doc_type, doc_id, anchor_id, comment_id, repo_id, path, symbol, kind, title, body)
		values
			('anchor', 'anchor:anchor-1', 'anchor-1', '', 'repo-1', 'sample.go', '', 'warning', 'title', 'body'),
			('comment', 'comment:comment-1', 'anchor-1', 'comment-1', 'repo-1', 'sample.go', '', 'warning', 'title', 'note');
		pragma user_version = 1;
	`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
}

func seedSearchData(t *testing.T, ctx context.Context, store *Store) (domain.Anchor, domain.Comment) {
	t.Helper()

	repo, err := store.CreateRepo(ctx, domain.Repo{
		ID:         "repo-1",
		Name:       "demo",
		RootPath:   "/tmp/demo",
		DefaultRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	anchor, err := store.CreateAnchor(ctx, domain.Anchor{
		ID:        "anchor-1",
		RepoID:    repo.ID,
		Kind:      domain.AnchorKindWarning,
		Status:    domain.AnchorStatusActive,
		Title:     "warning",
		Body:      "watch this",
		Author:    "human://test",
		SourceRef: "HEAD",
		Binding: domain.Binding{
			Type:             domain.BindingTypeSpan,
			Ref:              "HEAD",
			Path:             "sample.go",
			Language:         "go",
			StartLine:        1,
			StartCol:         1,
			EndLine:          1,
			EndCol:           8,
			SelectedText:     "package",
			SelectedTextHash: "hash",
		},
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	comment, err := store.CreateComment(ctx, domain.Comment{
		ID:       "comment-1",
		AnchorID: anchor.ID,
		Author:   "human://test",
		Body:     "note",
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	return anchor, comment
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query count %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q: expected %d rows, got %d", query, want, got)
	}
}
