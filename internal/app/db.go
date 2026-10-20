package app

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// schemaMigrations is the ordered list of schema changes. Each entry is applied
// at most once; the count of applied entries is stored in pragma user_version.
var schemaMigrations = []string{
	// 1
	`create table if not exists users (
		id integer primary key autoincrement,
		email text not null unique,
		password_hash text not null,
		location text not null,
		created_at datetime default current_timestamp
	)`,
	// 2
	`create table if not exists sessions (
		token text primary key,
		user_id integer not null,
		created_at datetime default current_timestamp,
		foreign key(user_id) references users(id)
	)`,
	// 3
	`create table if not exists channels (
		id text primary key,
		name text not null,
		location text not null,
		description text not null default '',
		avatar_url text not null default '',
		owner_user_id text not null,
		is_admin_locked integer not null default 0
	)`,
	// 4
	`create table if not exists videos (
		id integer primary key autoincrement,
		channel_id text not null,
		title text not null,
		description text not null,
		media_url text not null,
		thumbnail_url text not null,
		location text not null,
		user_id integer default 0,
		is_public integer default 1,
		foreign key(channel_id) references channels(id)
	)`,
	// 5
	`create table if not exists favorites (
		user_id integer not null,
		video_id integer not null,
		primary key(user_id, video_id),
		foreign key(user_id) references users(id),
		foreign key(video_id) references videos(id)
	)`,
	// 6
	`create table if not exists subscriptions (
		user_id integer not null,
		channel_id integer not null,
		primary key(user_id, channel_id),
		foreign key(user_id) references users(id),
		foreign key(channel_id) references channels(id)
	)`,
	// 7: creator panel upgrades; databases from the pre-migration era may
	// already carry these columns, so duplicate-column errors are tolerated.
	`alter table videos add column user_id integer default 0`,
	// 8
	`alter table videos add column is_public integer default 1`,
	// 9
	`alter table channels add column user_id integer default 0`,
	// 10
	`alter table users add column is_admin integer default 0`,
	// 11
	`alter table videos add column is_admin_locked integer default 0`,
	// 12
	`alter table channels add column description text default ''`,
	// 13
	`alter table channels add column is_admin_locked integer default 0`,
	// 14
	`alter table channels add column avatar_url text default ''`,
	// 15
	`update users set location = 'Sofia' where location = 'Berlin';
	update channels set location = 'Sofia' where location = 'Berlin';
	update videos set location = 'Sofia' where location = 'Berlin'`,
	// 16
	`create table if not exists comments (
		id integer primary key autoincrement,
		video_id integer not null,
		user_id integer not null,
		content text not null,
		created_at datetime default current_timestamp,
		foreign key(video_id) references videos(id),
		foreign key(user_id) references users(id)
	)`,
	// 17
	`alter table videos add column allow_comments integer default 1`,
	// 18
	`alter table videos add column made_for_kids integer default 0`,
	// 19
	`alter table subscriptions add column notify integer default 0`,
}

