package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestProtectedRoutesRedirectToLogin(t *testing.T) {
	s := newTestServer(t)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/dashboard"},
		{http.MethodGet, "/edit-video/1"},
		{http.MethodPost, "/upload"},
		{http.MethodPost, "/videos/1/favorite"},
	} {
		rec := doRequest(t, s, tc.method, tc.path, nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("%s %s: status = %d, want 303", tc.method, tc.path, rec.Code)
			continue
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Errorf("%s %s: redirect = %q, want /login", tc.method, tc.path, loc)
		}
	}
}

func TestNotFoundAndMethodRoutes(t *testing.T) {
	s := newTestServer(t)

	notFound := []string{"/no-such-page", "/watch/not-a-number", "/watch/999"}
	for _, path := range notFound {
		rec := doRequest(t, s, http.MethodGet, path, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}

	methodNotAllowed := []string{"/upload", "/videos/1/favorite"}
	for _, path := range methodNotAllowed {
		rec := doRequest(t, s, http.MethodGet, path, nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status = %d, want 405", path, rec.Code)
		}
	}
}

func TestFavorite(t *testing.T) {
	s := newTestServer(t)
	db := s.db

	ownerID := insertUser(t, db, "owner@fav.test", "pw", "Sofia", false)
	fanID := insertUser(t, db, "fan@fav.test", "pw", "Sofia", false)
	insertChannel(t, db, "1", "Fav Channel", "Sofia", ownerID, false)
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Fav Vid", description: "d",
		mediaURL: "/media/fav.mp4", thumbURL: "/media/fav.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})

	cookie := newSessionCookie(t, s, fanID)

	rec := doRequest(t, s, http.MethodPost, "/videos/1/favorite", nil, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("favorite status = %d, want 303", rec.Code)
	}
	if n := countRows(t, db, "favorites"); n != 1 {
		t.Fatalf("favorites = %d, want 1", n)
	}

	rec = doRequest(t, s, http.MethodPost, "/videos/1/favorite", nil, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("repeat favorite status = %d, want 303", rec.Code)
	}
	if n := countRows(t, db, "favorites"); n != 1 {
		t.Fatalf("favorites after repeat = %d, want 1", n)
	}

	for _, path := range []string{"/videos/abc/favorite", "/videos/1/unfavorite"} {
		rec := doRequest(t, s, http.MethodPost, path, nil, cookie)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", path, rec.Code)
		}
	}
	if n := countRows(t, db, "favorites"); n != 1 {
		t.Errorf("favorites after malformed posts = %d, want 1", n)
	}
}

func TestWatchPermissions(t *testing.T) {
	s := newTestServerWithTemplates(t, "watch.html")
	db := s.db

	ownerID := insertUser(t, db, "owner@watch.test", "pw", "Sofia", false)
	otherID := insertUser(t, db, "other@watch.test", "pw", "Sofia", false)
	adminID := insertUser(t, db, "admin@watch.test", "pw", "Sofia", true)

	insertChannel(t, db, "1", "Watch Channel", "Sofia", ownerID, false)
	insertChannel(t, db, "2", "Locked Channel", "Sofia", ownerID, true)

	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Unique Public Watch Title", description: "d",
		mediaURL: "/media/w1.mp4", thumbURL: "/media/w1.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 2, channelID: "1", title: "Unique Private Watch Title", description: "d",
		mediaURL: "/media/w2.mp4", thumbURL: "/media/w2.jpg",
		location: "Sofia", userID: ownerID, isPublic: 0,
	})
	insertVideo(t, db, testVideoRow{
		id: 3, channelID: "1", title: "Unique Locked Watch Title", description: "d",
		mediaURL: "/media/w3.mp4", thumbURL: "/media/w3.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 4, channelID: "2", title: "Unique ChanLocked Watch Title", description: "d",
		mediaURL: "/media/w4.mp4", thumbURL: "/media/w4.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})

	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)

	cases := []struct {
		name   string
		path   string
		cookie *http.Cookie
		status int
		title  string
	}{
		{"public anonymous", "/watch/1", nil, http.StatusOK, "Unique Public Watch Title"},
		{"public other", "/watch/1", otherCookie, http.StatusOK, "Unique Public Watch Title"},
		{"private anonymous", "/watch/2", nil, http.StatusNotFound, ""},
		{"private other", "/watch/2", otherCookie, http.StatusNotFound, ""},
		{"private owner", "/watch/2", ownerCookie, http.StatusOK, "Unique Private Watch Title"},
		{"private admin", "/watch/2", adminCookie, http.StatusOK, "Unique Private Watch Title"},
		{"video locked anonymous", "/watch/3", nil, http.StatusNotFound, ""},
		{"video locked other", "/watch/3", otherCookie, http.StatusNotFound, ""},
		{"video locked owner", "/watch/3", ownerCookie, http.StatusNotFound, ""},
		{"video locked admin", "/watch/3", adminCookie, http.StatusOK, "Unique Locked Watch Title"},
		{"channel locked anonymous", "/watch/4", nil, http.StatusNotFound, ""},
		{"channel locked other", "/watch/4", otherCookie, http.StatusNotFound, ""},
		{"channel locked owner", "/watch/4", ownerCookie, http.StatusNotFound, ""},
		{"channel locked admin", "/watch/4", adminCookie, http.StatusOK, "Unique ChanLocked Watch Title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, tc.path, nil, cookies...)
			if rec.Code != tc.status {
				t.Fatalf("GET %s status = %d, want %d", tc.path, rec.Code, tc.status)
			}
			if tc.status == http.StatusOK && !strings.Contains(rec.Body.String(), tc.title) {
				t.Errorf("body missing title %q", tc.title)
			}
		})
	}
}
