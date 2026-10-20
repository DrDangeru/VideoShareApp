package app

import (
	"reflect"
	"testing"
)

func feedVideoIDs(feed []VideoView) []int64 {
	ids := make([]int64, len(feed))
	for i, v := range feed {
		ids[i] = v.ID
	}
	return ids
}

func feedVideoByID(feed []VideoView, id int64) *VideoView {
	for i := range feed {
		if feed[i].ID == id {
			return &feed[i]
		}
	}
	return nil
}

func TestLoadFeed(t *testing.T) {
	s := newTestServer(t)
	db := s.db

	ownerID := insertUser(t, db, "owner@feed.test", "pw", "Sofia", false)
	viewerID := insertUser(t, db, "viewer@feed.test", "pw", "Sofia", false)
	adminID := insertUser(t, db, "admin@feed.test", "pw", "Sofia", true)

	insertChannel(t, db, "1", "Open Channel", "Sofia", ownerID, false)
	insertChannel(t, db, "2", "Locked Channel", "Sofia", ownerID, true)

	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Public Local", description: "d",
		mediaURL: "/media/v1.mp4", thumbURL: "/media/v1.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 2, channelID: "1", title: "Public Remote", description: "d",
		mediaURL: "/media/v2.mp4", thumbURL: "/media/v2.jpg",
		location: "Berlin", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 3, channelID: "1", title: "Private Local", description: "d",
		mediaURL: "/media/v3.mp4", thumbURL: "/media/v3.jpg",
		location: "Sofia", userID: ownerID, isPublic: 0,
	})
	insertVideo(t, db, testVideoRow{
		id: 4, channelID: "1", title: "Video Locked", description: "d",
		mediaURL: "/media/v4.mp4", thumbURL: "/media/v4.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 5, channelID: "2", title: "Channel Locked", description: "d",
		mediaURL: "/media/v5.mp4", thumbURL: "/media/v5.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})

	if _, err := db.Exec(
		`insert into favorites(user_id, video_id) values(?, 1)`, viewerID,
	); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}
	if _, err := db.Exec(
		`insert into subscriptions(user_id, channel_id) values(?, '1')`, viewerID,
	); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	t.Run("anonymous", func(t *testing.T) {
		feed, err := s.loadFeed(nil)
		if err != nil {
			t.Fatalf("loadFeed: %v", err)
		}
		if ids := feedVideoIDs(feed); !reflect.DeepEqual(ids, []int64{2, 1}) {
			t.Fatalf("ids = %v, want [2 1]", ids)
		}
		for _, v := range feed {
			if v.IsFavorite || v.IsSubscribed {
				t.Errorf("video %d should have no viewer flags", v.ID)
			}
		}
	})

	t.Run("authenticated viewer", func(t *testing.T) {
		viewer := &User{ID: viewerID, Email: "viewer@feed.test", Location: "Sofia"}
		feed, err := s.loadFeed(viewer)
		if err != nil {
			t.Fatalf("loadFeed: %v", err)
		}
		if ids := feedVideoIDs(feed); !reflect.DeepEqual(ids, []int64{1, 2}) {
			t.Fatalf("ids = %v, want [1 2]", ids)
		}
		v1 := feedVideoByID(feed, 1)
		if v1 == nil || !v1.IsFavorite || !v1.IsSubscribed {
			t.Errorf("video 1 flags = %+v, want favorite+subscribed", v1)
		}
		v2 := feedVideoByID(feed, 2)
		if v2 == nil || v2.IsFavorite || !v2.IsSubscribed {
			t.Errorf("video 2 flags = %+v, want subscribed only", v2)
		}
	})

	t.Run("admin", func(t *testing.T) {
		admin := &User{ID: adminID, Email: "admin@feed.test", Location: "Sofia", IsAdmin: true}
		feed, err := s.loadFeed(admin)
		if err != nil {
			t.Fatalf("loadFeed: %v", err)
		}
		if ids := feedVideoIDs(feed); !reflect.DeepEqual(ids, []int64{5, 4, 3, 1, 2}) {
			t.Fatalf("ids = %v, want [5 4 3 1 2]", ids)
		}
		for _, v := range feed {
			if v.IsFavorite || v.IsSubscribed {
				t.Errorf("video %d should have no flags for admin", v.ID)
			}
		}
		if v := feedVideoByID(feed, 3); v == nil || v.IsPublic {
			t.Error("video 3 should be visible to admin as private")
		}
		if v := feedVideoByID(feed, 4); v == nil || !v.IsAdminLocked {
			t.Error("video 4 should be visible to admin as locked")
		}
	})
}

func TestLoadFeedEmpty(t *testing.T) {
	s := newTestServer(t)

	feed, err := s.loadFeed(nil)
	if err != nil {
		t.Fatalf("loadFeed: %v", err)
	}
	if len(feed) != 0 {
		t.Errorf("feed length = %d, want 0", len(feed))
	}
}
