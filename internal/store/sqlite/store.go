package sqlite

import (
	"context"
	"database/sql"
)

type Store struct {
	db *sql.DB
}

func (s *Store) Close() error {
	return s.db.Close()
}

// executor is satisfied by both *sql.DB and *sql.Tx so query helpers can run
// either standalone or inside a transaction.
//
// Threading this through matters more than it looks: the pool is capped at a
// single connection, so a helper that reached for s.db while a transaction was
// open would block forever waiting for a connection the transaction holds.
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// withTx runs fn in a transaction, rolling back on error. Writes that touch
// several tables -- an anchor plus its event plus its search-index row -- go
// through here so a mid-sequence failure cannot leave the search index
// describing a state the base tables never reached.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
