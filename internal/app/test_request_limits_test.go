package app

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestParseRequestFormLimitsAndTypes(t *testing.T) {
	const bodyLimit int64 = 64
	exactBody := "x=" + strings.Repeat("a", int(bodyLimit)-2)
	request := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(exactBody))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	cleanup, err := parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 32)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("parse exact-limit form: %v", err)
	}
	if got := request.FormValue("x"); len(got) != int(bodyLimit)-2 {
		t.Errorf("exact-limit form value length = %d", len(got))
	}

	request = httptest.NewRequest(http.MethodPost, "/save", nil)
	request.ContentLength = bodyLimit + 1
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 32)
	assertMaxBytesError(t, err)

	request = httptest.NewRequest(
		http.MethodPost,
		"/save",
		strings.NewReader(strings.Repeat("a", int(bodyLimit)+1)),
	)
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 32)
	assertMaxBytesError(t, err)

	request = httptest.NewRequest(http.MethodPost, "/save", strings.NewReader("x=%zz"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 32)
	if err == nil {
		t.Fatal("malformed URL-encoded body unexpectedly parsed")
	}
	rec := httptest.NewRecorder()
	rejectFormParseError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed form status = %d, want 400", rec.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/action", nil)
	if cleanup, err := parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 32); err != nil {
		t.Fatalf("empty action body rejected: %v", err)
	} else if cleanup != nil {
		cleanup()
	}
}

