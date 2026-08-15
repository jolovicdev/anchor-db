package sqlite

import (
	"context"
	"fmt"
)

func (s *Store) migrate() error {
	migrations := []string{
		`
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

		create virtual table if not exists search_index using fts5(
			doc_type unindexed,
			doc_id unindexed,
			anchor_id unindexed,
			comment_id unindexed,
			repo_id unindexed,
			path unindexed,
			symbol unindexed,
			kind unindexed,
			title,
			body
		);
		`,
		`
		alter table comments rename to comments_legacy;
		alter table anchor_events rename to anchor_events_legacy;
		alter table anchors rename to anchors_legacy;
		alter table repos rename to repos_legacy;

		create table repos (
			id text primary key,
			name text not null,
			root_path text not null,
			default_ref text not null,
			created_at timestamp not null,
			updated_at timestamp not null
		);

		insert into repos (id, name, root_path, default_ref, created_at, updated_at)
		select id, name, root_path, default_ref, created_at, updated_at
		from repos_legacy;

		create table anchors (
			id text primary key,
			repo_id text not null references repos(id) on delete cascade,
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

		insert into anchors (id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at)
		select id, repo_id, kind, status, title, body, author, source_ref, tags_json, binding_json, created_at, updated_at
		from anchors_legacy;

		create index idx_anchors_repo_id on anchors(repo_id);

		create table anchor_events (
			id text primary key,
			anchor_id text not null references anchors(id) on delete cascade,
			type text not null,
			reason text not null,
			confidence real not null,
			from_binding_json text,
			to_binding_json text,
			created_at timestamp not null
		);

		insert into anchor_events (id, anchor_id, type, reason, confidence, from_binding_json, to_binding_json, created_at)
		select id, anchor_id, type, reason, confidence, from_binding_json, to_binding_json, created_at
		from anchor_events_legacy;

		create index idx_anchor_events_anchor_id on anchor_events(anchor_id);

		create table comments (
			id text primary key,
			anchor_id text not null references anchors(id) on delete cascade,
			parent_id text references comments(id) on delete cascade deferrable initially deferred,
			author text not null,
			body text not null,
			created_at timestamp not null
		);

		insert into comments (id, anchor_id, parent_id, author, body, created_at)
		select id, anchor_id, parent_id, author, body, created_at
		from comments_legacy
		order by created_at asc, id asc;

		create index idx_comments_anchor_id on comments(anchor_id);
		create index idx_comments_parent_id on comments(parent_id);

		drop table comments_legacy;
		drop table anchor_events_legacy;
		drop table anchors_legacy;
		drop table repos_legacy;

		create trigger delete_anchor_search after delete on anchors begin
			delete from search_index where doc_id = 'anchor:' || old.id;
		end;

		create trigger delete_comment_search after delete on comments begin
			delete from search_index where doc_id = 'comment:' || old.id;
		end;
		`,
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(context.Background(), `pragma user_version`).Scan(&version); err != nil {
		return err
	}
	for idx := version; idx < len(migrations); idx++ {
		if _, err := tx.ExecContext(context.Background(), migrations[idx]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), fmt.Sprintf(`pragma user_version = %d`, idx+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
