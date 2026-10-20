package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestDiscoveryChannelLocks(t *testing.T) {
	s := newTestServerWithTemplates(t, "home.html", "watch.html")
	db := s.db
	openOwnerID := insertUser(t, db, "open-owner@discovery.test", "synthetic-password", "Sofia", false)
	lockedOwnerID := insertUser(t, db, "locked-owner@discovery.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, db, "other@discovery.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "admin@discovery.test", "synthetic-password", "Sofia", true)

	insertChannel(t, db, "101", "Open Source Channel", "Sofia", openOwnerID, false)
	insertChannel(t, db, "202", "Locked Discovery Channel", "Sofia", lockedOwnerID, true)
	insertChannel(t, db, "303", "Nearby Open Channel", "Sofia", openOwnerID, false)

	for _, video := range []testVideoRow{
		{
			id: 1, channelID: "101", title: "Current Open Video", description: "fixture",
			mediaURL: "/media/discovery-current.mp4", thumbURL: "/media/discovery-current.jpg",
			location: "Sofia", userID: openOwnerID, isPublic: 1,
		},
		{
			id: 2, channelID: "101", title: "Open Similar Video", description: "fixture",
			mediaURL: "/media/discovery-open.mp4", thumbURL: "/media/discovery-open.jpg",
			location: "Sofia", userID: openOwnerID, isPublic: 1,
		},
		{
			id: 3, channelID: "202", title: "Locked Discovery Video", description: "fixture",
			mediaURL: "/media/discovery-locked.mp4", thumbURL: "/media/discovery-locked.jpg",
			location: "Sofia", userID: lockedOwnerID, isPublic: 1,
		},
		{
			id: 4, channelID: "303", title: "Nearby Public Video", description: "fixture",
			mediaURL: "/media/discovery-nearby.mp4", thumbURL: "/media/discovery-nearby.jpg",
			location: "Sofia", userID: openOwnerID, isPublic: 1,
		},
	} {
		insertVideo(t, db, video)
	}

	for _, channelID := range []string{"101", "202"} {
		if _, err := db.Exec(
			`insert into subscriptions(user_id, channel_id) values(?, ?)`,
			lockedOwnerID,
			channelID,
		); err != nil {
			t.Fatalf("subscribe locked owner to %s: %v", channelID, err)
		}
		if _, err := db.Exec(
			`insert into subscriptions(user_id, channel_id) values(?, ?)`,
			adminID,
			channelID,
		); err != nil {
			t.Fatalf("subscribe admin to %s: %v", channelID, err)
		}
	}

	roles := []struct {
		name       string
		user       *User
		seesLocked bool
	}{
		{"anonymous", nil, false},
		{"locked channel owner", &User{ID: lockedOwnerID}, false},
		{"other user", &User{ID: otherID}, false},
		{"admin", &User{ID: adminID, IsAdmin: true}, true},
	}
	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			videos, err := s.loadSimilarVideos(1, 101, "Sofia", role.user)
			if err != nil {
				t.Fatalf("loadSimilarVideos: %v", err)
			}
			videoTitles := make([]string, 0, len(videos))
			for _, video := range videos {
				videoTitles = append(videoTitles, video.Title)
			}
			assertDiscoveryItem(t, videoTitles, "Open Similar Video", true)
			assertDiscoveryItem(t, videoTitles, "Nearby Public Video", true)
			assertDiscoveryItem(t, videoTitles, "Locked Discovery Video", role.seesLocked)

			channels, err := s.loadSimilarChannels(101, "Sofia", role.user)
			if err != nil {
				t.Fatalf("loadSimilarChannels: %v", err)
			}
			channelNames := make([]string, 0, len(channels))
			for _, channel := range channels {
				channelNames = append(channelNames, channel.ChannelName)
			}
			assertDiscoveryItem(t, channelNames, "Nearby Open Channel", true)
			assertDiscoveryItem(t, channelNames, "Locked Discovery Channel", role.seesLocked)

			sidebar, err := s.loadChannelSidebar(role.user, false)
			if err != nil {
				t.Fatalf("loadChannelSidebar: %v", err)
			}
			sidebarNames := make([]string, 0, len(sidebar))
			for _, channel := range sidebar {
				sidebarNames = append(sidebarNames, channel.ChannelName)
			}
			assertDiscoveryItem(t, sidebarNames, "Open Source Channel", true)
			assertDiscoveryItem(t, sidebarNames, "Nearby Open Channel", true)
			assertDiscoveryItem(t, sidebarNames, "Locked Discovery Channel", role.seesLocked)

			if role.name == "locked channel owner" {
				subscribed, err := s.loadChannelSidebar(role.user, true)
				if err != nil {
					t.Fatalf("load subscribed sidebar: %v", err)
				}
				subscribedNames := make([]string, 0, len(subscribed))
				for _, channel := range subscribed {
					subscribedNames = append(subscribedNames, channel.ChannelName)
				}
				assertDiscoveryItem(t, subscribedNames, "Open Source Channel", true)
				assertDiscoveryItem(t, subscribedNames, "Locked Discovery Channel", false)
			}
			if role.name == "admin" {
				subscribed, err := s.loadChannelSidebar(role.user, true)
				if err != nil {
					t.Fatalf("load admin subscribed sidebar: %v", err)
				}
				subscribedNames := make([]string, 0, len(subscribed))
				for _, channel := range subscribed {
					subscribedNames = append(subscribedNames, channel.ChannelName)
				}
				assertDiscoveryItem(t, subscribedNames, "Locked Discovery Channel", true)
			}
		})
	}

	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)
	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
		admin   bool
	}{
		{name: "anonymous"},
		{name: "other", cookies: []*http.Cookie{otherCookie}},
		{name: "admin", cookies: []*http.Cookie{adminCookie}, admin: true},
	} {
		t.Run(tc.name+" HTTP discovery", func(t *testing.T) {
			home := doRequest(t, s, http.MethodGet, "/", nil, tc.cookies...)
			watch := doRequest(t, s, http.MethodGet, "/watch/1", nil, tc.cookies...)
			for name, body := range map[string]string{
				"home":  home.Body.String(),
				"watch": watch.Body.String(),
			} {
				if home.Code != http.StatusOK || watch.Code != http.StatusOK {
					t.Fatalf("%s/watch status = %d/%d, want 200", name, home.Code, watch.Code)
				}
				lockedPresent := strings.Contains(body, "Locked Discovery Channel") ||
					strings.Contains(body, "Locked Discovery Video")
				if lockedPresent != tc.admin {
					t.Errorf("%s exposed locked discovery metadata = %v, admin = %v",
						name, lockedPresent, tc.admin)
				}
			}
		})
	}
}

func assertDiscoveryItem(t *testing.T, items []string, target string, want bool) {
	t.Helper()
	found := false
	for _, item := range items {
		if item == target {
			found = true
			break
		}
	}
	if found != want {
		t.Errorf("item %q present = %v, want %v; items=%v", target, found, want, items)
	}
}