func TestUnsupportedFormBodyCannotFallBackToQuery(t *testing.T) {
	s := newTestServer(t)
	request := httptest.NewRequest(
		http.MethodPost,
		"/register?email=query-fallback%40limits.test&password=synthetic-password&location=Sofia",
		strings.NewReader(`{"email":"body@example.test"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported content type status = %d, want 400", rec.Code)
	}
	if countRows(t, s.db, "users") != 0 || countRows(t, s.db, "sessions") != 0 {
		t.Fatal("unsupported body mutated users or sessions through query parameters")
	}

	request = httptest.NewRequest(
		http.MethodPost,
		"/register?email=query-fallback%40limits.test&password=synthetic-password&location=Sofia",
		strings.NewReader("email=body%40limits.test&password=%zz&location=Sofia"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed form status = %d, want 400", rec.Code)
	}
	if countRows(t, s.db, "users") != 0 || countRows(t, s.db, "sessions") != 0 {
		t.Fatal("malformed body mutated users or sessions through query parameters")
	}

	request = httptest.NewRequest(
		http.MethodPost,
		"/register?email=query-fallback%40limits.test&password=synthetic-password&location=Sofia",
		nil,
	)
	request.ContentLength = 1
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("truncated form status = %d, want 400", rec.Code)
	}
	if countRows(t, s.db, "users") != 0 || countRows(t, s.db, "sessions") != 0 {
		t.Fatal("truncated body mutated users or sessions through query parameters")
	}

	validBody := url.Values{
		"email":    {"body-query-malformed@limits.test"},
		"password": {"synthetic-password"},
		"location": {"Sofia"},
	}.Encode()
	request = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(validBody))
	request.URL.RawQuery = "query-value=%zz"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed query with valid body status = %d, want 400", rec.Code)
	}
	if countRows(t, s.db, "users") != 0 || countRows(t, s.db, "sessions") != 0 {
		t.Fatal("malformed query with valid body mutated users or sessions")
	}

	request = httptest.NewRequest(
		http.MethodPost,
		"/register?email=query-fallback%40limits.test&password=synthetic-password&location=Sofia",
		nil,
	)
	request.URL.RawQuery += "&query-value=%zz"
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed query with empty body status = %d, want 400", rec.Code)
	}
	if countRows(t, s.db, "users") != 0 || countRows(t, s.db, "sessions") != 0 {
		t.Fatal("malformed query with empty body mutated users or sessions")
	}
}

func TestMultipartFormSpoolsFileAndCleansTemporaryFile(t *testing.T) {
	tempDir := useMultipartTempDirectory(t)
	request, fileData := syntheticMultipartFileRequest(t)
	bodyLimit := request.ContentLength
	cleanup, err := parseRequestForm(httptest.NewRecorder(), request, bodyLimit, 8)
	if err != nil {
		t.Fatalf("parse exact-limit multipart form: %v", err)
	}
	if cleanup == nil {
		t.Fatal("multipart parse returned no cleanup function")
	}
	fileHeaders := request.MultipartForm.File["upload"]
	if len(fileHeaders) != 1 {
		t.Fatalf("file part count = %d, want 1", len(fileHeaders))
	}
	file, err := fileHeaders[0].Open()
	if err != nil {
		t.Fatalf("open spooled file: %v", err)
	}
	got, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close spooled file: %v / %v", readErr, closeErr)
	}
	if !bytes.Equal(got, fileData) {
		t.Error("spooled file data differs from the synthetic fixture")
	}
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) == 0 {
		t.Fatalf("multipart did not spool a temp file: entries=%d err=%v", len(entries), err)
	}
	cleanup()
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) != 0 {
		t.Errorf("temporary files after cleanup = %d, err=%v; want none", len(entries), err)
	}
}

func TestChunkedMultipartOverflowCleansTemporaryFiles(t *testing.T) {
	tempDir := useMultipartTempDirectory(t)
	request, _ := syntheticMultipartFileRequest(t)
	bodyLimit := request.ContentLength - 1
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	rec := httptest.NewRecorder()
	cleanup, err := parseRequestForm(rec, request, bodyLimit, 8)
	if cleanup != nil {
		cleanup()
	}
	assertMaxBytesError(t, err)
	rejectFormParseError(rec, err)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked multipart overflow status = %d, want 413", rec.Code)
	}
	if entries, err := os.ReadDir(tempDir); err != nil || len(entries) != 0 {
		t.Errorf("temporary files after overflow = %d, err=%v; want none", len(entries), err)
	}
}

func useMultipartTempDirectory(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
	return tempDir
}

func syntheticMultipartFileRequest(t *testing.T) (*http.Request, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileData := bytes.Repeat([]byte("synthetic-upload-part"), 16)
	file, err := writer.CreateFormFile("upload", "synthetic.bin")
	if err != nil {
		t.Fatalf("create multipart file part: %v", err)
	}
	if _, err := file.Write(fileData); err != nil {
		t.Fatalf("write multipart file part: %v", err)
	}
	if err := writer.WriteField("padding", strings.Repeat("p", 128)); err != nil {
		t.Fatalf("write multipart padding field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request, fileData
}

func TestPOSTRoutesEnforceConfiguredBodyLimits(t *testing.T) {
	s := newTestServer(t)
	userID := insertUser(t, s.db, "request-limits@security.test", "synthetic-password", "Sofia", false)
	cookie := newSessionCookie(t, s, userID)
	routes := []struct {
		path         string
		limit        int64
		authRequired bool
		contentType  string
	}{
		// Literal byte limits, deliberately not derived from the production
		// constants: a mutation that changes a constant must fail this test.
		{"/register", 1<<20 + 1, false, "application/x-www-form-urlencoded"},
		{"/login", 1<<20 + 1, false, "application/x-www-form-urlencoded"},
		{"/upload", 512<<20 + 1, true, "multipart/form-data; boundary=synthetic"},
		{"/edit-video/1", 10<<20 + 1, true, "application/x-www-form-urlencoded"},
		{"/edit-channel/101", 10<<20 + 1, true, "application/x-www-form-urlencoded"},
		{"/add-comment", 1<<20 + 1, true, "application/x-www-form-urlencoded"},
		{"/delete-comment/1", 1<<20 + 1, true, "application/x-www-form-urlencoded"},
		{"/videos/1/favorite", 1<<20 + 1, true, "application/x-www-form-urlencoded"},
		{"/channels/101/subscribe", 1<<20 + 1, true, "application/x-www-form-urlencoded"},
	}
	for _, tc := range routes {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, tc.path, nil)
			request.ContentLength = tc.limit + 1
			request.Header.Set("Content-Type", tc.contentType)
			if tc.authRequired {
				request.AddCookie(cookie)
			}
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, request)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("POST %s status = %d, want 413; body=%q", tc.path, rec.Code, rec.Body.String())
			}
		})
	}

	anonymousUpload := httptest.NewRequest(http.MethodPost, "/upload", nil)
	anonymousUpload.ContentLength = uploadBodyLimit + 1
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, anonymousUpload)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("unauthenticated upload response = %d location %q, want redirect before parsing",
			rec.Code, rec.Header().Get("Location"))
	}

	if n := countRows(t, s.db, "channels"); n != 0 {
		t.Errorf("oversized requests created %d channels", n)
	}
	if n := countRows(t, s.db, "videos"); n != 0 {
		t.Errorf("oversized requests created %d videos", n)
	}
	if n := countRows(t, s.db, "favorites"); n != 0 {
		t.Errorf("oversized requests created %d favorites", n)
	}
}

func assertMaxBytesError(t *testing.T, err error) {
	t.Helper()
	var maxBytesError *http.MaxBytesError
	if !errors.As(err, &maxBytesError) {
		t.Fatalf("error = %v, want *http.MaxBytesError", err)
	}
}
