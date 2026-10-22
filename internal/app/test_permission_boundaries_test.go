package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestChannelAdminLockPermissions(t *testing.T) {
	s := newTestServer(t)
	ownerID := insertUser(t, s.db, "lock-owner@permissions.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, s.db, "lock-other@permissions.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, s.db, "lock-admin@permissions.test", "synthetic-password", "Sofia", true)
	insertChannel(t, s.db, "101", "Original Permission Channel", "Sofia", ownerID, false)

	cookies := map[string]*http.Cookie{
		"owner": newSessionCookie(t, s, ownerID),
		"other": newSessionCookie(t, s, otherID),
		"admin": newSessionCookie(t, s, adminID),
	}
	for _, tc := range []struct {
		name           string
		role           string
		initialLock    int
		submittedLock  string
		wantLock       int
		wantStatus     int
		wantNameUpdate bool
	}{
		{"owner cannot set lock", "owner", 0, "1", 0, http.StatusSeeOther, true},
		{"owner cannot clear lock", "owner", 1, "0", 1, http.StatusSeeOther, true},
		{"other cannot set lock", "other", 0, "1", 0, http.StatusNotFound, false},
		{"other cannot clear lock", "other", 1, "0", 1, http.StatusNotFound, false},
		{"admin can set lock", "admin", 0, "1", 1, http.StatusSeeOther, false},
		{"admin can clear lock", "admin", 1, "0", 0, http.StatusSeeOther, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.db.Exec(
				`update channels set name = 'Original Permission Channel', is_admin_locked = ? where id = '101'`,
				tc.initialLock,
			); err != nil {
				t.Fatalf("reset channel fixture: %v", err)
			}
			rec := doRequest(t, s, http.MethodPost, "/edit-channel/101", url.Values{
				"name":            {"Permission Channel"},
				"location":        {"Sofia"},
				"description":     {"fixture"},
				"is_admin_locked": {tc.submittedLock},
			}, cookies[tc.role])
			if rec.Code != tc.wantStatus {
				t.Fatalf("edit status = %d, want %d", rec.Code, tc.wantStatus)
			}

			var gotLock int
			var gotName string
			if err := s.db.QueryRow(
				`select name, is_admin_locked from channels where id = '101'`,
			).Scan(&gotName, &gotLock); err != nil {
				t.Fatalf("read channel state: %v", err)
			}
			if gotLock != tc.wantLock {
				t.Errorf("is_admin_locked = %d, want %d", gotLock, tc.wantLock)
			}
			wantName := "Original Permission Channel"
			if tc.wantNameUpdate {
				wantName = "Permission Channel"
			}
			if gotName != wantName {
				t.Errorf("channel name = %q, want %q", gotName, wantName)
			}
		})
	}
}

func TestChannelPageVideoPermissions(t *testing.T) {
	s := newTestServerWithTemplates(t, "channel.html")
	ownerID := insertUser(t, s.db, "channel-video-owner@permissions.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, s.db, "channel-video-other@permissions.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, s.db, "channel-video-admin@permissions.test", "synthetic-password", "Sofia", true)
	insertChannel(t, s.db, "101", "Synthetic Permission Channel", "Sofia", ownerID, false)
	for _, video := range []testVideoRow{
		{
			id: 1, channelID: "101", title: "SYNTHETIC-CHANNEL-PUBLIC",
			mediaURL: "/media/permission-public.mp4", thumbURL: "/media/permission-public.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
		{
			id: 2, channelID: "101", title: "SYNTHETIC-CHANNEL-PRIVATE",
			mediaURL: "/media/permission-private.mp4", thumbURL: "/media/permission-private.jpg",
			location: "Sofia", userID: ownerID, isPublic: 0,
		},
		{
			id: 3, channelID: "101", title: "SYNTHETIC-CHANNEL-LOCKED",
			mediaURL: "/media/permission-locked.mp4", thumbURL: "/media/permission-locked.jpg",
			location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
		},
	} {
		insertVideo(t, s.db, video)
	}

	for _, tc := range []struct {
		name        string
		cookie      *http.Cookie
		wantPrivate bool
		wantLocked  bool
	}{
		{"anonymous", nil, false, false},
		{"owner", newSessionCookie(t, s, ownerID), false, false},
		{"unrelated user", newSessionCookie(t, s, otherID), false, false},
		{"admin", newSessionCookie(t, s, adminID), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, "/channel/101", nil, cookies...)
			if rec.Code != http.StatusOK {
				t.Fatalf("channel page status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "SYNTHETIC-CHANNEL-PUBLIC") {
				t.Error("channel page did not include the public video")
			}
			if got := strings.Contains(body, "SYNTHETIC-CHANNEL-PRIVATE"); got != tc.wantPrivate {
				t.Errorf("private video visible = %v, want %v", got, tc.wantPrivate)
			}
			if got := strings.Contains(body, "SYNTHETIC-CHANNEL-LOCKED"); got != tc.wantLocked {
				t.Errorf("locked video visible = %v, want %v", got, tc.wantLocked)
			}
		})
	}
}

