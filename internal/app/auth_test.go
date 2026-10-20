package app

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestRegister(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")

		rec := doRequest(t, s, http.MethodPost, "/register", url.Values{
			"email":    {"  newuser@test.com  "},
			"password": {"secret-pass"},
			"location": {"  Sofia  "},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Fatalf("redirect = %q, want /", loc)
		}

		cookie := findCookie(rec, "session_token")
		if cookie == nil {
			t.Fatal("no session_token cookie set")
		}
		if len(cookie.Value) != 64 {
			t.Errorf("session token length = %d, want 64", len(cookie.Value))
		}
		if _, err := hex.DecodeString(cookie.Value); err != nil {
			t.Errorf("session token not hex: %v", err)
		}
		if cookie.Path != "/" {
			t.Errorf("cookie Path = %q, want /", cookie.Path)
		}
		if !cookie.HttpOnly {
			t.Error("cookie not HttpOnly")
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
		}

		var email, location, hash string
		var isAdmin int
		err := s.db.QueryRow(
			`select email, location, password_hash, is_admin from users`,
		).Scan(&email, &location, &hash, &isAdmin)
		if err != nil {
			t.Fatalf("read user: %v", err)
		}
		if email != "newuser@test.com" {
			t.Errorf("email = %q, want trimmed newuser@test.com", email)
		}
		if location != "Sofia" {
			t.Errorf("location = %q, want trimmed Sofia", location)
		}
		if hash == "secret-pass" {
			t.Error("password stored as plaintext")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("secret-pass")); err != nil {
			t.Errorf("stored hash does not match password: %v", err)
		}
		if isAdmin != 0 {
			t.Error("registered user should not be admin")
		}
		if n := countRows(t, s.db, "sessions"); n != 1 {
			t.Errorf("sessions = %d, want 1", n)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		user, err := s.currentUser(req)
		if err != nil {
			t.Fatalf("currentUser with issued cookie: %v", err)
		}
		if user.Email != "newuser@test.com" {
			t.Errorf("currentUser email = %q, want newuser@test.com", user.Email)
		}
		if user.IsAdmin {
			t.Error("currentUser should not be admin")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		cases := map[string]url.Values{
			"email":    {"password": {"pw123456"}, "location": {"Sofia"}},
			"password": {"email": {"a@b.com"}, "location": {"Sofia"}},
			"location": {"email": {"a@b.com"}, "password": {"pw123456"}},
		}
		for field, form := range cases {
			t.Run(field, func(t *testing.T) {
				s := newTestServerWithTemplates(t, "auth.html")
				rec := doRequest(t, s, http.MethodPost, "/register", form)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
				if !strings.Contains(rec.Body.String(), "All fields are required.") {
					t.Error("body lacks required-fields error")
				}
				if n := countRows(t, s.db, "users"); n != 0 {
					t.Errorf("users = %d, want 0", n)
				}
				if n := countRows(t, s.db, "sessions"); n != 0 {
					t.Errorf("sessions = %d, want 0", n)
				}
				if findCookie(rec, "session_token") != nil {
					t.Error("session cookie should not be set")
				}
			})
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")
		form := url.Values{
			"email":    {"dup@test.com"},
			"password": {"pw123456"},
			"location": {"Sofia"},
		}
		if rec := doRequest(t, s, http.MethodPost, "/register", form); rec.Code != http.StatusSeeOther {
			t.Fatalf("first register status = %d, want 303", rec.Code)
		}

		rec := doRequest(t, s, http.MethodPost, "/register", form)
		if rec.Code != http.StatusOK {
			t.Fatalf("duplicate register status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Email already exists or is invalid.") {
			t.Error("duplicate register body lacks existing-email error")
		}
		if findCookie(rec, "session_token") != nil {
			t.Error("duplicate register should not set session cookie")
		}
		if n := countRows(t, s.db, "users"); n != 1 {
			t.Errorf("users = %d, want 1", n)
		}
		if n := countRows(t, s.db, "sessions"); n != 1 {
			t.Errorf("sessions = %d, want 1", n)
		}
	})

	t.Run("unsupported methods", func(t *testing.T) {
		s := newTestServer(t)
		for _, target := range []string{"/register", "/login"} {
			rec := doRequest(t, s, http.MethodPut, target, nil)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("PUT %s: status = %d, want 405", target, rec.Code)
			}
		}
	})
}

func TestLogin(t *testing.T) {
	const password = "login-pass-1"

	t.Run("valid credentials", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")
		uid := insertUser(t, s.db, "login@test.com", password, "Sofia", false)

		rec := doRequest(t, s, http.MethodPost, "/login", url.Values{
			"email":    {"login@test.com"},
			"password": {password},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Fatalf("redirect = %q, want /", loc)
		}
		cookie := findCookie(rec, "session_token")
		if cookie == nil {
			t.Fatal("no session_token cookie set")
		}
		if n := countRows(t, s.db, "sessions"); n != 1 {
			t.Fatalf("sessions = %d, want 1", n)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		user, err := s.currentUser(req)
		if err != nil {
			t.Fatalf("currentUser: %v", err)
		}
		if user.ID != uid {
			t.Errorf("currentUser ID = %d, want %d", user.ID, uid)
		}
	})

	t.Run("email with surrounding spaces", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")
		uid := insertUser(t, s.db, "spaced@test.com", password, "Sofia", false)

		rec := doRequest(t, s, http.MethodPost, "/login", url.Values{
			"email":    {"  spaced@test.com  "},
			"password": {password},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		cookie := findCookie(rec, "session_token")
		if cookie == nil {
			t.Fatal("no session_token cookie set")
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		user, err := s.currentUser(req)
		if err != nil {
			t.Fatalf("currentUser: %v", err)
		}
		if user.ID != uid {
			t.Errorf("currentUser ID = %d, want %d", user.ID, uid)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")
		insertUser(t, s.db, "wrongpw@test.com", password, "Sofia", false)

		rec := doRequest(t, s, http.MethodPost, "/login", url.Values{
			"email":    {"wrongpw@test.com"},
			"password": {"not-the-password"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Invalid credentials.") {
			t.Error("body lacks invalid-credentials error")
		}
		if findCookie(rec, "session_token") != nil {
			t.Error("session cookie should not be set")
		}
		if n := countRows(t, s.db, "sessions"); n != 0 {
			t.Errorf("sessions = %d, want 0", n)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		s := newTestServerWithTemplates(t, "auth.html")

		rec := doRequest(t, s, http.MethodPost, "/login", url.Values{
			"email":    {"ghost@test.com"},
			"password": {password},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Invalid credentials.") {
			t.Error("body lacks invalid-credentials error")
		}
		if findCookie(rec, "session_token") != nil {
			t.Error("session cookie should not be set")
		}
		if n := countRows(t, s.db, "sessions"); n != 0 {
			t.Errorf("sessions = %d, want 0", n)
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("valid session", func(t *testing.T) {
		s := newTestServer(t)
		uid := insertUser(t, s.db, "logout@test.com", "pw", "Sofia", false)
		cookie := newSessionCookie(t, s, uid)

		rec := doRequest(t, s, http.MethodGet, "/logout", nil, cookie)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Fatalf("redirect = %q, want /", loc)
		}
		cleared := findCookie(rec, "session_token")
		if cleared == nil {
			t.Fatal("logout did not set clearing cookie")
		}
		if cleared.MaxAge >= 0 {
			t.Errorf("cleared cookie MaxAge = %d, want < 0", cleared.MaxAge)
		}
		if n := countRows(t, s.db, "sessions"); n != 0 {
			t.Errorf("sessions = %d, want 0 after logout", n)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		if _, err := s.currentUser(req); err == nil {
			t.Error("replayed old cookie should not resolve a user")
		}
	})

	t.Run("without cookie", func(t *testing.T) {
		s := newTestServer(t)

		rec := doRequest(t, s, http.MethodGet, "/logout", nil)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		cleared := findCookie(rec, "session_token")
		if cleared == nil {
			t.Fatal("logout did not set clearing cookie")
		}
		if cleared.MaxAge >= 0 {
			t.Errorf("cleared cookie MaxAge = %d, want < 0", cleared.MaxAge)
		}
	})
}

func TestSessionLookup(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := s.currentUser(req); err == nil {
		t.Error("missing cookie should not resolve a user")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: strings.Repeat("a", 64)})
	if _, err := s.currentUser(req); err == nil {
		t.Error("unknown token should not resolve a user")
	}
}
