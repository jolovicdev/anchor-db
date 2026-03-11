package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

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
	if err := s.upsertAnchorSearch(ctx, anchor); err != nil {
		return domain.Anchor{}, err
	}
	return anchor, nil
}

func (s *Store) UpdateAnchor(ctx context.Context, anchor domain.Anchor, reason string) (domain.Anchor, error) {
	previous, err := s.GetAnchor(ctx, anchor.ID)
	if err != nil {
		return domain.Anchor{}, err
	}
	updated, err := s.updateAnchorRecord(ctx, anchor)
	if err != nil {
		return domain.Anchor{}, err
	}
	if err := s.addEvent(ctx, domain.AnchorEvent{
		ID:          domain.NewID("event"),
		AnchorID:    updated.ID,
		Type:        domain.AnchorEventUpdated,
		Reason:      reason,
		Confidence:  updated.Binding.Confidence,
		FromBinding: &previous.Binding,
		ToBinding:   &updated.Binding,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		return domain.Anchor{}, err
	}
	return updated, nil
}

func (s *Store) updateAnchorRecord(ctx context.Context, anchor domain.Anchor) (domain.Anchor, error) {
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
	if err := s.upsertAnchorSearch(ctx, anchor); err != nil {
		return domain.Anchor{}, err
	}
	if err := s.reindexCommentsForAnchor(ctx, anchor.ID); err != nil {
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
	query := `select id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at from anchors`
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 6)
	if filter.RepoID != "" {
		conditions = append(conditions, `repo_id = ?`)
		args = append(args, filter.RepoID)
	}
	if filter.Status != "" {
		conditions = append(conditions, `status = ?`)
		args = append(args, filter.Status)
	}
	if filter.Path != "" {
		conditions = append(conditions, `json_extract(binding_json, '$.path') = ?`)
		args = append(args, filter.Path)
	}
	if filter.SymbolPath != "" {
		conditions = append(conditions, `json_extract(binding_json, '$.symbol_path') = ?`)
		args = append(args, filter.SymbolPath)
	}
	if len(conditions) > 0 {
		query += ` where ` + strings.Join(conditions, ` and `)
	}
	query += ` order by created_at asc`
	if filter.Limit > 0 {
		query += ` limit ?`
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += ` offset ?`
			args = append(args, filter.Offset)
		}
	} else if filter.Offset > 0 {
		query += ` limit -1 offset ?`
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var anchors []domain.Anchor
	for rows.Next() {
		anchor, err := scanAnchor(rows)
		if err != nil {
			return nil, err
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
	updated, err := s.updateAnchorRecord(ctx, anchor)
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

func scanAnchor(scanner interface {
	Scan(dest ...any) error
}) (domain.Anchor, error) {
	var anchor domain.Anchor
	var tagsJSON string
	var bindingJSON string
	err := scanner.Scan(
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
