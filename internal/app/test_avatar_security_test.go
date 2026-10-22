package app

import (
	"bytes"
	"encoding/base64"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelAvatarValidationAndStorage(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format+" normalized to JPEG", func(t *testing.T) {
			s, cookie := newAvatarTestServer(t)
			rec := postChannelEdit(t, s, cookie, url.Values{
				"name":        {"Updated Avatar Channel"},
				"location":    {"Sofia"},
				"description": {"Synthetic avatar fixture"},
				"avatar":      {testThumbnailDataURL(t, format, 3, 2)},
			})
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("edit status = %d, want 303; body=%q", rec.Code, rec.Body.String())
			}

			wantURL := "/static/uploads/channels/101/avatar.jpg"
			var avatarURL string
			if err := s.db.QueryRow(`select avatar_url from channels where id = '101'`).Scan(&avatarURL); err != nil {
				t.Fatalf("read avatar URL: %v", err)
			}
			if avatarURL != wantURL {
				t.Fatalf("avatar URL = %q, want %q", avatarURL, wantURL)
			}
			avatar := doRequest(t, s, http.MethodGet, wantURL, nil, cookie)
			if avatar.Code != http.StatusOK {
				t.Fatalf("GET avatar status = %d, want 200", avatar.Code)
			}
			config, imageFormat, err := image.DecodeConfig(bytes.NewReader(avatar.Body.Bytes()))
			if err != nil {
				t.Fatalf("decode served avatar: %v", err)
			}
			if imageFormat != "jpeg" || config.Width != 3 || config.Height != 2 {
				t.Errorf("served avatar = %s %dx%d, want JPEG 3x2", imageFormat, config.Width, config.Height)
			}
		})
	}
}

func TestChannelAvatarInvalidInputHasNoSideEffects(t *testing.T) {
	validPNG := testThumbnailDataURL(t, "png", 2, 2)
	invalid := []struct {
		name string
		data string
	}{
		{"bad base64", "data:image/jpeg;base64,%%%"},
		{"fake HTML", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("<html>not an image</html>"))},
		{"declared type mismatch", "data:image/jpeg;base64," + strings.TrimPrefix(validPNG, "data:image/png;base64,")},
		{"truncated image", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff})},
		{
			"oversize image",
			"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(make([]byte, maxThumbnailBytes+1)),
		},
		{"dimensions over limit", testThumbnailDataURL(t, "png", 4097, 1)},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			s, cookie := newAvatarTestServer(t)
			if _, err := s.db.Exec(
				`update channels set name = 'Original Channel', location = 'Original Location',
					description = 'Original Description', avatar_url = '/static/original-avatar.jpg'
					where id = '101'`,
			); err != nil {
				t.Fatalf("set original channel state: %v", err)
			}
			rec := postChannelEdit(t, s, cookie, url.Values{
				"name":        {"Changed Channel"},
				"location":    {"Changed Location"},
				"description": {"Changed Description"},
				"avatar":      {tc.data},
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("edit status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			assertAvatarChannelState(t, s, "Original Channel", "Original Location", "Original Description", "/static/original-avatar.jpg")
			if _, err := os.Stat(filepath.Join("static", "uploads")); !os.IsNotExist(err) {
				t.Errorf("uploads path stat error = %v, want not-exist", err)
			}
		})
	}
}

