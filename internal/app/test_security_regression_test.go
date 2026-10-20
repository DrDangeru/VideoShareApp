package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestSecurityMediaFailClosed(t *testing.T) {
	s := newTestServerInTempDir(t)
	db := s.db
	ownerID := insertUser(t, db, "media-security@security.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "media-security-admin@security.test", "synthetic-password", "Sofia", true)
	insertChannel(t, db, "1", "Media Security Channel", "Sofia", ownerID, false)

	const mediaMarker = "SYNTHETIC-REGISTERED-MEDIA"
	const uploadMarker = "SYNTHETIC-REGISTERED-UPLOAD"
	const unknownMarker = "SYNTHETIC-UNREGISTERED-FILE"
	const staticMarker = "SYNTHETIC-PUBLIC-STATIC"
	uploadURL := fmt.Sprintf("/static/uploads/users/%d/1/original.mp4", ownerID)
	writeTestFile(t, "Videos/registered.mp4", []byte(mediaMarker))
	writeTestFile(t, "Videos/unregistered.mp4", []byte(unknownMarker))
	writeTestFile(t, "static/uploads/users/"+strconv.FormatInt(ownerID, 10)+"/1/original.mp4",
		[]byte(uploadMarker))
	writeTestFile(t, "static/uploads/users/"+strconv.FormatInt(ownerID, 10)+"/99/original.mp4",
		[]byte(unknownMarker))
	writeTestFile(t, "static/img/security-probe.svg", []byte(staticMarker))
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Registered", description: "fixture",
		mediaURL: "/media/registered.mp4", thumbURL: uploadURL,
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	ownerCookie := newSessionCookie(t, s, ownerID)
	adminCookie := newSessionCookie(t, s, adminID)

	for _, target := range []string{
		"/media/unregistered.mp4",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/99/original.mp4",
		"/media/REGISTERED.MP4",
		"/static/UPLOADS/users/" + strconv.FormatInt(ownerID, 10) + "/1/original.mp4",
		"/static/uploads",
		"/static/uploads/",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/",
		"/media/registered.mp4.",
		"/media/registered.mp4%20",
		"/media/dir%5Cregistered.mp4",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/original.mp4.",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/original.mp4%20",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "%5C1/original.mp4",
		"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/original%3A.mp4",
	} {
		rec := doRequest(t, s, http.MethodGet, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", target, rec.Code)
		}
		for _, marker := range []string{mediaMarker, uploadMarker, unknownMarker} {
			if strings.Contains(rec.Body.String(), marker) {
				t.Errorf("GET %s leaked marker %q", target, marker)
			}
		}
	}
	for _, tc := range []struct {
		path   string
		cookie *http.Cookie
	}{
		{"/media/unregistered.mp4", ownerCookie},
		{"/media/unregistered.mp4", adminCookie},
		{"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/99/original.mp4", ownerCookie},
		{"/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/99/original.mp4", adminCookie},
	} {
		rec := doRequest(t, s, http.MethodGet, tc.path, nil, tc.cookie)
		if rec.Code != http.StatusNotFound {
			t.Errorf("authenticated GET %s status = %d, want 404", tc.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), unknownMarker) {
			t.Errorf("authenticated GET %s leaked unregistered bytes", tc.path)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	for _, tc := range []struct {
		path   string
		marker string
	}{
		{"/media/registered.mp4", mediaMarker},
		{uploadURL, uploadMarker},
	} {
		rec := doRequest(t, s, http.MethodGet, tc.path, nil)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s with closed DB status = %d, want 500", tc.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), tc.marker) {
			t.Errorf("GET %s with closed DB leaked marker", tc.path)
		}
	}

	rec := doRequest(t, s, http.MethodGet, "/static/img/security-probe.svg", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != staticMarker {
		t.Errorf("ordinary static asset with closed DB = %d/%q, want 200/%q",
			rec.Code, rec.Body.String(), staticMarker)
	}
}

func TestSecurityAvatars(t *testing.T) {
	s := newTestServerInTempDir(t)
	db := s.db
	ownerID := insertUser(t, db, "avatar-owner@security.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, db, "avatar-other@security.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "avatar-admin@security.test", "synthetic-password", "Sofia", true)
	insertChannel(t, db, "1", "Open Avatar Channel", "Sofia", ownerID, false)
	insertChannel(t, db, "2", "Locked Avatar Channel", "Sofia", ownerID, true)

	openAvatar := "/static/uploads/channels/1/avatar.jpg"
	lockedAvatar := "/static/uploads/channels/2/avatar.jpg"
	const openMarker = "SYNTHETIC-OPEN-AVATAR"
	const lockedMarker = "SYNTHETIC-LOCKED-AVATAR"
	writeTestFile(t, "static/uploads/channels/1/avatar.jpg", []byte(openMarker))
	writeTestFile(t, "static/uploads/channels/2/avatar.jpg", []byte(lockedMarker))
	if _, err := db.Exec(`update channels set avatar_url = ? where id = '1'`, openAvatar); err != nil {
		t.Fatalf("set open avatar: %v", err)
	}
	if _, err := db.Exec(`update channels set avatar_url = ? where id = '2'`, lockedAvatar); err != nil {
		t.Fatalf("set locked avatar: %v", err)
	}

	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)
	for _, tc := range []struct {
		name       string
		path       string
		cookie     *http.Cookie
		status     int
		bodyMarker string
	}{
		{"open anonymous", openAvatar, nil, http.StatusOK, openMarker},
		{"locked anonymous", lockedAvatar, nil, http.StatusForbidden, ""},
		{"locked other", lockedAvatar, otherCookie, http.StatusForbidden, ""},
		{"locked owner", lockedAvatar, ownerCookie, http.StatusOK, lockedMarker},
		{"locked admin", lockedAvatar, adminCookie, http.StatusOK, lockedMarker},
		{"unknown avatar", "/static/uploads/channels/99/avatar.jpg", nil, http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, tc.path, nil, cookies...)
			if rec.Code != tc.status {
				t.Fatalf("GET %s status = %d, want %d", tc.path, rec.Code, tc.status)
			}
			if tc.bodyMarker != "" && !strings.Contains(rec.Body.String(), tc.bodyMarker) {
				t.Errorf("body missing marker %q", tc.bodyMarker)
			}
			if tc.bodyMarker == "" && (strings.Contains(rec.Body.String(), openMarker) ||
				strings.Contains(rec.Body.String(), lockedMarker)) {
				t.Error("denied or unknown avatar response leaked content")
			}
		})
	}
}

