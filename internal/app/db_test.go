package app

import (
	"database/sql"
	"testing"
)

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var name string
	err := db.QueryRow(
		`select name from sqlite_master where type = 'table' and name = ?`,
		table,
	).Scan(&name)
	if err != nil {
		t.Errorf("table %s missing: %v", table, err)
	}
}

func assertSchemaVersion(t *testing.T, db *sql.DB, want int) {
	t.Helper()

	var version int
	if err := db.QueryRow(`pragma user_version`).Scan(&version); err != nil {
		t.Fatalf("pragma user_version: %v", err)
	}
	if version != want {
		t.Errorf("user_version = %d, want %d", version, want)
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()

	rows, err := db.Query(`pragma table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info(%s): %v", table, err)
	}
	if !found {
		t.Errorf("column %s.%s missing", table, column)
	}
}

func TestInitSchema(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	for _, table := range []string{
		"users", "sessions", "channels", "videos",
		"favorites", "subscriptions", "comments",
	} {
		assertTableExists(t, db, table)
	}
	assertSchemaVersion(t, db, len(schemaMigrations))

	assertColumnExists(t, db, "users", "is_admin")
	assertColumnExists(t, db, "videos", "allow_comments")
	assertColumnExists(t, db, "videos", "made_for_kids")
	assertColumnExists(t, db, "subscriptions", "notify")

	res, err := db.Exec(`insert into users(email, password_hash, location)
		values('defaults@test.com', 'test-hash', 'Sofia')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	insertChannel(t, db, "1", "Defaults", "Sofia", userID, false)
	if _, err := db.Exec(`insert into videos(
		channel_id, title, description, media_url, thumbnail_url, location
	) values('1', 'Defaults Vid', 'd', '/media/a.mp4', '/media/a.jpg', 'Sofia')`); err != nil {
		t.Fatalf("insert video: %v", err)
	}
	if _, err := db.Exec(
		`insert into subscriptions(user_id, channel_id) values(?, '1')`, userID,
	); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	var isAdmin int
	if err := db.QueryRow(
		`select is_admin from users where id = ?`, userID,
	).Scan(&isAdmin); err != nil {
		t.Fatalf("read is_admin: %v", err)
	}
	if isAdmin != 0 {
		t.Errorf("users.is_admin default = %d, want 0", isAdmin)
	}

	var allowComments, madeForKids int
	if err := db.QueryRow(
		`select allow_comments, made_for_kids from videos`,
	).Scan(&allowComments, &madeForKids); err != nil {
		t.Fatalf("read video defaults: %v", err)
	}
	if allowComments != 1 {
		t.Errorf("videos.allow_comments default = %d, want 1", allowComments)
	}
	if madeForKids != 0 {
		t.Errorf("videos.made_for_kids default = %d, want 0", madeForKids)
	}

	var notify int
	if err := db.QueryRow(`select notify from subscriptions`).Scan(&notify); err != nil {
		t.Fatalf("read notify: %v", err)
	}
	if notify != 0 {
		t.Errorf("subscriptions.notify default = %d, want 0", notify)
	}
}

func TestInitSchemaIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("first initSchema: %v", err)
	}
	userID := insertUser(t, db, "persist@test.com", "pw", "Sofia", false)

	if err := initSchema(db); err != nil {
		t.Fatalf("second initSchema: %v", err)
	}

	assertSchemaVersion(t, db, len(schemaMigrations))

	var email string
	if err := db.QueryRow(
		`select email from users where id = ?`, userID,
	).Scan(&email); err != nil {
		t.Fatalf("fixture row lost after re-run: %v", err)
	}
	if email != "persist@test.com" {
		t.Errorf("fixture email = %q, want persist@test.com", email)
	}
}

