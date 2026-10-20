package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"image"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUploadThumbnailValidationAndStorage(t *testing.T) {
	t.Run("valid PNG normalized to served JPEG", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		ownerID := insertUser(t, s.db, "upload-thumbnail@thumbnail.test", "synthetic-password", "Sofia", false)
		cookie := newSessionCookie(t, s, ownerID)
		rec := postThumbnailUpload(t, s, cookie, testThumbnailDataURL(t, "png", 3, 2))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("upload status = %d, want 303; body=%q", rec.Code, rec.Body.String())
		}

		var mediaURL, thumbnailURL string
		if err := s.db.QueryRow(
			`select media_url, thumbnail_url from videos where title = 'Thumbnail Upload'`,
		).Scan(&mediaURL, &thumbnailURL); err != nil {
			t.Fatalf("read uploaded video: %v", err)
		}
		wantURL := "/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/original.jpg"
		if thumbnailURL != wantURL {
			t.Fatalf("thumbnail_url = %q, want %q", thumbnailURL, wantURL)
		}
		if !strings.HasPrefix(mediaURL, "/static/uploads/users/") {
			t.Fatalf("media_url = %q", mediaURL)
		}
		rec = doRequest(t, s, http.MethodGet, thumbnailURL, nil, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET uploaded thumbnail status = %d, want 200", rec.Code)
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("decode served thumbnail: %v", err)
		}
		if format != "jpeg" || config.Width != 3 || config.Height != 2 {
			t.Errorf("served thumbnail = %s %dx%d, want JPEG 3x2", format, config.Width, config.Height)
		}
	})

	t.Run("thumbnail write failure returns 500", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		ownerID := insertUser(t, s.db, "upload-thumb-write@thumbnail.test", "synthetic-password", "Sofia", false)
		cookie := newSessionCookie(t, s, ownerID)
		thumbPath := filepath.Join("static", "uploads", "users", strconv.FormatInt(ownerID, 10), "1", "original.jpg")
		if err := os.MkdirAll(thumbPath, 0o755); err != nil {
			t.Fatalf("create directory at thumbnail path: %v", err)
		}

		rec := postThumbnailUpload(t, s, cookie, testThumbnailDataURL(t, "jpeg", 1, 1))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("upload status = %d, want 500", rec.Code)
		}
	})
}

func TestUploadInvalidThumbnailHasNoSideEffects(t *testing.T) {
	validPNG := testThumbnailDataURL(t, "png", 1, 1)
	invalid := []struct {
		name string
		data string
	}{
		{"bad base64", "data:image/jpeg;base64,%%%"},
		{"fake HTML", "data:image/jpeg;base64," + base64Text([]byte("<html>not an image</html>"))},
		{"declared type mismatch", "data:image/jpeg;base64," + strings.TrimPrefix(validPNG, "data:image/png;base64,")},
		{"truncated JPEG", "data:image/jpeg;base64," + base64Text([]byte{0xff, 0xd8, 0xff})},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServerInTempDir(t)
			ownerID := insertUser(t, s.db, "invalid-upload@thumbnail.test", "synthetic-password", "Sofia", false)
			cookie := newSessionCookie(t, s, ownerID)
			rec := postThumbnailUpload(t, s, cookie, tc.data)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("upload status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			if n := countRows(t, s.db, "channels"); n != 0 {
				t.Errorf("channels = %d, want 0", n)
			}
			if n := countRows(t, s.db, "videos"); n != 0 {
				t.Errorf("videos = %d, want 0", n)
			}
			if _, err := os.Stat(filepath.Join("static", "uploads")); !os.IsNotExist(err) {
				t.Errorf("uploads path stat error = %v, want not-exist", err)
			}
		})
	}
}