func TestDashboardOwnershipIsolation(t *testing.T) {
	s := newTestServerWithTemplates(t, "dashboard.html")
	userAID := insertUser(t, s.db, "dashboard-a@permissions.test", "synthetic-password", "Sofia", false)
	userBID := insertUser(t, s.db, "dashboard-b@permissions.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, s.db, "dashboard-admin@permissions.test", "synthetic-password", "Sofia", true)
	emptyID := insertUser(t, s.db, "dashboard-empty@permissions.test", "synthetic-password", "Sofia", false)

	fixtures := []struct {
		channelID   string
		videoID     int64
		channel     string
		video       string
		ownerUserID int64
	}{
		{"101", 1, "SYNTHETIC-DASH-A-CHANNEL", "SYNTHETIC-DASH-A-VIDEO", userAID},
		{"202", 2, "SYNTHETIC-DASH-B-CHANNEL", "SYNTHETIC-DASH-B-VIDEO", userBID},
		{"303", 3, "SYNTHETIC-DASH-ADMIN-CHANNEL", "SYNTHETIC-DASH-ADMIN-VIDEO", adminID},
	}
	for _, fixture := range fixtures {
		insertChannel(t, s.db, fixture.channelID, fixture.channel, "Sofia", fixture.ownerUserID, false)
		insertVideo(t, s.db, testVideoRow{
			id: fixture.videoID, channelID: fixture.channelID, title: fixture.video,
			mediaURL: fmt.Sprintf("/media/dashboard-%s.mp4", fixture.channelID),
			thumbURL: fmt.Sprintf("/media/dashboard-%s.jpg", fixture.channelID),
			location: "Sofia", userID: fixture.ownerUserID, isPublic: 0,
		})
	}

	for _, tc := range []struct {
		name          string
		userID        int64
		wantChannelID string
		wantVideoID   int64
	}{
		{"user A", userAID, "101", 1},
		{"user B", userBID, "202", 2},
		{"admin remains owner-scoped", adminID, "303", 3},
		{"empty user", emptyID, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &User{ID: tc.userID, IsAdmin: tc.userID == adminID}
			videos, err := s.loadUserVideos(user)
			if err != nil {
				t.Fatalf("load user videos: %v", err)
			}
			channels, err := s.loadManageableChannels(user)
			if err != nil {
				t.Fatalf("load manageable channels: %v", err)
			}
			wantCount := 0
			if tc.wantVideoID != 0 {
				wantCount = 1
			}
			if len(videos) != wantCount || len(channels) != wantCount {
				t.Fatalf("loader counts = videos %d, channels %d; want %d each",
					len(videos), len(channels), wantCount)
			}
			if wantCount == 1 {
				if videos[0].ID != tc.wantVideoID {
					t.Errorf("video ID = %d, want %d", videos[0].ID, tc.wantVideoID)
				}
				if channels[0].ID != tc.wantChannelID {
					t.Errorf("channel ID = %q, want %q", channels[0].ID, tc.wantChannelID)
				}
			}

			cookie := newSessionCookie(t, s, tc.userID)
			rec := doRequest(t, s, http.MethodGet, "/dashboard", nil, cookie)
			if rec.Code != http.StatusOK {
				t.Fatalf("dashboard status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			for _, fixture := range fixtures {
				wantVisible := fixture.ownerUserID == tc.userID
				if got := strings.Contains(body, fixture.video); got != wantVisible {
					t.Errorf("video %q visible = %v, want %v", fixture.video, got, wantVisible)
				}
				channelOption := fmt.Sprintf(`value="%s">%s`, fixture.channelID, fixture.channel)
				if got := strings.Contains(body, channelOption); got != wantVisible {
					t.Errorf("channel option %q visible = %v, want %v", channelOption, got, wantVisible)
				}
			}
		})
	}

	anonymous := doRequest(t, s, http.MethodGet, "/dashboard", nil)
	if anonymous.Code != http.StatusSeeOther || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous dashboard response = %d location %q, want login redirect",
			anonymous.Code, anonymous.Header().Get("Location"))
	}
	for _, fixture := range fixtures {
		if strings.Contains(anonymous.Body.String(), fixture.channel) ||
			strings.Contains(anonymous.Body.String(), fixture.video) {
			t.Errorf("anonymous dashboard response exposed %q or %q", fixture.channel, fixture.video)
		}
	}
}
