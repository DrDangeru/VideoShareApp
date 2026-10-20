package app

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// openTestDB returns a single-connection in-memory SQLite database. The
// connection limit must be set before any query so every statement shares
// the same in-memory database and the foreign_keys pragma stays in effect.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		t.Fatalf("enable foreign keys: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// newTestServer builds a Server backed by a fresh in-memory database with
// the full schema applied. It never calls New/seedData and never touches
// the real data/app.db file.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	db := openTestDB(t)
	if err := initSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	s := &Server{db: db, mux: http.NewServeMux()}
	s.routes()
	return s
}

// newTestServerWithTemplates does the same but first switches the test into
// a temporary working directory holding copies of the real templates, so
// handlers that render pages can be exercised.
func newTestServerWithTemplates(t *testing.T, pages ...string) *Server {
	t.Helper()

	// Find the real templates directory relative to this source file, so the
	// lookup works no matter which directory the test runs from.
	_, currentFile, _, found := runtime.Caller(0)
	if !found {
		t.Fatal("runtime.Caller failed")
	}
	appDirectory := filepath.Dir(currentFile)
	projectDirectory := filepath.Join(appDirectory, "..", "..")
	sourceTemplatesDirectory := filepath.Join(projectDirectory, "templates")

	// Give this test a disposable working directory, cleaned up automatically.
	// Changing the working directory means this test must not run in parallel.
	temporaryDirectory := t.TempDir()
	t.Chdir(temporaryDirectory)
	destinationTemplatesDirectory := filepath.Join(temporaryDirectory, "templates")
	err := os.MkdirAll(destinationTemplatesDirectory, 0o755)
	if err != nil {
		t.Fatalf("create temp templates dir: %v", err)
	}

	// Copy the shared layout plus each page the test asked for.
	templateNames := []string{"layout.html"}
	for _, page := range pages {
		templateNames = append(templateNames, page)
	}
	for _, templateName := range templateNames {
		sourcePath := filepath.Join(sourceTemplatesDirectory, templateName)
		contents, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read template %s: %v", templateName, err)
		}
		destinationPath := filepath.Join(destinationTemplatesDirectory, templateName)
		err = os.WriteFile(destinationPath, contents, 0o644)
		if err != nil {
			t.Fatalf("write template %s: %v", templateName, err)
		}
	}

	server := newTestServer(t)
	return server
}

// newTestServerInTempDir switches into an empty temporary working directory
// so tests can create their own Videos/ and static/ fixture trees without
// touching the real ones.
func newTestServerInTempDir(t *testing.T) *Server {
	t.Helper()
	t.Chdir(t.TempDir())
	return newTestServer(t)
}

func insertUser(t *testing.T, db *sql.DB, email, password, location string, isAdmin bool) int64 {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := db.Exec(
		`insert into users(email, password_hash, location, is_admin) values(?, ?, ?, ?)`,
		email, string(hash), location, admin,
	)
	if err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	return id
}

func insertChannel(
	t *testing.T,
	db *sql.DB,
	id, name, location string,
	ownerID int64,
	adminLocked bool,
) {
	t.Helper()

	locked := 0
	if adminLocked {
		locked = 1
	}
	_, err := db.Exec(`insert into channels(
		id, name, location, description, avatar_url, owner_user_id, user_id, is_admin_locked
	) values(?, ?, ?, '', '', ?, ?, ?)`,
		id, name, location, strconv.FormatInt(ownerID, 10), ownerID, locked,
	)
	if err != nil {
		t.Fatalf("insert channel %s: %v", id, err)
	}
}

type testVideoRow struct {
	id          int64
	channelID   string
	title       string
	description string
	mediaURL    string
	thumbURL    string
	location    string
	userID      int64
	isPublic    int
	adminLocked int
}

func insertVideo(t *testing.T, db *sql.DB, v testVideoRow) {
	t.Helper()

	_, err := db.Exec(`insert into videos(
		id, channel_id, title, description, media_url, thumbnail_url,
		location, user_id, is_public, is_admin_locked
	) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.id, v.channelID, v.title, v.description, v.mediaURL, v.thumbURL,
		v.location, v.userID, v.isPublic, v.adminLocked,
	)
	if err != nil {
		t.Fatalf("insert video %d: %v", v.id, err)
	}
}

func newSessionCookie(t *testing.T, s *Server, userID int64) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := s.createSession(rec, userID); err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := findCookie(rec, "session_token")
	if cookie == nil {
		t.Fatal("createSession did not set session_token cookie")
	}
	return cookie
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func doRequest(
	t *testing.T,
	s *Server,
	method, target string,
	form url.Values,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	if err := db.QueryRow(`select count(*) from ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func writeTestFile(t *testing.T, relPath string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(relPath), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(relPath, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}
