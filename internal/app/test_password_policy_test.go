package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRegistrationPasswordByteAndCharacterLimits(t *testing.T) {
	cases := []struct {
		name     string
		password string
		valid    bool
	}{
		{"seven characters", "1234567", false},
		{"minimum eight characters", "12345678", true},
		{"maximum 72 bytes", strings.Repeat("a", 72), true},
		{"over 72 bytes", strings.Repeat("a", 73), false},
		{"eight multibyte characters", strings.Repeat("é", 8), true},
		{"multibyte password over 72 bytes", strings.Repeat("é", 37), false},
		{"all whitespace", strings.Repeat(" ", 8), false},
		{"invalid UTF-8", string([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8}), false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServerWithTemplates(t, "auth.html")
			rec := doRequest(t, s, http.MethodPost, "/register", url.Values{
				"email":    {fmt.Sprintf("password-%d@password.test", index)},
				"password": {tc.password},
				"location": {"Sofia"},
			})
			wantStatus := http.StatusBadRequest
			if tc.valid {
				wantStatus = http.StatusSeeOther
			}
			if rec.Code != wantStatus {
				t.Fatalf("registration status = %d, want %d; body=%q", rec.Code, wantStatus, rec.Body.String())
			}
			wantUsers, wantSessions := 0, 0
			if tc.valid {
				wantUsers, wantSessions = 1, 1
			}
			if got := countRows(t, s.db, "users"); got != wantUsers {
				t.Errorf("user count = %d, want %d", got, wantUsers)
			}
			if got := countRows(t, s.db, "sessions"); got != wantSessions {
				t.Errorf("session count = %d, want %d", got, wantSessions)
			}
		})
	}
}

func TestLoginKeepsLegacyShortPasswordsAndRejectsOverlong(t *testing.T) {
	s := newTestServerWithTemplates(t, "auth.html")
	insertUser(t, s.db, "legacy-short@password.test", "legacy", "Sofia", false)

	loginPage := doRequest(t, s, http.MethodGet, "/login", nil)
	if loginPage.Code != http.StatusOK {
		t.Fatalf("login page status = %d, want 200", loginPage.Code)
	}
	if strings.Contains(loginPage.Body.String(), `name="password" required minlength=`) {
		t.Error("login page imposes a password minimum")
	}
	registerPage := doRequest(t, s, http.MethodGet, "/register", nil)
	if !strings.Contains(registerPage.Body.String(), `name="password" required minlength="8"`) {
		t.Error("registration page is missing the eight-character hint")
	}

	rec := doRequest(t, s, http.MethodPost, "/login", url.Values{
		"email":    {"legacy-short@password.test"},
		"password": {"legacy"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy short-password login status = %d, want 303", rec.Code)
	}
	if countRows(t, s.db, "sessions") != 1 {
		t.Fatal("legacy short-password login did not create a session")
	}

	insertUser(t, s.db, "overlong-login@password.test", "valid-password", "Sofia", false)
	rec = doRequest(t, s, http.MethodPost, "/login", url.Values{
		"email":    {"overlong-login@password.test"},
		"password": {strings.Repeat("a", 73)},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid credentials.") {
		t.Errorf("overlong login response = %d %q, want normal invalid credentials", rec.Code, rec.Body.String())
	}
	if countRows(t, s.db, "sessions") != 1 {
		t.Error("overlong login created a session")
	}
}
