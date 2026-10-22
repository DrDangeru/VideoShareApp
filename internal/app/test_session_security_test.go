package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionLifetimeAndOpportunisticPurge(t *testing.T) {
	s := newTestServer(t)
	userID := insertUser(t, s.db, "session-lifetime@security.test", "synthetic-password", "Sofia", false)
	now := time.Now().UTC().Truncate(time.Second)
	createdAt := map[string]any{
		"fresh":     now.Format(time.DateTime),
		"23-hours":  now.Add(-23 * time.Hour).Format(time.DateTime),
		"25-hours":  now.Add(-25 * time.Hour).Format(time.DateTime),
		"malformed": "not-a-date",
		"future":    now.Add(time.Hour).Format(time.DateTime),
		"null":      nil,
	}
	for token, timestamp := range createdAt {
		if _, err := s.db.Exec(
			`insert into sessions(token, user_id, created_at) values(?, ?, ?)`,
			token, userID, timestamp,
		); err != nil {
			t.Fatalf("insert %s session: %v", token, err)
		}
	}

	for _, token := range []string{"fresh", "23-hours"} {
		user, err := s.currentUser(sessionRequest(token))
		if err != nil || user.ID != userID {
			t.Errorf("%s session user = %v, error = %v; want user %d", token, user, err, userID)
		}
	}
	for _, token := range []string{"25-hours", "malformed", "future", "null"} {
		if user, err := s.currentUser(sessionRequest(token)); err == nil {
			t.Errorf("%s session unexpectedly authenticated as user %d", token, user.ID)
		}
	}

	rec := httptest.NewRecorder()
	if err := s.createSession(rec, userID); err != nil {
		t.Fatalf("create fresh session: %v", err)
	}
	cookie := findCookie(rec, "session_token")
	if cookie == nil {
		t.Fatal("createSession did not set a session cookie")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Errorf("cookie flags = HttpOnly:%v SameSite:%v Path:%q", cookie.HttpOnly, cookie.SameSite, cookie.Path)
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Errorf("session cookie is persistent: MaxAge=%d Expires=%v", cookie.MaxAge, cookie.Expires)
	}
	if user, err := s.currentUser(sessionRequest(cookie.Value)); err != nil || user.ID != userID {
		t.Errorf("new session user = %v, error = %v; want user %d", user, err, userID)
	}

	for _, token := range []string{"fresh", "23-hours", cookie.Value} {
		var count int
		if err := s.db.QueryRow(`select count(*) from sessions where token = ?`, token).Scan(&count); err != nil {
			t.Fatalf("check retained %s session: %v", token, err)
		}
		if count != 1 {
			t.Errorf("retained session %q count = %d, want 1", token, count)
		}
	}
	for _, token := range []string{"25-hours", "malformed", "future", "null"} {
		var count int
		if err := s.db.QueryRow(`select count(*) from sessions where token = ?`, token).Scan(&count); err != nil {
			t.Fatalf("check purged %s session: %v", token, err)
		}
		if count != 0 {
			t.Errorf("purged session %q count = %d, want 0", token, count)
		}
	}
}

func TestCurrentUserFailsClosedOnDatabaseError(t *testing.T) {
	s := newTestServer(t)
	userID := insertUser(t, s.db, "closed-db-session@security.test", "synthetic-password", "Sofia", false)
	cookie := newSessionCookie(t, s, userID)
	if err := s.db.Close(); err != nil {
		t.Fatalf("close disposable database: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	if user, err := s.currentUser(request); err == nil || user != nil {
		t.Errorf("currentUser = %v, error = %v; want closed-database failure", user, err)
	}
	if user, err := s.requireUser(request); err == nil || user != nil {
		t.Errorf("requireUser = %v, error = %v; want authentication failure", user, err)
	}
}

func TestCreateSessionRollsBackPurgeWhenInsertFails(t *testing.T) {
	s := newTestServer(t)
	userID := insertUser(t, s.db, "session-rollback@security.test", "synthetic-password", "Sofia", false)
	const expiredToken = "synthetic-expired-session"
	createdAt := time.Now().UTC().Add(-sessionLifetime - time.Hour).Format(time.DateTime)
	if _, err := s.db.Exec(
		`insert into sessions(token, user_id, created_at) values(?, ?, ?)`,
		expiredToken,
		userID,
		createdAt,
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	rec := httptest.NewRecorder()
	if err := s.createSession(rec, userID+1_000_000); err == nil {
		t.Fatal("createSession unexpectedly accepted a nonexistent user")
	}
	if cookie := findCookie(rec, "session_token"); cookie != nil {
		t.Fatal("failed createSession set a session cookie")
	}
	var retained int
	if err := s.db.QueryRow(`select count(*) from sessions where token = ?`, expiredToken).Scan(&retained); err != nil {
		t.Fatalf("check purged session after rollback: %v", err)
	}
	if retained != 1 {
		t.Errorf("expired session count after rollback = %d, want 1", retained)
	}
	if countRows(t, s.db, "sessions") != 1 {
		t.Error("failed createSession changed the session table")
	}
}

func sessionRequest(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	return request
}
