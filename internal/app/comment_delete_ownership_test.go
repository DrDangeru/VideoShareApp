package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCommentDeleteOwnership(t *testing.T) {
	s := newTestServerWithTemplates(t, "watch.html")
	db := s.db
	ownerID := insertUser(t, db, "channel-owner@delete.test", "synthetic-password", "Sofia", false)
	authorID := insertUser(t, db, "comment-author@delete.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, db, "unrelated@delete.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "admin@delete.test", "synthetic-password", "Sofia", true)
	const channelID = "101"
	if ownerID == 101 {
		t.Fatal("test requires channel ID to differ from its owner user ID")
	}
	insertChannel(t, db, channelID, "Delete Test Channel", "Sofia", ownerID, false)
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: channelID, title: "Delete Test Video", description: "fixture",
		mediaURL: "/media/delete-test.mp4", thumbURL: "/media/delete-test.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	if _, err := db.Exec(`
		insert into users(id, email, password_hash, location, is_admin)
		values(101, 'channel-id-collision@delete.test', 'synthetic-hash', 'Sofia', 0)
	`); err != nil {
		t.Fatalf("insert collision user: %v", err)
	}

	authorCookie := newSessionCookie(t, s, authorID)
	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)
	collisionCookie := newSessionCookie(t, s, 101)

	for _, tc := range []struct {
		name    string
		cookie  *http.Cookie
		status  int
		deleted bool
	}{
		{"author", authorCookie, http.StatusSeeOther, true},
		{"channel owner", ownerCookie, http.StatusSeeOther, true},
		{"admin", adminCookie, http.StatusSeeOther, true},
		{"unrelated user", otherCookie, http.StatusForbidden, false},
		{"user ID equals channel ID", collisionCookie, http.StatusForbidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := db.Exec(
				`insert into comments(video_id, user_id, content) values(1, ?, ?)`,
				authorID,
				"synthetic delete authorization comment",
			)
			if err != nil {
				t.Fatalf("insert comment: %v", err)
			}
			commentID, err := res.LastInsertId()
			if err != nil {
				t.Fatalf("comment ID: %v", err)
			}
			before := countRows(t, db, "comments")
			rec := doRequest(
				t,
				s,
				http.MethodPost,
				fmt.Sprintf("/delete-comment/%d", commentID),
				nil,
				tc.cookie,
			)
			if rec.Code != tc.status {
				t.Fatalf("delete status = %d, want %d; body=%q", rec.Code, tc.status, rec.Body.String())
			}
			after := countRows(t, db, "comments")
			wantAfter := before
			if tc.deleted {
				wantAfter--
			}
			if after != wantAfter {
				t.Errorf("comments after delete = %d, want %d", after, wantAfter)
			}
		})
	}

	res, err := db.Exec(
		`insert into comments(video_id, user_id, content) values(1, ?, 'synthetic collision visibility comment')`,
		authorID,
	)
	if err != nil {
		t.Fatalf("insert collision visibility comment: %v", err)
	}
	commentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("collision comment ID: %v", err)
	}
	ownerPage := doRequest(t, s, http.MethodGet, "/watch/1", nil, ownerCookie)
	if ownerPage.Code != http.StatusOK {
		t.Fatalf("owner watch status = %d, want 200", ownerPage.Code)
	}
	if !strings.Contains(ownerPage.Body.String(), fmt.Sprintf("/delete-comment/%d", commentID)) {
		t.Error("channel owner did not receive a delete-comment form")
	}
	page := doRequest(t, s, http.MethodGet, "/watch/1", nil, collisionCookie)
	if page.Code != http.StatusOK {
		t.Fatalf("collision-user watch status = %d, want 200", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "synthetic collision visibility comment") {
		t.Fatal("watch page omitted fixture comment")
	}
	if strings.Contains(body, fmt.Sprintf("/delete-comment/%d", commentID)) {
		t.Error("channel-ID collision exposed a delete-comment form")
	}
}