func TestEditThumbnailValidationAndStorage(t *testing.T) {
	t.Run("invalid thumbnail leaves metadata and files unchanged", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		ownerID := insertUser(t, s.db, "invalid-edit@thumbnail.test", "synthetic-password", "Sofia", false)
		insertChannel(t, s.db, "101", "Edit Thumbnail Channel", "Sofia", ownerID, false)
		insertVideo(t, s.db, testVideoRow{
			id: 1, channelID: "101", title: "Original Title", description: "Original Description",
			mediaURL: "/media/original.mp4", thumbURL: "/static/img/placeholder.svg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		})
		cookie := newSessionCookie(t, s, ownerID)
		rec := postEditThumbnail(t, s, cookie, "data:image/jpeg;base64,%%%")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("edit status = %d, want 400", rec.Code)
		}
		assertVideoThumbnailState(t, s.db, "Original Title", "/static/img/placeholder.svg")
		if _, err := os.Stat(filepath.Join("static", "uploads")); !os.IsNotExist(err) {
			t.Errorf("uploads path stat error = %v, want not-exist", err)
		}
	})

	t.Run("valid thumbnail uses owner/video-specific path", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		ownerID := insertUser(t, s.db, "valid-edit@thumbnail.test", "synthetic-password", "Sofia", false)
		insertChannel(t, s.db, "101", "Edit Thumbnail Channel", "Sofia", ownerID, false)
		insertVideo(t, s.db, testVideoRow{
			id: 1, channelID: "101", title: "Original Title", description: "Original Description",
			mediaURL: "/media/original.mp4", thumbURL: "/static/img/placeholder.svg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		})
		cookie := newSessionCookie(t, s, ownerID)
		rec := postEditThumbnail(t, s, cookie, testThumbnailDataURL(t, "png", 2, 2))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("edit status = %d, want 303; body=%q", rec.Code, rec.Body.String())
		}
		wantURL := "/static/uploads/users/" + strconv.FormatInt(ownerID, 10) + "/1/original.jpg"
		assertVideoThumbnailState(t, s.db, "Edited Title", wantURL)
		thumb := doRequest(t, s, http.MethodGet, wantURL, nil, cookie)
		if thumb.Code != http.StatusOK {
			t.Fatalf("GET edited thumbnail status = %d, want 200", thumb.Code)
		}
		if _, format, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes())); err != nil || format != "jpeg" {
			t.Fatalf("edited thumbnail decode = %s, %v", format, err)
		}
	})

	t.Run("thumbnail write failure returns 500 before metadata update", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		ownerID := insertUser(t, s.db, "edit-thumb-write@thumbnail.test", "synthetic-password", "Sofia", false)
		insertChannel(t, s.db, "101", "Edit Thumbnail Channel", "Sofia", ownerID, false)
		insertVideo(t, s.db, testVideoRow{
			id: 1, channelID: "101", title: "Original Title", description: "Original Description",
			mediaURL: "/media/original.mp4", thumbURL: "/static/img/placeholder.svg",
			location: "Sofia", userID: ownerID, isPublic: 1,
		})
		cookie := newSessionCookie(t, s, ownerID)
		thumbPath := filepath.Join("static", "uploads", "users", strconv.FormatInt(ownerID, 10), "1", "original.jpg")
		if err := os.MkdirAll(thumbPath, 0o755); err != nil {
			t.Fatalf("create directory at thumbnail path: %v", err)
		}

		rec := postEditThumbnail(t, s, cookie, testThumbnailDataURL(t, "jpeg", 1, 1))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("edit status = %d, want 500", rec.Code)
		}
		assertVideoThumbnailState(t, s.db, "Original Title", "/static/img/placeholder.svg")
	})
}

func postThumbnailUpload(
	t *testing.T,
	s *Server,
	cookie *http.Cookie,
	thumbnail string,
) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"new_channel":      "Thumbnail Upload Channel",
		"title":            "Thumbnail Upload",
		"description":      "synthetic upload fixture",
		"is_public":        "on",
		"allow_comments":   "on",
		"thumbnail_base64": thumbnail,
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}
	video, err := writer.CreateFormFile("video", "synthetic.mp4")
	if err != nil {
		t.Fatalf("create synthetic video: %v", err)
	}
	if _, err := video.Write([]byte("SYNTHETIC-THUMBNAIL-VIDEO")); err != nil {
		t.Fatalf("write synthetic video: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart form: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func postEditThumbnail(
	t *testing.T,
	s *Server,
	cookie *http.Cookie,
	thumbnail string,
) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"title":          {"Edited Title"},
		"description":    {"Edited Description"},
		"location":       {"Sofia"},
		"is_public":      {"1"},
		"allow_comments": {"1"},
		"made_for_kids":  {"0"},
		"thumbnail":      {thumbnail},
	}
	return doRequest(t, s, http.MethodPost, "/edit-video/1", form, cookie)
}

func assertVideoThumbnailState(t *testing.T, db *sql.DB, wantTitle, wantURL string) {
	t.Helper()
	var title, thumbnailURL string
	if err := db.QueryRow(
		`select title, thumbnail_url from videos where id = 1`,
	).Scan(&title, &thumbnailURL); err != nil {
		t.Fatalf("read video metadata: %v", err)
	}
	if title != wantTitle || thumbnailURL != wantURL {
		t.Errorf("video metadata title/thumbnail = %q/%q, want %q/%q",
			title, thumbnailURL, wantTitle, wantURL)
	}
}

func base64Text(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
