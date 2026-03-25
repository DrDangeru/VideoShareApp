package app

import (
	"database/sql"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func initSchema(db *sql.DB) error {
	// 1. Ensure migrations table exists
	_, err := db.Exec(`create table if not exists schema_migrations (
		version integer primary key,
		applied_at datetime default current_timestamp
	)`)
	if err != nil {
		return err
	}

	// 2. Define ordered migrations
	migrations := []struct {
		version int
		stmt    string
	}{
		{1, `create table if not exists users (
			id integer primary key autoincrement,
			email text not null unique,
			password_hash text not null,
			location text not null,
			created_at datetime default current_timestamp
		)`},
		{2, `create table if not exists sessions (
			token text primary key,
			user_id integer not null,
			created_at datetime default current_timestamp,
			foreign key(user_id) references users(id)
		)`},
		{3, `create table if not exists channels (
			id text primary key,
			name text not null,
			location text not null,
			description text not null default '',
			avatar_url text not null default '',
			owner_user_id text not null,
			is_admin_locked integer not null default 0
		)`},
		{4, `create table if not exists videos (
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
		)`},
		{5, `create table if not exists favorites (
			user_id integer not null,
			video_id integer not null,
			primary key(user_id, video_id),
			foreign key(user_id) references users(id),
			foreign key(video_id) references videos(id)
		)`},
		{6, `create table if not exists subscriptions (
			user_id integer not null,
			channel_id integer not null,
			primary key(user_id, channel_id),
			foreign key(user_id) references users(id),
			foreign key(channel_id) references channels(id)
		)`},
		// Creator panel upgrades - these will gracefully error if the columns already exist
		// but the version will be saved so we don't keep trying to run them.
		{7, `alter table videos add column user_id integer default 0`},
		{8, `alter table videos add column is_public integer default 1`},
		{9, `alter table channels add column user_id integer default 0`},
		{10, `alter table users add column is_admin integer default 0`},
		{11, `alter table videos add column is_admin_locked integer default 0`},
		{12, `alter table channels add column description text default ''`},
		{13, `alter table channels add column is_admin_locked integer default 0`},
		{14, `alter table channels add column avatar_url text default ''`},
		{15, `
			update users set location = 'Sofia' where location = 'Berlin';
			update channels set location = 'Sofia' where location = 'Berlin';
			update videos set location = 'Sofia' where location = 'Berlin';
		`},
	}

	// 3. Apply missing migrations
	for _, m := range migrations {
		var applied int
		err := db.QueryRow(`
			select count(*) from schema_migrations where version = ?
		`, m.version).Scan(&applied)
		if err != nil {
			return err
		}

		if applied == 0 {
			if _, err := db.Exec(m.stmt); err != nil {
				// Ignore errors for "duplicate column" when transitioning from the old blind alter method
				if !strings.Contains(err.Error(), "duplicate column name") {
					return err
				}
			}
			if _, err := db.Exec(
				`insert into schema_migrations (version) values (?)`,
				m.version,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

func seedData(db *sql.DB) error {
	adminHash, err := bcrypt.GenerateFromPassword([]byte("admin12345"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if _, err := db.Exec(`insert into users(email, password_hash, location, is_admin)
		select ?, ?, ?, ?
		where not exists (select 1 from users where email = ?)`,
		"admin@admin.com",
		string(adminHash),
		"Sofia",
		1,
		"admin@admin.com",
	); err != nil {
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

	for _, item := range channelSeeds {
		var exists int
		if err := db.QueryRow(
			`select count(*) from channels where name = ?`,
			item.name,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		if _, err := db.Exec(
			`insert into channels(name, location) values(?, ?)`,
			item.name,
			item.location,
		); err != nil {
			return err
		}
	}

	videoSeeds := []struct {
		channelID    int
		title        string
		description  string
		mediaURL     string
		thumbnailURL string
		location     string
	}{
		{
			channelID:    1,
			title:        "Winter Creek Flow",
			description:  "A creek winding through bare winter woods with fallen leaves and exposed roots.",
			mediaURL:     "/media/VID_20260313_170235.mp4",
			thumbnailURL: "/media/VID_20260313_170235.jpg",
			location:     "Sofia",
		},
		{
			channelID:    1,
			title:        "Rocky Creek Bend",
			description:  "Stream curving past rocks and fallen branches under leafless trees.",
			mediaURL:     "/media/VID_20260313_170322.mp4",
			thumbnailURL: "/media/VID_20260313_170322.jpg",
			location:     "Sofia",
		},
		{
			channelID:    2,
			title:        "Ducks on the Pond",
			description:  "A calm park pond with ducks swimming among the reeds on a clear winter day.",
			mediaURL:     "/media/VID_20260313_170438.mp4",
			thumbnailURL: "/media/VID_20260313_170438.jpg",
			location:     "Sofia",
		},
		{
			channelID:    2,
			title:        "Reedy Pond Edge",
			description:  "Still water bordered by dry reeds and grass at the edge of a park pond.",
			mediaURL:     "/media/VID_20260313_170539.mp4",
			thumbnailURL: "/media/VID_20260313_170539.jpg",
			location:     "Sofia",
		},
		{
			channelID:    3,
			title:        "Bridge Railing Shadows",
			description:  "Afternoon sun casting long railing shadows across a" +
				" pedestrian bridge over water.",
			mediaURL:     "/media/VID_20260313_170644.mp4",
			thumbnailURL: "/media/VID_20260313_170644.jpg",
			location:     "Sofia",
		},
		{
			channelID:    3,
			title:        "Carved Forest Trees",
			description:  "A forest path lined with tall pines and tree trunks bearing carved wooden art.",
			mediaURL:     "/media/VID_20260313_171117.mp4",
			thumbnailURL: "/media/VID_20260313_171117.jpg",
			location:     "Sofia",
		},
		{
			channelID:    4,
			title:        "Rocky Stream Waterfall",
			description:  "Water cascading over mossy rocks in a small stone-lined stream.",
			mediaURL:     "/media/VID_20260313_171308.mp4",
			thumbnailURL: "/media/VID_20260313_171308.jpg",
			location:     "Sofia",
		},
		{
			channelID:    5,
			title:        "Waterfront Sculptures",
			description: "An urban canal with white horse sculptures, weeping willows," +
				" and apartment blocks.",
			mediaURL:     "/media/VID_20260313_172627.mp4",
			thumbnailURL: "/media/VID_20260313_172627.jpg",
			location:     "Sofia",
		},
		{
			channelID:    5,
			title:        "Sunlit Flooded Woods",
			description:  "Late afternoon sun reflecting off standing water in a flooded woodland.",
			mediaURL:     "/media/VID_20260314_172838.mp4",
			thumbnailURL: "/media/VID_20260314_172838.jpg",
			location:     "Sofia",
		},
		{
			channelID:    5,
			title:        "Parked Car Close-Up",
			description:  "Close-up of a car hood and headlight on an overcast day.",
			mediaURL:     "/media/VID_20260220_171545.mp4",
			thumbnailURL: "/media/VID_20260220_171545.jpg",
			location:     "Sofia",
		},
	}

	for _, item := range videoSeeds {
		var exists int
		if err := db.QueryRow(
			`select count(*) from videos where channel_id = ? and title = ?`,
			item.channelID,
			item.title,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		if _, err := db.Exec(
			`insert into videos(
				channel_id, title, description, media_url, thumbnail_url, location
			) values(?, ?, ?, ?, ?, ?)`,
			item.channelID,
			item.title,
			item.description,
			item.mediaURL,
			item.thumbnailURL,
			item.location,
		); err != nil {
			return err
		}
	}

	return nil
}
