package app

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	defaultFormBodyLimit int64 = 1 << 20
	editFormBodyLimit    int64 = 10 << 20
	uploadBodyLimit      int64 = 512 << 20
	multipartMemoryLimit int64 = 8 << 20
)

func parseRequestForm(
	w http.ResponseWriter,
	r *http.Request,
	bodyLimit, memoryLimit int64,
) (func(), error) {
	if r.ContentLength > bodyLimit {
		return nil, &http.MaxBytesError{Limit: bodyLimit}
	}
	if r.Body == nil {
		r.Body = http.NoBody
	}
	if r.Body == http.NoBody && r.ContentLength > 0 {
		return nil, io.ErrUnexpectedEOF
	}
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if r.Body == http.NoBody && contentType == "" {
		return nil, r.ParseForm()
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	switch mediaType {
	case "application/x-www-form-urlencoded":
		err = r.ParseForm()
	case "multipart/form-data":
		err = r.ParseMultipartForm(memoryLimit)
	default:
		err = errors.New("unsupported form content type")
	}

	var cleanup func()
	if r.MultipartForm != nil {
		cleanup = func() { _ = r.MultipartForm.RemoveAll() }
	}
	return cleanup, err
}

func rejectFormParseError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "Invalid form data", http.StatusBadRequest)
}
