package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCommentAuthorPrivacy(t *testing.T) {
	s := newTestServerWithTemplates(t, "watch.html")
	db := s.db
	ownerID := insertUser(t, db, "video-owner@comments.test", "synthetic-password", "Sofia", false)
	authorID := insertUser(t, db, "distinct-comment-author@comment-author.test", "synthetic-password", "Sofia", false)
	viewerID := insertUser(t, db, "current-viewer@viewer.test", "synthetic-password", "Sofia", false)
	insertChannel(t, db, "101", "Public Comment Privacy Channel", "Sofia", ownerID, false)
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "101", title: "Public Comment Privacy Video", description: "fixture",
		mediaURL: "/media/comment-privacy.mp4", thumbURL: "/media/comment-privacy.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	if _, err := db.Exec(
		`insert into comments(video_id, user_id, content) values(1, ?, 'synthetic privacy comment')`,
		authorID,
	); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	comments, err := s.loadComments(1)
	if err != nil {
		t.Fatalf("loadComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments length = %d, want 1", len(comments))
	}
	wantAuthor := fmt.Sprintf("Member %d", authorID)
	if comments[0].AuthorName != wantAuthor {
		t.Errorf("author name = %q, want %q", comments[0].AuthorName, wantAuthor)
	}

	viewerCookie := newSessionCookie(t, s, viewerID)
	rec := doRequest(t, s, http.MethodGet, "/watch/1", nil, viewerCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("watch status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{wantAuthor, "synthetic privacy comment", "current-viewer@viewer.test"} {
		if !strings.Contains(body, want) {
			t.Errorf("watch page missing %q", want)
		}
	}
	for _, privatePart := range []string{
		"distinct-comment-author@comment-author.test",
		"distinct-comment-author",
		"comment-author.test",
	} {
		if strings.Contains(body, privatePart) {
			t.Errorf("watch page exposed comment author data %q", privatePart)
		}
	}
}

func TestCommentLengthIsLimitedByUnicodeCharacterCount(t *testing.T) {
	s := newTestServer(t)
	ownerID := insertUser(t, s.db, "comment-length@comments.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "101", "Comment Length Channel", "Sofia", ownerID, false)
	insertVideo(t, s.db, testVideoRow{
		id: 1, channelID: "101", title: "Comment Length Video", description: "fixture",
		mediaURL: "/media/comment-length.mp4", thumbURL: "/media/comment-length.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	cookie := newSessionCookie(t, s, ownerID)

	valid := strings.Repeat("é", 4000)
	rec := doRequest(t, s, http.MethodPost, "/add-comment", url.Values{
		"video_id": {"1"},
		"content":  {valid},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("4000-character comment status = %d, want 303; body=%q", rec.Code, rec.Body.String())
	}
	if countRows(t, s.db, "comments") != 1 {
		t.Fatal("valid 4000-character comment was not inserted")
	}

	for _, content := range []string{
		strings.Repeat("é", 4001),
		string([]byte{0xff}),
	} {
		rec = doRequest(t, s, http.MethodPost, "/add-comment", url.Values{
			"video_id": {"1"},
			"content":  {content},
		}, cookie)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("invalid comment status = %d, want 400", rec.Code)
		}
	}
	if countRows(t, s.db, "comments") != 1 {
		t.Error("invalid comments were inserted")
	}
}
