package sqlite

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

// snippetLimit caps search snippets, measured in runes.
const snippetLimit = 160

func (s *Store) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchHit, error) {
	// Callers type prose, not FTS5 expressions. Passing raw input to MATCH turns
	// ordinary queries ("C++", "don't", "retry (v2)") into syntax errors.
	terms := ftsTerms(query.Query)
	if len(terms) == 0 {
		return nil, nil
	}

	// FTS5 joins bare terms with AND, so one word that happens not to appear --
	// a synonym, a word from the question rather than the note -- returned
	// nothing at all. Requiring every term is still the better answer when it
	// finds something, so that is tried first and OR is the fallback rather than
	// the default: it keeps precise queries precise without letting a single
	// stray word blank the result.
	hits, err := s.searchMatching(ctx, query, strings.Join(terms, " "))
	if err != nil || len(hits) > 0 || len(terms) == 1 {
		return hits, err
	}
	return s.searchMatching(ctx, query, strings.Join(terms, " OR "))
}

func (s *Store) searchMatching(ctx context.Context, query domain.SearchQuery, match string) ([]domain.SearchHit, error) {
	stmt := `select doc_type, doc_id, anchor_id, comment_id, repo_id, path, symbol, kind, title, body, bm25(search_index) as score from search_index where search_index match ?`
	args := make([]any, 0, 7)
	args = append(args, match)
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
		// bm25 scores are negative, most relevant first. Reporting that outward
		// means every consumer has to know the sign convention to display or sort
		// it, so it is flipped to a plain "higher is better" relevance.
		hit.Score = -hit.Score
		if kind != "" {
			hit.Kind = domain.AnchorKind(kind)
		}
		hit.Snippet = buildSnippet(hit.Title, hit.Body)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func rebuildSearchIndex(ctx context.Context, ex executor) error {
	if _, err := ex.ExecContext(ctx, `delete from search_index`); err != nil {
		return err
	}
	anchors, err := listAnchors(ctx, ex, domain.AnchorFilter{})
	if err != nil {
		return err
	}
	for _, anchor := range anchors {
		if err := upsertAnchorSearch(ctx, ex, anchor); err != nil {
			return err
		}
		comments, err := listComments(ctx, ex, anchor.ID)
		if err != nil {
			return err
		}
		for _, comment := range comments {
			if err := upsertCommentSearchWithAnchor(ctx, ex, comment, anchor); err != nil {
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

func upsertAnchorSearch(ctx context.Context, ex executor, anchor domain.Anchor) error {
	if _, err := ex.ExecContext(ctx, `delete from search_index where doc_id = ?`, "anchor:"+anchor.ID); err != nil {
		return err
	}
	_, err := ex.ExecContext(
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

func upsertCommentSearch(ctx context.Context, ex executor, comment domain.Comment) error {
	anchor, err := getAnchor(ctx, ex, comment.AnchorID)
	if err != nil {
		return err
	}
	return upsertCommentSearchWithAnchor(ctx, ex, comment, anchor)
}

func upsertCommentSearchWithAnchor(ctx context.Context, ex executor, comment domain.Comment, anchor domain.Anchor) error {
	if _, err := ex.ExecContext(ctx, `delete from search_index where doc_id = ?`, "comment:"+comment.ID); err != nil {
		return err
	}
	_, err := ex.ExecContext(
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

// reindexCommentsForAnchor refreshes the denormalised anchor fields (path,
// symbol, kind, title) copied onto each comment's search row. It takes the
// already-updated anchor so it reflects the write in progress rather than
// re-reading a row the caller is midway through changing.
func reindexCommentsForAnchor(ctx context.Context, ex executor, anchor domain.Anchor) error {
	comments, err := listComments(ctx, ex, anchor.ID)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if err := upsertCommentSearchWithAnchor(ctx, ex, comment, anchor); err != nil {
			return err
		}
	}
	return nil
}

// ftsMatchExpression turns free-form user input into a safe FTS5 MATCH
// expression. Every whitespace-separated term becomes a quoted phrase, so FTS5
// operators and punctuation are matched literally instead of being parsed.
// A trailing '*' is preserved as a prefix search, which is the one operator
// worth keeping for an interactive search box. Terms carrying no letters or
// digits tokenize to nothing and are dropped; an input made up entirely of such
// terms yields "", which the caller treats as "no results".
func ftsTerms(input string) []string {
	terms := make([]string, 0, 8)
	for _, field := range strings.Fields(input) {
		prefix := strings.HasSuffix(field, "*")
		if prefix {
			field = strings.TrimSuffix(field, "*")
		}
		if !hasSearchableRune(field) {
			continue
		}
		term := `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
		if prefix {
			term += "*"
		}
		terms = append(terms, term)
	}
	return terms
}

func hasSearchableRune(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func buildSnippet(title, body string) string {
	source := strings.TrimSpace(body)
	if source == "" {
		source = strings.TrimSpace(title)
	}
	// Truncate on runes: slicing bytes splits multi-byte characters and emits
	// invalid UTF-8, which then fails to encode as JSON.
	if utf8.RuneCountInString(source) <= snippetLimit {
		return source
	}
	return fmt.Sprintf("%s...", string([]rune(source)[:snippetLimit-3]))
}