func initSchema(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`pragma user_version`).Scan(&version); err != nil {
		return err
	}

	// Migrate bookkeeping from the legacy schema_migrations table, which
	// databases initialized by older code may still carry.
	var hasLegacyTable int
	if err := db.QueryRow(
		`select count(*) from sqlite_master where type = 'table' and name = 'schema_migrations'`,
	).Scan(&hasLegacyTable); err != nil {
		return err
	}
	if hasLegacyTable > 0 {
		if version == 0 {
			if err := db.QueryRow(
				`select coalesce(max(version), 0) from schema_migrations`,
			).Scan(&version); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`drop table schema_migrations`); err != nil {
			return err
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := version; i < len(schemaMigrations); i++ {
		if _, err := tx.Exec(schemaMigrations[i]); err != nil {
			// Tolerate columns added by the old blind-alter path; any other
			// failure aborts the whole schema update.
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("migration %d: %w", i+1, err)
			}
		}
	}
	if _, err := tx.Exec(
		fmt.Sprintf(`pragma user_version = %d`, len(schemaMigrations)),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func createChannel(
	db interface {
		QueryRow(string, ...any) *sql.Row
	},
	name, location string,
	ownerID int64,
) (int64, error) {
	var channelID int64
	err := db.QueryRow(`
		insert into channels(id, name, location, owner_user_id, user_id)
		select cast(coalesce(max(cast(id as integer)), 0) + 1 as text), ?, ?, ?, ?
		from channels
		returning id
	`, name, location, strconv.FormatInt(ownerID, 10), ownerID).Scan(&channelID)
	return channelID, err
}

const seedAdminEmail = "admin@admin.com"

// seedAdmin creates the default administrator used for local development and
// testing. The password defaults to admin12345 and can be overridden by
// VIDEO_SHARE_ADMIN_PASSWORD at first creation; an existing account is never
// modified.
func seedAdmin(db interface {
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}) error {
	var exists int
	if err := db.QueryRow(
		`select count(*) from users where email = ?`, seedAdminEmail,
	).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	password := os.Getenv("VIDEO_SHARE_ADMIN_PASSWORD")
	if password == "" {
		password = "admin12345"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`insert into users(email, password_hash, location, is_admin) values(?, ?, ?, 1)`,
		seedAdminEmail, string(hash), "Sofia",
	)
	return err
}

func seedData(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := seedAdmin(tx); err != nil {
		return err
	}

	channelSeeds := []struct {
		name     string
		location string
	}{
		{name: "Creek Walks", location: "Sofia"},
		{name: "Park Ponds", location: "Sofia"},
		{name: "Bridges & Paths", location: "Sofia"},
		{name: "Water Features", location: "Sofia"},
		{name: "Urban Views", location: "Sofia"},
	}

	channelIDs := make([]int64, len(channelSeeds))
	for i, item := range channelSeeds {
		err := tx.QueryRow(`select id from channels where name = ?`, item.name).Scan(&channelIDs[i])
		if err == sql.ErrNoRows {
			channelIDs[i], err = createChannel(tx, item.name, item.location, 0)
		}
		if err != nil {
			return err
		}
	}

	videoSeeds := []struct {
		channelIndex int
		title        string
		description  string
		mediaURL     string
		thumbnailURL string
		location     string
	}{
		{
			channelIndex: 1,
			title:        "Winter Creek Flow",
			description:  "A creek winding through bare winter woods with fallen leaves and exposed roots.",
			mediaURL:     "/media/VID_20260313_170235.mp4",
			thumbnailURL: "/media/VID_20260313_170235.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 1,
			title:        "Rocky Creek Bend",
			description:  "Stream curving past rocks and fallen branches under leafless trees.",
			mediaURL:     "/media/VID_20260313_170322.mp4",
			thumbnailURL: "/media/VID_20260313_170322.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 2,
			title:        "Ducks on the Pond",
			description:  "A calm park pond with ducks swimming among the reeds on a clear winter day.",
			mediaURL:     "/media/VID_20260313_170438.mp4",
			thumbnailURL: "/media/VID_20260313_170438.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 2,
			title:        "Reedy Pond Edge",
			description:  "Still water bordered by dry reeds and grass at the edge of a park pond.",
			mediaURL:     "/media/VID_20260313_170539.mp4",
			thumbnailURL: "/media/VID_20260313_170539.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 3,
			title:        "Bridge Railing Shadows",
			description: "Afternoon sun casting long railing shadows across a" +
				" pedestrian bridge over water.",
			mediaURL:     "/media/VID_20260313_170644.mp4",
			thumbnailURL: "/media/VID_20260313_170644.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 3,
			title:        "Carved Forest Trees",
			description:  "A forest path lined with tall pines and tree trunks bearing carved wooden art.",
			mediaURL:     "/media/VID_20260313_171117.mp4",
			thumbnailURL: "/media/VID_20260313_171117.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 4,
			title:        "Rocky Stream Waterfall",
			description:  "Water cascading over mossy rocks in a small stone-lined stream.",
			mediaURL:     "/media/VID_20260313_171308.mp4",
			thumbnailURL: "/media/VID_20260313_171308.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 5,
			title:        "Waterfront Sculptures",
			description: "An urban canal with white horse sculptures, weeping willows," +
				" and apartment blocks.",
			mediaURL:     "/media/VID_20260313_172627.mp4",
			thumbnailURL: "/media/VID_20260313_172627.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 5,
			title:        "Sunlit Flooded Woods",
			description:  "Late afternoon sun reflecting off standing water in a flooded woodland.",
			mediaURL:     "/media/VID_20260314_172838.mp4",
			thumbnailURL: "/media/VID_20260314_172838.jpg",
			location:     "Sofia",
		},
		{
			channelIndex: 5,
			title:        "Parked Car Close-Up",
			description:  "Close-up of a car hood and headlight on an overcast day.",
			mediaURL:     "/media/VID_20260220_171545.mp4",
			thumbnailURL: "/media/VID_20260220_171545.jpg",
			location:     "Sofia",
		},
	}

	for _, item := range videoSeeds {
		channelID := strconv.FormatInt(channelIDs[item.channelIndex-1], 10)
		var exists int
		if err := tx.QueryRow(
			`select count(*) from videos where channel_id = ? and title = ?`,
			channelID,
			item.title,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		if _, err := tx.Exec(
			`insert into videos(
				channel_id, title, description, media_url, thumbnail_url, location
			) values(?, ?, ?, ?, ?, ?)`,
			channelID,
			item.title,
			item.description,
			item.mediaURL,
			item.thumbnailURL,
			item.location,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}
