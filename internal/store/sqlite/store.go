package sqlite

import "database/sql"

type Store struct {
	db *sql.DB
}

func (s *Store) Close() error {
	return s.db.Close()
}
