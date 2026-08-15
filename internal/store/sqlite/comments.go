package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

func (s *Store) CreateComment(ctx context.Context, comment domain.Comment) (domain.Comment, error) {
	if comment.ID == "" {
		comment.ID = domain.NewID("comment")
	}
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = time.Now().UTC()
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			`insert into comments (id, anchor_id, parent_id, author, body, created_at) values (?, ?, ?, ?, ?, ?)`,
			comment.ID,
			comment.AnchorID,
			nullString(comment.ParentID),
			comment.Author,
			comment.Body,
			comment.CreatedAt,
		); err != nil {
			return err
		}
		return upsertCommentSearch(ctx, tx, comment)
	})
	if err != nil {
		return domain.Comment{}, err
	}
	return comment, nil
}

func (s *Store) ListComments(ctx context.Context, anchorID string) ([]domain.Comment, error) {
	return listComments(ctx, s.db, anchorID)
}

func listComments(ctx context.Context, ex executor, anchorID string) ([]domain.Comment, error) {
	rows, err := ex.QueryContext(
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

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