func TestSecurityAvatarLookupFailure(t *testing.T) {
	s := newTestServerInTempDir(t)
	ownerID := insertUser(t, s.db, "avatar-db-error@security.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "1", "Avatar DB Error Channel", "Sofia", ownerID, false)
	const marker = "SYNTHETIC-AVATAR-DB-ERROR"
	avatarURL := "/static/uploads/channels/1/avatar.jpg"
	writeTestFile(t, "static/uploads/channels/1/avatar.jpg", []byte(marker))
	if _, err := s.db.Exec(`update channels set avatar_url = ? where id = '1'`, avatarURL); err != nil {
		t.Fatalf("set avatar URL: %v", err)
	}
	if _, err := s.db.Exec(`alter table channels drop column avatar_url`); err != nil {
		t.Fatalf("drop avatar_url to inject lookup failure: %v", err)
	}

	rec := doRequest(t, s, http.MethodGet, avatarURL, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("avatar lookup status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), marker) {
		t.Error("avatar DB lookup failure leaked file content")
	}
}

func TestSecurityPrivateComments(t *testing.T) {
	s := newTestServerInTempDir(t)
	db := s.db
	ownerID := insertUser(t, db, "comment-owner@security.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, db, "comment-other@security.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "comment-admin@security.test", "synthetic-password", "Sofia", true)
	insertChannel(t, db, "1", "Open Comment Channel", "Sofia", ownerID, false)
	insertChannel(t, db, "2", "Locked Comment Channel", "Sofia", ownerID, true)

	for _, video := range []testVideoRow{
		{
			id: 1, channelID: "1", title: "Public", description: "fixture",
			mediaURL: "/media/comment-public.mp4", thumbURL: "/media/comment-public.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
		{
			id: 2, channelID: "1", title: "Private", description: "fixture",
			mediaURL: "/media/comment-private.mp4", thumbURL: "/media/comment-private.jpg",
			location: "Sofia", userID: ownerID, isPublic: 0,
		},
		{
			id: 3, channelID: "1", title: "Video Locked", description: "fixture",
			mediaURL: "/media/comment-video-locked.mp4", thumbURL: "/media/comment-video-locked.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
		},
		{
			id: 4, channelID: "2", title: "Channel Locked", description: "fixture",
			mediaURL: "/media/comment-channel-locked.mp4", thumbURL: "/media/comment-channel-locked.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
		{
			id: 5, channelID: "1", title: "Comments Disabled", description: "fixture",
			mediaURL: "/media/comment-disabled.mp4", thumbURL: "/media/comment-disabled.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
	} {
		insertVideo(t, db, video)
	}
	if _, err := db.Exec(`update videos set allow_comments = 0 where id = 5`); err != nil {
		t.Fatalf("disable comments: %v", err)
	}

	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)
	for _, tc := range []struct {
		name     string
		videoID  string
		cookie   *http.Cookie
		status   int
		inserted bool
	}{
		{"public other", "1", otherCookie, http.StatusSeeOther, true},
		{"private other", "2", otherCookie, http.StatusNotFound, false},
		{"private owner", "2", ownerCookie, http.StatusSeeOther, true},
		{"private admin", "2", adminCookie, http.StatusSeeOther, true},
		{"video lock owner", "3", ownerCookie, http.StatusNotFound, false},
		{"video lock other", "3", otherCookie, http.StatusNotFound, false},
		{"video lock admin", "3", adminCookie, http.StatusSeeOther, true},
		{"channel lock owner", "4", ownerCookie, http.StatusNotFound, false},
		{"channel lock other", "4", otherCookie, http.StatusNotFound, false},
		{"channel lock admin", "4", adminCookie, http.StatusSeeOther, true},
		{"disabled ordinary user", "5", otherCookie, http.StatusForbidden, false},
		{"disabled admin", "5", adminCookie, http.StatusForbidden, false},
		{"missing video", "999", adminCookie, http.StatusNotFound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := countRows(t, db, "comments")
			form := url.Values{
				"video_id": {tc.videoID},
				"content":  {"synthetic review comment"},
			}
			rec := doRequest(t, s, http.MethodPost, "/add-comment", form, tc.cookie)
			if rec.Code != tc.status {
				t.Fatalf("POST /add-comment status = %d, want %d; body=%q",
					rec.Code, tc.status, rec.Body.String())
			}
			after := countRows(t, db, "comments")
			wantAfter := before
			if tc.inserted {
				wantAfter++
			}
			if after != wantAfter {
				t.Errorf("comments after request = %d, want %d", after, wantAfter)
			}
		})
	}
}

func TestSecurityCommentFailures(t *testing.T) {
	s := newTestServerWithTemplates(t, "auth.html")
	db := s.db
	ownerID := insertUser(t, db, "comment-failures@security.test", "synthetic-password", "Sofia", false)
	insertChannel(t, db, "1", "Comment Failure Channel", "Sofia", ownerID, false)
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Comment Failure Video", description: "fixture",
		mediaURL: "/media/comment-failure.mp4", thumbURL: "/media/comment-failure.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	cookie := newSessionCookie(t, s, ownerID)

	form := url.Values{
		"video_id": {"1"},
		"content":  {"synthetic anonymous comment"},
	}
	rec := doRequest(t, s, http.MethodPost, "/add-comment", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Login") {
		t.Errorf("anonymous comment status/body = %d/%q, want login form with status 200",
			rec.Code, rec.Body.String())
	}
	if n := countRows(t, db, "comments"); n != 0 {
		t.Errorf("comments after anonymous request = %d, want 0", n)
	}

	form.Set("video_id", "not-an-id")
	rec = doRequest(t, s, http.MethodPost, "/add-comment", form, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid video ID status = %d, want 400", rec.Code)
	}
	if n := countRows(t, db, "comments"); n != 0 {
		t.Errorf("comments after invalid ID = %d, want 0", n)
	}

	if _, err := db.Exec(`drop table videos`); err != nil {
		t.Fatalf("drop videos to inject query failure: %v", err)
	}
	form.Set("video_id", "1")
	form.Set("content", "synthetic database failure comment")
	rec = doRequest(t, s, http.MethodPost, "/add-comment", form, cookie)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("comment query failure status = %d, want 500", rec.Code)
	}
	if n := countRows(t, db, "comments"); n != 0 {
		t.Errorf("comments after query failure = %d, want 0", n)
	}
}

func TestSecurityMediaHead(t *testing.T) {
	s := newTestServerInTempDir(t)
	ownerID := insertUser(t, s.db, "media-head@security.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "1", "Media Head Channel", "Sofia", ownerID, false)
	const publicMarker = "SYNTHETIC-HEAD-PUBLIC"
	const privateMarker = "SYNTHETIC-HEAD-PRIVATE"
	writeTestFile(t, "Videos/head-public.mp4", []byte(publicMarker))
	writeTestFile(t, "Videos/head-private.mp4", []byte(privateMarker))
	insertVideo(t, s.db, testVideoRow{
		id: 1, channelID: "1", title: "Head Public", description: "fixture",
		mediaURL: "/media/head-public.mp4", thumbURL: "/media/head-public.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, s.db, testVideoRow{
		id: 2, channelID: "1", title: "Head Private", description: "fixture",
		mediaURL: "/media/head-private.mp4", thumbURL: "/media/head-private.jpg",
		location: "Sofia", userID: ownerID, isPublic: 0,
	})

	public := doRequest(t, s, http.MethodHead, "/media/head-public.mp4", nil)
	if public.Code != http.StatusOK {
		t.Errorf("public HEAD status = %d, want 200", public.Code)
	}
	if public.Body.Len() != 0 {
		t.Errorf("public HEAD body length = %d, want 0", public.Body.Len())
	}

	private := doRequest(t, s, http.MethodHead, "/media/head-private.mp4", nil)
	if private.Code != http.StatusForbidden {
		t.Errorf("private HEAD status = %d, want 403", private.Code)
	}
	if strings.Contains(private.Body.String(), privateMarker) {
		t.Error("private HEAD leaked media content")
	}
}

func TestSecurityDashboardUploadTemplate(t *testing.T) {
	s := newTestServerWithTemplates(t, "dashboard.html")
	ownerID := insertUser(t, s.db, "dashboard-template@security.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "1", "Dashboard Template Channel", "Sofia", ownerID, false)
	cookie := newSessionCookie(t, s, ownerID)

	rec := doRequest(t, s, http.MethodGet, "/dashboard", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="video"`,
		`name="thumbnail_base64"`,
		`name="is_public" value="on" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	for _, stale := range []string{`name="video_file"`, `name="thumbnail"`, `name="is_public" value="1"`} {
		if strings.Contains(body, stale) {
			t.Errorf("dashboard contains stale upload attribute %q", stale)
		}
	}
}
