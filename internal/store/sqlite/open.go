package sqlite

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`pragma journal_mode = wal`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`pragma synchronous = normal`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`pragma foreign_keys = on`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`pragma busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	ctx := context.Background()
	needsRebuild, err := store.searchIndexNeedsRebuild(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	if needsRebuild {
		if err := store.withTx(ctx, func(tx *sql.Tx) error {
			return rebuildSearchIndex(ctx, tx)
		}); err != nil {
			db.Close()
			return nil, err
		}
	}
	return store, nil
}