func TestInitSchemaLegacyBootstrap(t *testing.T) {
	db := openTestDB(t)

	// Simulate a database whose old startup path applied migrations 1-10:
	// tables and early columns exist in their historical shape, and applied
	// versions are recorded in the legacy schema_migrations ledger.
	for _, stmt := range []string{
		`create table users (
			id integer primary key autoincrement,
			email text not null unique,
			password_hash text not null,
			location text not null,
			created_at datetime default current_timestamp
		)`,
		`create table channels (
			id text primary key,
			name text not null,
			location text not null,
			owner_user_id text not null
		)`,
		`create table videos (
			id integer primary key autoincrement,
			channel_id text not null,
			title text not null,
			description text not null,
			media_url text not null,
			thumbnail_url text not null,
			location text not null
		)`,
		`create table sessions (
			token text primary key,
			user_id integer not null,
			created_at datetime default current_timestamp
		)`,
		`create table favorites (
			user_id integer not null,
			video_id integer not null,
			primary key(user_id, video_id)
		)`,
		`create table subscriptions (
			user_id integer not null,
			channel_id integer not null,
			primary key(user_id, channel_id)
		)`,
		`alter table videos add column user_id integer default 0`,
		`alter table videos add column is_public integer default 1`,
		`alter table channels add column user_id integer default 0`,
		`alter table users add column is_admin integer default 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("build legacy database: %v", err)
		}
	}
	const legacyApplied = 10
	if _, err := db.Exec(`create table schema_migrations (
		version integer primary key,
		applied_at datetime default current_timestamp
	)`); err != nil {
		t.Fatalf("create legacy ledger: %v", err)
	}
	for v := 1; v <= legacyApplied; v++ {
		if _, err := db.Exec(
			`insert into schema_migrations(version) values(?)`, v,
		); err != nil {
			t.Fatalf("insert legacy version %d: %v", v, err)
		}
	}

	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	assertSchemaVersion(t, db, len(schemaMigrations))
	assertColumnExists(t, db, "channels", "avatar_url")
	assertColumnExists(t, db, "subscriptions", "notify")

	var legacyName string
	err := db.QueryRow(
		`select name from sqlite_master where type = 'table' and name = 'schema_migrations'`,
	).Scan(&legacyName)
	if err != sql.ErrNoRows {
		t.Errorf("schema_migrations lookup = %v, want table dropped", err)
	}

	if err := initSchema(db); err != nil {
		t.Fatalf("second initSchema: %v", err)
	}
	assertSchemaVersion(t, db, len(schemaMigrations))
}

func TestInitSchemaPreMigrationDatabase(t *testing.T) {
	db := openTestDB(t)

	// Simulate a database older than any bookkeeping: tables exist in their
	// historical shape and the old blind-alter startup path already added
	// some columns, so initSchema must tolerate the duplicates.
	for _, stmt := range []string{
		`create table users (
			id integer primary key autoincrement,
			email text not null unique,
			password_hash text not null,
			location text not null,
			created_at datetime default current_timestamp
		)`,
		`create table channels (
			id text primary key,
			name text not null,
			location text not null,
			owner_user_id text not null
		)`,
		`create table videos (
			id integer primary key autoincrement,
			channel_id text not null,
			title text not null,
			description text not null,
			media_url text not null,
			thumbnail_url text not null,
			location text not null
		)`,
		`alter table videos add column is_public integer default 1`,
		`alter table users add column is_admin integer default 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("build pre-migration database: %v", err)
		}
	}

	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	assertSchemaVersion(t, db, len(schemaMigrations))
	assertColumnExists(t, db, "videos", "made_for_kids")
	assertColumnExists(t, db, "channels", "is_admin_locked")
}

func TestForeignKeyConstraint(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	if _, err := db.Exec(
		`insert into sessions(token, user_id) values('orphan-token', 4242)`,
	); err == nil {
		t.Fatal("expected foreign key violation inserting session for missing user")
	}

	userID := insertUser(t, db, "fk@test.com", "pw", "Sofia", false)
	if _, err := db.Exec(
		`insert into sessions(token, user_id) values('ok-token', ?)`, userID,
	); err != nil {
		t.Fatalf("valid session insert failed: %v", err)
	}
}