func TestChannelAvatarPreserveRemoveAndWriteFailure(t *testing.T) {
	t.Run("no new image preserves avatar", func(t *testing.T) {
		s, cookie := newAvatarTestServer(t)
		if _, err := s.db.Exec(`update channels set avatar_url = '/static/existing-avatar.jpg' where id = '101'`); err != nil {
			t.Fatalf("set existing avatar: %v", err)
		}
		rec := postChannelEdit(t, s, cookie, url.Values{
			"name":        {"Preserved Avatar Channel"},
			"location":    {"Sofia"},
			"description": {"Metadata updated without an avatar"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("edit status = %d, want 303", rec.Code)
		}
		assertAvatarChannelState(t, s, "Preserved Avatar Channel", "Sofia", "Metadata updated without an avatar", "/static/existing-avatar.jpg")
	})

	t.Run("remove clears database URL but retains file", func(t *testing.T) {
		s, cookie := newAvatarTestServer(t)
		const existing = "/static/uploads/channels/101/avatar.jpg"
		const marker = "SYNTHETIC-EXISTING-AVATAR"
		writeTestFile(t, filepath.FromSlash(strings.TrimPrefix(existing, "/")), []byte(marker))
		if _, err := s.db.Exec(`update channels set avatar_url = ? where id = '101'`, existing); err != nil {
			t.Fatalf("set existing avatar: %v", err)
		}
		rec := postChannelEdit(t, s, cookie, url.Values{
			"name":          {"Avatar Removed Channel"},
			"location":      {"Sofia"},
			"description":   {"Avatar removed"},
			"remove_avatar": {"1"},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("edit status = %d, want 303", rec.Code)
		}
		assertAvatarChannelState(t, s, "Avatar Removed Channel", "Sofia", "Avatar removed", "")
		content, err := os.ReadFile(filepath.FromSlash(strings.TrimPrefix(existing, "/")))
		if err != nil {
			t.Fatalf("read retained avatar file: %v", err)
		}
		if string(content) != marker {
			t.Errorf("retained avatar content = %q, want %q", content, marker)
		}
	})

	t.Run("remove and replacement are rejected before metadata changes", func(t *testing.T) {
		s, cookie := newAvatarTestServer(t)
		if _, err := s.db.Exec(
			`update channels set name = 'Original Channel', location = 'Original Location',
				description = 'Original Description', avatar_url = '/static/original-avatar.jpg'
				where id = '101'`,
		); err != nil {
			t.Fatalf("set original channel state: %v", err)
		}
		rec := postChannelEdit(t, s, cookie, url.Values{
			"name":          {"Changed Channel"},
			"location":      {"Changed Location"},
			"description":   {"Changed Description"},
			"avatar":        {testThumbnailDataURL(t, "png", 2, 2)},
			"remove_avatar": {"1"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("edit status = %d, want 400", rec.Code)
		}
		assertAvatarChannelState(t, s, "Original Channel", "Original Location", "Original Description", "/static/original-avatar.jpg")
	})

	t.Run("write failure returns 500 before metadata changes", func(t *testing.T) {
		s, cookie := newAvatarTestServer(t)
		target := filepath.Join("static", "uploads", "channels", "101", "avatar.jpg")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("create directory at avatar path: %v", err)
		}
		rec := postChannelEdit(t, s, cookie, url.Values{
			"name":        {"Changed Channel"},
			"location":    {"Changed Location"},
			"description": {"Changed Description"},
			"avatar":      {testThumbnailDataURL(t, "jpeg", 2, 2)},
		})
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("edit status = %d, want 500", rec.Code)
		}
		assertAvatarChannelState(t, s, "Test Avatar Channel", "Sofia", "", "")
	})
}

func TestChannelAvatarAuthorizationAndTemplateContract(t *testing.T) {
	s := newTestServerWithTemplates(t, "edit_channel.html")
	ownerID := insertUser(t, s.db, "avatar-contract-owner@security.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, s.db, "avatar-contract-other@security.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, s.db, "avatar-contract-admin@security.test", "synthetic-password", "Sofia", true)
	insertChannel(t, s.db, "101", "Test Avatar Channel", "Sofia", ownerID, false)
	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)

	page := doRequest(t, s, http.MethodGet, "/edit-channel/101", nil, ownerCookie)
	if page.Code != http.StatusOK {
		t.Fatalf("owner edit page status = %d, want 200", page.Code)
	}
	body := page.Body.String()
	for _, expected := range []string{
		`name="avatar" id="avatar-data"`,
		`name="remove_avatar" id="remove-avatar"`,
		`id="avatar-preview"`,
		`id="avatar-select"`,
		`id="avatar-remove"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("edit page missing %q", expected)
		}
	}
	for _, stale := range []string{"avatar_base64", "handleAvatarSelect", "clearAvatar", `src=""`} {
		if strings.Contains(body, stale) {
			t.Errorf("edit page contains stale avatar contract %q", stale)
		}
	}

	other := postChannelEdit(t, s, otherCookie, url.Values{
		"name":   {"Unauthorized Change"},
		"avatar": {testThumbnailDataURL(t, "png", 2, 2)},
	})
	if other.Code != http.StatusNotFound {
		t.Fatalf("other user edit status = %d, want 404", other.Code)
	}
	assertAvatarChannelState(t, s, "Test Avatar Channel", "Sofia", "", "")
	if _, err := os.Stat(filepath.Join("static", "uploads")); !os.IsNotExist(err) {
		t.Errorf("unauthorized avatar upload path stat error = %v, want not-exist", err)
	}

	admin := postChannelEdit(t, s, adminCookie, url.Values{
		"avatar":          {"not-a-valid-image"},
		"is_admin_locked": {"1"},
	})
	if admin.Code != http.StatusSeeOther {
		t.Fatalf("admin moderation status = %d, want 303", admin.Code)
	}
	var locked int
	if err := s.db.QueryRow(`select is_admin_locked from channels where id = '101'`).Scan(&locked); err != nil {
		t.Fatalf("read admin lock: %v", err)
	}
	if locked != 1 {
		t.Errorf("admin lock = %d, want 1", locked)
	}
	assertAvatarChannelState(t, s, "Test Avatar Channel", "Sofia", "", "")
}

func newAvatarTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	s := newTestServerInTempDir(t)
	ownerID := insertUser(t, s.db, "avatar-owner@security.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "101", "Test Avatar Channel", "Sofia", ownerID, false)
	return s, newSessionCookie(t, s, ownerID)
}

func postChannelEdit(t *testing.T, s *Server, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodPost, "/edit-channel/101", form, cookie)
}

func assertAvatarChannelState(t *testing.T, s *Server, name, location, description, avatarURL string) {
	t.Helper()
	var gotName, gotLocation, gotDescription, gotAvatarURL string
	if err := s.db.QueryRow(
		`select name, location, description, avatar_url from channels where id = '101'`,
	).Scan(&gotName, &gotLocation, &gotDescription, &gotAvatarURL); err != nil {
		t.Fatalf("read channel state: %v", err)
	}
	if gotName != name || gotLocation != location || gotDescription != description || gotAvatarURL != avatarURL {
		t.Errorf("channel state = %q/%q/%q/%q, want %q/%q/%q/%q",
			gotName, gotLocation, gotDescription, gotAvatarURL,
			name, location, description, avatarURL)
	}
}
