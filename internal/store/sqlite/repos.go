package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jolovicdev/anchor-db/internal/domain"
)

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

func (s *Store) UpdateRepo(ctx context.Context, repo domain.Repo) (domain.Repo, error) {
	repo.UpdatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`update repos set name = ?, root_path = ?, default_ref = ?, updated_at = ? where id = ?`,
		repo.Name,
		repo.RootPath,
		repo.DefaultRef,
		repo.UpdatedAt,
		repo.ID,
	)
	if err != nil {
		return domain.Repo{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return domain.Repo{}, err
	}
	if rowsAffected == 0 {
		return domain.Repo{}, domain.ErrNotFound
	}
	return repo, nil
}

func (s *Store) DeleteRepo(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `delete from repos where id = ?`, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
