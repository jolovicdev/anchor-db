package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"anchordb/internal/domain"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateRepo(ctx context.Context, repo domain.Repo) (domain.Repo, error) {
	now := time.Now().UTC()
	if repo.ID == "" {
		repo.ID = domain.NewID("repo")
	}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	_, err := s.db.ExecContext(
		ctx,
		`insert into repos (id, name, root_path, default_ref, created_at, updated_at) values (?, ?, ?, ?, ?, ?)`,
		repo.ID,
		repo.Name,
		repo.RootPath,
		repo.DefaultRef,
		repo.CreatedAt,
		repo.UpdatedAt,
	)
	return repo, err
}

func (s *Store) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, root_path, default_ref, created_at, updated_at from repos order by created_at asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []domain.Repo
	for rows.Next() {
		var repo domain.Repo
		if err := rows.Scan(&repo.ID, &repo.Name, &repo.RootPath, &repo.DefaultRef, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (s *Store) GetRepo(ctx context.Context, id string) (domain.Repo, error) {
	var repo domain.Repo
	err := s.db.QueryRowContext(
		ctx,
		`select id, name, root_path, default_ref, created_at, updated_at from repos where id = ?`,
		id,
	).Scan(&repo.ID, &repo.Name, &repo.RootPath, &repo.DefaultRef, &repo.CreatedAt, &repo.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repo{}, domain.ErrNotFound
	}
	return repo, err
}

func (s *Store) CreateAnchor(ctx context.Context, anchor domain.Anchor) (domain.Anchor, error) {
	now := time.Now().UTC()
	if anchor.ID == "" {
		anchor.ID = domain.NewID("anchor")
	}
	if anchor.CreatedAt.IsZero() {
		anchor.CreatedAt = now
	}
	anchor.UpdatedAt = now
	bindingJSON, err := json.Marshal(anchor.Binding)
	if err != nil {
		return domain.Anchor{}, err
	}
	tagsJSON, err := json.Marshal(anchor.Tags)
	if err != nil {
		return domain.Anchor{}, err
	}
	_, err = s.db.ExecContext(
		ctx,
		`insert into anchors (id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		anchor.ID,
		anchor.RepoID,
		anchor.Kind,
		anchor.Status,
		anchor.Title,
		anchor.Body,
		anchor.Author,
		anchor.SourceRef,
		string(tagsJSON),
		string(bindingJSON),
		anchor.CreatedAt,
		anchor.UpdatedAt,
	)
	if err != nil {
		return domain.Anchor{}, err
	}
	if err := s.addEvent(ctx, domain.AnchorEvent{
		ID:         domain.NewID("event"),
		AnchorID:   anchor.ID,
		Type:       domain.AnchorEventCreated,
		Confidence: anchor.Binding.Confidence,
		ToBinding:  &anchor.Binding,
		CreatedAt:  now,
	}); err != nil {
		return domain.Anchor{}, err
	}
	return anchor, nil
}

func (s *Store) UpdateAnchor(ctx context.Context, anchor domain.Anchor) (domain.Anchor, error) {
	anchor.UpdatedAt = time.Now().UTC()
	bindingJSON, err := json.Marshal(anchor.Binding)
	if err != nil {
		return domain.Anchor{}, err
	}
	tagsJSON, err := json.Marshal(anchor.Tags)
	if err != nil {
		return domain.Anchor{}, err
	}
	_, err = s.db.ExecContext(
		ctx,
		`update anchors set kind = ?, status = ?, title = ?, body = ?, author = ?, source_ref = ?, tags_json = ?, binding_json = ?, updated_at = ? where id = ?`,
		anchor.Kind,
		anchor.Status,
		anchor.Title,
		anchor.Body,
		anchor.Author,
		anchor.SourceRef,
		string(tagsJSON),
		string(bindingJSON),
		anchor.UpdatedAt,
		anchor.ID,
	)
	if err != nil {
		return domain.Anchor{}, err
	}
	return anchor, nil
}

func (s *Store) GetAnchor(ctx context.Context, id string) (domain.Anchor, error) {
	var anchor domain.Anchor
	var tagsJSON string
	var bindingJSON string
	err := s.db.QueryRowContext(
		ctx,
		`select id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at from anchors where id = ?`,
		id,
	).Scan(
		&anchor.ID,
		&anchor.RepoID,
		&anchor.Kind,
		&anchor.Status,
		&anchor.Title,
		&anchor.Body,
		&anchor.Author,
		&anchor.SourceRef,
		&tagsJSON,
		&bindingJSON,
		&anchor.CreatedAt,
		&anchor.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Anchor{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Anchor{}, err
	}
	if err := json.Unmarshal([]byte(tagsJSON), &anchor.Tags); err != nil {
		return domain.Anchor{}, err
	}
	if err := json.Unmarshal([]byte(bindingJSON), &anchor.Binding); err != nil {
		return domain.Anchor{}, err
	}
	return anchor, nil
}

func (s *Store) ListAnchors(ctx context.Context, filter domain.AnchorFilter) ([]domain.Anchor, error) {
	query := `select id from anchors`
	args := make([]any, 0, 2)
	if filter.RepoID != "" {
		query += ` where repo_id = ?`
		args = append(args, filter.RepoID)
		if filter.Status != "" {
			query += ` and status = ?`
			args = append(args, filter.Status)
		}
	} else if filter.Status != "" {
		query += ` where status = ?`
		args = append(args, filter.Status)
	}
	query += ` order by created_at asc`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var anchors []domain.Anchor
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		anchor, err := s.GetAnchor(ctx, id)
		if err != nil {
			return nil, err
		}
		if filter.Path != "" && anchor.Binding.Path != filter.Path {
			continue
		}
		if filter.SymbolPath != "" && anchor.Binding.SymbolPath != filter.SymbolPath {
			continue
		}
		anchors = append(anchors, anchor)
	}
	return anchors, rows.Err()
}

func (s *Store) ReplaceBinding(ctx context.Context, anchorID string, binding domain.Binding, reason string, confidence float64) (domain.Anchor, error) {
	return s.ApplyResolution(ctx, anchorID, binding, domain.AnchorStatusActive, reason, confidence)
}

func (s *Store) ApplyResolution(ctx context.Context, anchorID string, binding domain.Binding, status domain.AnchorStatus, reason string, confidence float64) (domain.Anchor, error) {
	anchor, err := s.GetAnchor(ctx, anchorID)
	if err != nil {
		return domain.Anchor{}, err
	}
	previousBinding := anchor.Binding
	previousStatus := anchor.Status

	anchor.Binding = binding
	anchor.Binding.Confidence = confidence
	anchor.Status = status
	anchor.UpdatedAt = time.Now().UTC()
	updated, err := s.UpdateAnchor(ctx, anchor)
	if err != nil {
		return domain.Anchor{}, err
	}

	eventType := domain.AnchorEventUpdated
	switch {
	case status == domain.AnchorStatusStale:
		eventType = domain.AnchorEventStale
	case !bindingEqual(previousBinding, binding):
		eventType = domain.AnchorEventMoved
	case previousStatus != status:
		eventType = domain.AnchorEventUpdated
	}
	if err := s.addEvent(ctx, domain.AnchorEvent{
		ID:          domain.NewID("event"),
		AnchorID:    anchorID,
		Type:        eventType,
		Reason:      reason,
		Confidence:  confidence,
		FromBinding: &previousBinding,
		ToBinding:   &binding,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		return domain.Anchor{}, err
	}
	return updated, nil
}

func (s *Store) ListAnchorEvents(ctx context.Context, anchorID string) ([]domain.AnchorEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`select id, anchor_id, type, reason, confidence, from_binding_json, to_binding_json, created_at from anchor_events where anchor_id = ? order by created_at asc`,
		anchorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.AnchorEvent
	for rows.Next() {
		var event domain.AnchorEvent
		var fromBinding sql.NullString
		var toBinding sql.NullString
		if err := rows.Scan(&event.ID, &event.AnchorID, &event.Type, &event.Reason, &event.Confidence, &fromBinding, &toBinding, &event.CreatedAt); err != nil {
			return nil, err
		}
		if fromBinding.Valid {
			var binding domain.Binding
			if err := json.Unmarshal([]byte(fromBinding.String), &binding); err != nil {
				return nil, err
			}
			event.FromBinding = &binding
		}
		if toBinding.Valid {
			var binding domain.Binding
			if err := json.Unmarshal([]byte(toBinding.String), &binding); err != nil {
				return nil, err
			}
			event.ToBinding = &binding
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CreateComment(ctx context.Context, comment domain.Comment) (domain.Comment, error) {
	if comment.ID == "" {
		comment.ID = domain.NewID("comment")
	}
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`insert into comments (id, anchor_id, parent_id, author, body, created_at) values (?, ?, ?, ?, ?, ?)`,
		comment.ID,
		comment.AnchorID,
		nullString(comment.ParentID),
		comment.Author,
		comment.Body,
		comment.CreatedAt,
	)
	return comment, err
}

func (s *Store) ListComments(ctx context.Context, anchorID string) ([]domain.Comment, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`select id, anchor_id, parent_id, author, body, created_at from comments where anchor_id = ? order by created_at asc`,
		anchorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []domain.Comment
	for rows.Next() {
		var comment domain.Comment
		var parent sql.NullString
		if err := rows.Scan(&comment.ID, &comment.AnchorID, &parent, &comment.Author, &comment.Body, &comment.CreatedAt); err != nil {
			return nil, err
		}
		if parent.Valid {
			comment.ParentID = parent.String
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func (s *Store) addEvent(ctx context.Context, event domain.AnchorEvent) error {
	var fromJSON any
	var toJSON any
	if event.FromBinding != nil {
		encoded, err := json.Marshal(event.FromBinding)
		if err != nil {
			return err
		}
		fromJSON = string(encoded)
	}
	if event.ToBinding != nil {
		encoded, err := json.Marshal(event.ToBinding)
		if err != nil {
			return err
		}
		toJSON = string(encoded)
	}
	_, err := s.db.ExecContext(
		ctx,
		`insert into anchor_events (id, anchor_id, type, reason, confidence, from_binding_json, to_binding_json, created_at) values (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.AnchorID,
		event.Type,
		event.Reason,
		event.Confidence,
		fromJSON,
		toJSON,
		event.CreatedAt,
	)
	return err
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		create table if not exists repos (
			id text primary key,
			name text not null,
			root_path text not null,
			default_ref text not null,
			created_at timestamp not null,
			updated_at timestamp not null
		);

		create table if not exists anchors (
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

		create table if not exists anchor_events (
			id text primary key,
			anchor_id text not null,
			type text not null,
			reason text not null,
			confidence real not null,
			from_binding_json text,
			to_binding_json text,
			created_at timestamp not null
		);

		create table if not exists comments (
			id text primary key,
			anchor_id text not null,
			parent_id text,
			author text not null,
			body text not null,
			created_at timestamp not null
		);
	`)
	return err
}

func bindingEqual(left, right domain.Binding) bool {
	return left.Type == right.Type &&
		left.Ref == right.Ref &&
		left.Path == right.Path &&
		left.Language == right.Language &&
		left.SymbolPath == right.SymbolPath &&
		left.StartLine == right.StartLine &&
		left.StartCol == right.StartCol &&
		left.EndLine == right.EndLine &&
		left.EndCol == right.EndCol &&
		left.SelectedTextHash == right.SelectedTextHash &&
		left.BeforeHash == right.BeforeHash &&
		left.AfterHash == right.AfterHash
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
