package app

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadRejectsForeignChannel(t *testing.T) {
	s := newTestServerInTempDir(t)
	ownerID := insertUser(t, s.db, "channel-owner@upload.test", "synthetic-password", "Sofia", false)
	attackerID := insertUser(t, s.db, "foreign-uploader@upload.test", "synthetic-password", "Sofia", false)
	insertChannel(t, s.db, "101", "Foreign Upload Channel", "Sofia", ownerID, false)
	cookie := newSessionCookie(t, s, attackerID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"channel_id":     "101",
		"title":          "Foreign Upload",
		"description":    "synthetic fixture",
		"is_public":      "on",
		"allow_comments": "on",
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}
	file, err := writer.CreateFormFile("video", "synthetic.mp4")
	if err != nil {
		t.Fatalf("create multipart video: %v", err)
	}
	if _, err := file.Write([]byte("SYNTHETIC-FOREIGN-UPLOAD")); err != nil {
		t.Fatalf("write multipart video: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("upload to foreign channel status = %d, want 403; body=%q",
			rec.Code, rec.Body.String())
	}
	if n := countRows(t, s.db, "videos"); n != 0 {
		t.Errorf("videos = %d, want 0 after rejected upload", n)
	}
	if _, err := os.Stat(filepath.Join("static", "uploads")); !os.IsNotExist(err) {
		t.Errorf("uploads path stat error = %v, want not-exist", err)
	}
}
