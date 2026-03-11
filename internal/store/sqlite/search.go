package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

func (s *Store) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchHit, error) {
	stmt := `select doc_type, doc_id, anchor_id, comment_id, repo_id, path, symbol, kind, title, body, bm25(search_index) as score from search_index where search_index match ?`
	args := make([]any, 0, 7)
	args = append(args, query.Query)
	if query.RepoID != "" {
		stmt += ` and repo_id = ?`
		args = append(args, query.RepoID)
	}
	if query.Path != "" {
		stmt += ` and path = ?`
		args = append(args, query.Path)
	}
	if query.SymbolPath != "" {
		stmt += ` and symbol = ?`
		args = append(args, query.SymbolPath)
	}
	if query.Kind != "" {
		stmt += ` and kind = ?`
		args = append(args, query.Kind)
	}
	stmt += ` order by score`
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	stmt += ` limit ?`
	args = append(args, limit)
	if query.Offset > 0 {
		stmt += ` offset ?`
		args = append(args, query.Offset)
	}

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []domain.SearchHit
	for rows.Next() {
		var hit domain.SearchHit
		var kind string
		if err := rows.Scan(&hit.DocumentType, &hit.DocumentID, &hit.AnchorID, &hit.CommentID, &hit.RepoID, &hit.Path, &hit.SymbolPath, &kind, &hit.Title, &hit.Body, &hit.Score); err != nil {
			return nil, err
		}
		if kind != "" {
			hit.Kind = domain.AnchorKind(kind)
		}
		hit.Snippet = buildSnippet(hit.Title, hit.Body)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func (s *Store) rebuildSearchIndex(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `delete from search_index`); err != nil {
		return err
	}
	anchors, err := s.ListAnchors(ctx, domain.AnchorFilter{})
	if err != nil {
		return err
	}
	for _, anchor := range anchors {
		if err := s.upsertAnchorSearch(ctx, anchor); err != nil {
			return err
		}
		comments, err := s.ListComments(ctx, anchor.ID)
		if err != nil {
			return err
		}
		for _, comment := range comments {
			if err := s.upsertCommentSearchWithAnchor(ctx, comment, anchor); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) searchIndexNeedsRebuild(ctx context.Context) (bool, error) {
	checks := []string{
		`select exists(
			select 1
			from anchors
			where not exists (
				select 1
				from search_index
				where doc_type = 'anchor'
					and doc_id = 'anchor:' || anchors.id
					and anchor_id = anchors.id
			)
		)`,
		`select exists(
			select 1
			from comments
			where not exists (
				select 1
				from search_index
				where doc_type = 'comment'
					and doc_id = 'comment:' || comments.id
					and comment_id = comments.id
			)
		)`,
		`select exists(
			select 1
			from search_index
			where doc_type = 'anchor'
				and not exists (
					select 1
					from anchors
					where anchors.id = search_index.anchor_id
						and search_index.doc_id = 'anchor:' || anchors.id
				)
		)`,
		`select exists(
			select 1
			from search_index
			where doc_type = 'comment'
				and not exists (
					select 1
					from comments
					where comments.id = search_index.comment_id
						and search_index.doc_id = 'comment:' || comments.id
				)
		)`,
		`select exists(
			select 1
			from search_index
			where doc_type not in ('anchor', 'comment')
		)`,
		`select exists(
			select 1
			from search_index
			group by doc_id
			having count(*) > 1
		)`,
	}
	for _, check := range checks {
		var needsRebuild bool
		if err := s.db.QueryRowContext(ctx, check).Scan(&needsRebuild); err != nil {
			return false, err
		}
		if needsRebuild {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) upsertAnchorSearch(ctx context.Context, anchor domain.Anchor) error {
	if _, err := s.db.ExecContext(ctx, `delete from search_index where doc_id = ?`, "anchor:"+anchor.ID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		`insert into search_index (doc_type, doc_id, anchor_id, comment_id, repo_id, path, symbol, kind, title, body) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"anchor",
		"anchor:"+anchor.ID,
		anchor.ID,
		"",
		anchor.RepoID,
		anchor.Binding.Path,
		anchor.Binding.SymbolPath,
		string(anchor.Kind),
		anchor.Title,
		anchor.Body,
	)
	return err
}

func (s *Store) upsertCommentSearch(ctx context.Context, comment domain.Comment) error {
	anchor, err := s.GetAnchor(ctx, comment.AnchorID)
	if err != nil {
		return err
	}
	return s.upsertCommentSearchWithAnchor(ctx, comment, anchor)
}

func (s *Store) upsertCommentSearchWithAnchor(ctx context.Context, comment domain.Comment, anchor domain.Anchor) error {
	if _, err := s.db.ExecContext(ctx, `delete from search_index where doc_id = ?`, "comment:"+comment.ID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(
		ctx,
		`insert into search_index (doc_type, doc_id, anchor_id, comment_id, repo_id, path, symbol, kind, title, body) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"comment",
		"comment:"+comment.ID,
		anchor.ID,
		comment.ID,
		anchor.RepoID,
		anchor.Binding.Path,
		anchor.Binding.SymbolPath,
		string(anchor.Kind),
		anchor.Title,
		comment.Body,
	)
	return err
}

func (s *Store) reindexCommentsForAnchor(ctx context.Context, anchorID string) error {
	anchor, err := s.GetAnchor(ctx, anchorID)
	if err != nil {
		return err
	}
	comments, err := s.ListComments(ctx, anchorID)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if err := s.upsertCommentSearchWithAnchor(ctx, comment, anchor); err != nil {
			return err
		}
	}
	return nil
}

func buildSnippet(title, body string) string {
	source := strings.TrimSpace(body)
	if source == "" {
		source = strings.TrimSpace(title)
	}
	if len(source) <= 160 {
		return source
	}
	return fmt.Sprintf("%s...", source[:157])
}
