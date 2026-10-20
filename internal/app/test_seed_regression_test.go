package app

import (
	"bytes"
	"database/sql"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestSeedFreshDatabase(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	if err := seedData(db); err != nil {
		t.Fatalf("seedData: %v", err)
	}

	if n := countRows(t, db, "users"); n != 1 {
		t.Fatalf("users = %d, want seeded admin only", n)
	}
	var adminFlag int
	var adminHash string
	if err := db.QueryRow(
		`select is_admin, password_hash from users where email = ?`, seedAdminEmail,
	).Scan(&adminFlag, &adminHash); err != nil {
		t.Fatalf("read seeded admin: %v", err)
	}
	if adminFlag != 1 {
		t.Errorf("seeded admin is_admin = %d, want 1", adminFlag)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(adminHash), []byte("admin12345"),
	); err != nil {
		t.Errorf("seeded admin password does not verify: %v", err)
	}
	if n := countRows(t, db, "channels"); n != 5 {
		t.Fatalf("channels = %d, want 5", n)
	}
	if n := countRows(t, db, "videos"); n != 10 {
		t.Fatalf("videos = %d, want 10", n)
	}

	rows, err := db.Query(`select id, owner_user_id, user_id from channels`)
	if err != nil {
		t.Fatalf("query channels: %v", err)
	}
	for rows.Next() {
		var id string
		var ownerID string
		var userID int64
		if err := rows.Scan(&id, &ownerID, &userID); err != nil {
			rows.Close()
			t.Fatalf("scan channel: %v", err)
		}
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			t.Errorf("channel ID %q is not numeric: %v", id, err)
		}
		if ownerID != "0" || userID != 0 {
			t.Errorf("channel %s owner/user IDs = %q/%d, want 0/0", id, ownerID, userID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate channels: %v", err)
	}
	rows.Close()

	assertSeedVideoAssociations(t, db)
	if err := seedData(db); err != nil {
		t.Fatalf("second seedData: %v", err)
	}
	if n := countRows(t, db, "users"); n != 1 {
		t.Errorf("users after second seed = %d, want seeded admin only", n)
	}
	if n := countRows(t, db, "channels"); n != 5 {
		t.Errorf("channels after second seed = %d, want 5", n)
	}
	if n := countRows(t, db, "videos"); n != 10 {
		t.Errorf("videos after second seed = %d, want 10", n)
	}
}

func TestSeedPreservesExistingState(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	adminID := insertUser(t, db, "existing-admin@seed.test", "synthetic-seed-password", "Sofia", true)
	insertChannel(t, db, "20", "Unrelated Channel", "Sofia", adminID, false)

	var email, location, passwordHash string
	var isAdmin int
	err := db.QueryRow(
		`select email, location, password_hash, is_admin from users where id = ?`,
		adminID,
	).Scan(&email, &location, &passwordHash, &isAdmin)
	if err != nil {
		t.Fatalf("read existing admin: %v", err)
	}

	if err := seedData(db); err != nil {
		t.Fatalf("seedData: %v", err)
	}
	if err := seedData(db); err != nil {
		t.Fatalf("second seedData: %v", err)
	}

	var afterEmail, afterLocation, afterPasswordHash string
	var afterIsAdmin int
	err = db.QueryRow(
		`select email, location, password_hash, is_admin from users where id = ?`,
		adminID,
	).Scan(&afterEmail, &afterLocation, &afterPasswordHash, &afterIsAdmin)
	if err != nil {
		t.Fatalf("read admin after seed: %v", err)
	}
	if afterEmail != email || afterLocation != location ||
		afterPasswordHash != passwordHash || afterIsAdmin != isAdmin {
		t.Errorf("existing admin changed: before %q/%q/%d, after %q/%q/%d",
			email, location, isAdmin, afterEmail, afterLocation, afterIsAdmin)
	}
	if n := countRows(t, db, "users"); n != 2 {
		t.Errorf("users = %d, want existing admin plus seeded admin", n)
	}

	var unrelatedName string
	if err := db.QueryRow(`select name from channels where id = '20'`).Scan(&unrelatedName); err != nil {
		t.Fatalf("read unrelated channel: %v", err)
	}
	if unrelatedName != "Unrelated Channel" {
		t.Errorf("unrelated channel name = %q", unrelatedName)
	}
	for _, name := range []string{"Creek Walks", "Park Ponds", "Bridges & Paths", "Water Features", "Urban Views"} {
		var id int64
		if err := db.QueryRow(`select id from channels where name = ?`, name).Scan(&id); err != nil {
			t.Fatalf("find seeded channel %q: %v", name, err)
		}
		if id < 21 {
			t.Errorf("seeded channel %q ID = %d, want shifted numeric ID >= 21", name, id)
		}
	}
	if n := countRows(t, db, "channels"); n != 6 {
		t.Errorf("channels = %d, want unrelated plus five seed channels", n)
	}
	if n := countRows(t, db, "videos"); n != 10 {
		t.Errorf("videos = %d, want 10 after repeated seed", n)
	}
	assertSeedVideoAssociations(t, db)
}

func TestSeedRollback(t *testing.T) {
	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema: %v", err)
	}
	if _, err := db.Exec(`
		create trigger fail_seed_video before insert on videos
		begin
			select raise(abort, 'synthetic seed failure');
		end
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := seedData(db)
	if err == nil {
		t.Fatal("seedData error = nil, want synthetic trigger failure")
	}
	if !strings.Contains(err.Error(), "synthetic seed failure") {
		t.Fatalf("seedData error = %v, want synthetic failure", err)
	}
	for _, table := range []string{"users", "channels", "videos"} {
		if n := countRows(t, db, table); n != 0 {
			t.Errorf("%s rows after rollback = %d, want 0", table, n)
		}
	}
}

func TestSeedNewServerFreshDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Close() })

	for table, want := range map[string]int{
		"users": 1, "channels": 5, "videos": 10,
	} {
		if got := countRows(t, s.db, table); got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func assertSeedVideoAssociations(t *testing.T, db *sql.DB) {
	t.Helper()

	want := map[string]string{
		"Winter Creek Flow":      "Creek Walks",
		"Rocky Creek Bend":       "Creek Walks",
		"Ducks on the Pond":      "Park Ponds",
		"Reedy Pond Edge":        "Park Ponds",
		"Bridge Railing Shadows": "Bridges & Paths",
		"Carved Forest Trees":    "Bridges & Paths",
		"Rocky Stream Waterfall": "Water Features",
		"Waterfront Sculptures":  "Urban Views",
		"Sunlit Flooded Woods":   "Urban Views",
		"Parked Car Close-Up":    "Urban Views",
	}
	for title, wantChannel := range want {
		var channelName string
		err := db.QueryRow(`
			select c.name from videos v join channels c on c.id = v.channel_id
			where v.title = ?
		`, title).Scan(&channelName)
		if err != nil {
			t.Errorf("find seed video %q: %v", title, err)
			continue
		}
		if channelName != wantChannel {
			t.Errorf("video %q channel = %q, want %q", title, channelName, wantChannel)
		}
	}
}

func TestCreateChannel(t *testing.T) {
	t.Run("current schema", func(t *testing.T) {
		db := openTestDB(t)
		if err := initSchema(db); err != nil {
			t.Fatalf("initSchema: %v", err)
		}
		ownerID := insertUser(t, db, "channel-helper@seed.test", "synthetic-password", "Sofia", false)
		insertChannel(t, db, "17", "Existing Helper Channel", "Sofia", ownerID, false)

		channelID, err := createChannel(db, "Helper Channel", "Sofia", ownerID)
		if err != nil {
			t.Fatalf("createChannel: %v", err)
		}
		if channelID != 18 {
			t.Fatalf("new channel ID = %d, want 18", channelID)
		}
		var id, ownerIDText string
		var userID int64
		if err := db.QueryRow(
			`select id, owner_user_id, user_id from channels where id = ?`, channelID,
		).Scan(&id, &ownerIDText, &userID); err != nil {
			t.Fatalf("read created channel: %v", err)
		}
		if id != "18" || ownerIDText != strconv.FormatInt(ownerID, 10) || userID != ownerID {
			t.Errorf("created id/owner/user = %q/%q/%d, want %d/%d/%d",
				id, ownerIDText, userID, channelID, ownerID, ownerID)
		}
	})

	t.Run("legacy integer IDs", func(t *testing.T) {
		db := openTestDB(t)
		_, err := db.Exec(`create table channels (
			id integer primary key,
			name text not null,
			location text not null,
			owner_user_id text not null,
			user_id integer not null default 0
		)`)
		if err != nil {
			t.Fatalf("create legacy channels table: %v", err)
		}
		if _, err := db.Exec(
			`insert into channels(id, name, location, owner_user_id, user_id)
			values(17, 'Existing', 'Sofia', '0', 0)`,
		); err != nil {
			t.Fatalf("insert legacy channel: %v", err)
		}

		channelID, err := createChannel(db, "Legacy Helper Channel", "Sofia", 0)
		if err != nil {
			t.Fatalf("createChannel: %v", err)
		}
		if channelID != 18 {
			t.Errorf("new channel ID = %d, want 18", channelID)
		}
	})
}

func TestUploadNewChannel(t *testing.T) {
	s := newTestServerInTempDir(t)
	ownerID := insertUser(t, s.db, "upload-channel@seed.test", "synthetic-password", "Sofia", false)
	cookie := newSessionCookie(t, s, ownerID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"new_channel":    "Upload Channel",
		"title":          "Synthetic Upload",
		"description":    "fixture only",
		"is_public":      "on",
		"allow_comments": "on",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write multipart field %s: %v", key, err)
		}
	}
	file, err := writer.CreateFormFile("video", "synthetic.mp4")
	if err != nil {
		t.Fatalf("create multipart video: %v", err)
	}
	const marker = "SYNTHETIC-UPLOAD-FIXTURE"
	if _, err := file.Write([]byte(marker)); err != nil {
		t.Fatalf("write multipart video: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want 303; body=%q", rec.Code, rec.Body.String())
	}

	var channelID int64
	var ownerUserID int64
	if err := s.db.QueryRow(
		`select id, user_id from channels where name = 'Upload Channel'`,
	).Scan(&channelID, &ownerUserID); err != nil {
		t.Fatalf("read new channel: %v", err)
	}
	var ownerIDText string
	if err := s.db.QueryRow(
		`select owner_user_id from channels where id = ?`, channelID,
	).Scan(&ownerIDText); err != nil {
		t.Fatalf("read channel owner_user_id: %v", err)
	}
	if ownerUserID != ownerID || ownerIDText != strconv.FormatInt(ownerID, 10) {
		t.Errorf("new channel ownership = %d/%q, want %d/%d",
			ownerUserID, ownerIDText, ownerID, ownerID)
	}
	var videoChannelID string
	var mediaURL string
	if err := s.db.QueryRow(
		`select channel_id, media_url from videos where title = 'Synthetic Upload'`,
	).Scan(&videoChannelID, &mediaURL); err != nil {
		t.Fatalf("read uploaded video: %v", err)
	}
	if videoChannelID != strconv.FormatInt(channelID, 10) {
		t.Errorf("video channel_id = %q, want %d", videoChannelID, channelID)
	}
	if mediaURL != fmt.Sprintf("/static/uploads/users/%d/1/original.mp4", ownerID) {
		t.Errorf("media_url = %q", mediaURL)
	}
}
