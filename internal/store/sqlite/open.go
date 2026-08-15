package sqlite

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// pragmas are connection-scoped, so they belong in the DSN rather than in
// statements run once after opening. database/sql may discard a connection
// after a driver error and open a replacement transparently; a replacement
// configured by Exec would come back with foreign keys and the busy timeout
// silently off.
const pragmaDSN = "?_pragma=journal_mode(wal)" +
	"&_pragma=synchronous(normal)" +
	"&_pragma=foreign_keys(on)" +
	"&_pragma=busy_timeout(5000)"

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+pragmaDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
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
