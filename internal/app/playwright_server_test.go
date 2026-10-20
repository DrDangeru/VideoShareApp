//go:build playwright

package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

const e2eFixturePassword = "E2e-fixture-password-42"

type e2eFixtures struct {
	Ready            bool              `json:"ready"`
	Users            map[string]int64  `json:"users"`
	Channels         map[string]string `json:"channels"`
	Videos           map[string]int64  `json:"videos"`
	Media            map[string]string `json:"media"`
	Uploads          map[string]string `json:"uploads"`
	Avatars          map[string]string `json:"avatars"`
	ThumbnailDataURL string            `json:"thumbnailDataURL"`
}

func TestPlaywrightServer(t *testing.T) {
	if os.Getenv("VIDEO_SHARE_E2E") != "1" {
		t.Skip("set VIDEO_SHARE_E2E=1 to run the disposable Playwright fixture server")
	}

	s := newTestServerWithTemplates(
		t,
		"home.html",
		"dashboard.html",
		"auth.html",
		"watch.html",
		"channel.html",
		"edit_video.html",
		"edit_channel.html",
	)
	if err := seedData(s.db); err != nil {
		t.Fatalf("seedData: %v", err)
	}
	copyE2EStaticAssets(t)

	ownerID := insertUser(t, s.db, "owner@e2e.test", e2eFixturePassword, "Sofia", false)
	otherID := insertUser(t, s.db, "other@e2e.test", e2eFixturePassword, "Sofia", false)
	adminID := insertUser(t, s.db, "admin@e2e.test", e2eFixturePassword, "Sofia", true)
	insertChannel(t, s.db, "101", "E2E Open Channel", "Sofia", ownerID, false)
	insertChannel(t, s.db, "102", "E2E Locked Channel", "Sofia", ownerID, true)

	fixtures := e2eFixtures{
		Ready: true,
		Users: map[string]int64{
			"owner": ownerID,
			"other": otherID,
			"admin": adminID,
		},
		Channels: map[string]string{
			"open":   "101",
			"locked": "102",
		},
		Videos: map[string]int64{
			"public":           101,
			"private":          102,
			"videoLocked":      103,
			"channelLocked":    104,
			"commentsDisabled": 105,
		},
		Media:   make(map[string]string),
		Uploads: make(map[string]string),
		Avatars: map[string]string{
			"open":   "/static/uploads/channels/101/avatar.jpg",
			"locked": "/static/uploads/channels/102/avatar.jpg",
		},
		ThumbnailDataURL: testThumbnailDataURL(t, "jpeg", 640, 360),
	}
	videoFixtures := []testVideoRow{
		{
			id: 101, channelID: "101", title: "E2E Public Video", description: "Synthetic public fixture",
			mediaURL: "/media/e2e-101.mp4", thumbURL: e2eUploadURL(ownerID, 101),
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
		{
			id: 102, channelID: "101", title: "E2E Private Video", description: "Synthetic private fixture",
			mediaURL: "/media/e2e-102.mp4", thumbURL: e2eUploadURL(ownerID, 102),
			location: "Sofia", userID: ownerID, isPublic: 0,
		},
		{
			id: 103, channelID: "101", title: "E2E Video Locked", description: "Synthetic locked fixture",
			mediaURL: "/media/e2e-103.mp4", thumbURL: e2eUploadURL(ownerID, 103),
			location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
		},
		{
			id: 104, channelID: "102", title: "E2E Channel Locked", description: "Synthetic locked fixture",
			mediaURL: "/media/e2e-104.mp4", thumbURL: e2eUploadURL(ownerID, 104),
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
		{
			id: 105, channelID: "101", title: "E2E Comments Disabled", description: "Synthetic fixture",
			mediaURL: "/media/e2e-105.mp4", thumbURL: e2eUploadURL(ownerID, 105),
			location: "Sofia", userID: ownerID, isPublic: 1,
		},
	}
	for _, video := range videoFixtures {
		insertVideo(t, s.db, video)
		mediaMarker := []byte("SYNTHETIC-E2E-MEDIA-" + strconv.FormatInt(video.id, 10))
		uploadMarker := []byte("SYNTHETIC-E2E-UPLOAD-" + strconv.FormatInt(video.id, 10))
		writeTestFile(t, filepath.Join("Videos", "e2e-"+strconv.FormatInt(video.id, 10)+".mp4"), mediaMarker)
		writeTestFile(
			t,
			filepath.Join("static", "uploads", "users", strconv.FormatInt(ownerID, 10),
				strconv.FormatInt(video.id, 10), "original.jpg"),
			uploadMarker,
		)
		fixtures.Media[strconv.FormatInt(video.id, 10)] = video.mediaURL
		fixtures.Uploads[strconv.FormatInt(video.id, 10)] = video.thumbURL
	}
	if _, err := s.db.Exec(`update videos set allow_comments = 0 where id = 105`); err != nil {
		t.Fatalf("disable comments on fixture video: %v", err)
	}

	for _, channel := range []struct {
		id     string
		marker string
		avatar string
	}{
		{"101", "SYNTHETIC-E2E-OPEN-AVATAR", fixtures.Avatars["open"]},
		{"102", "SYNTHETIC-E2E-LOCKED-AVATAR", fixtures.Avatars["locked"]},
	} {
		if _, err := s.db.Exec(`update channels set avatar_url = ? where id = ?`, channel.avatar, channel.id); err != nil {
			t.Fatalf("set fixture avatar for channel %s: %v", channel.id, err)
		}
		writeTestFile(
			t,
			filepath.Join("static", "uploads", "channels", channel.id, "avatar.jpg"),
			[]byte(channel.marker),
		)
	}
	writeTestFile(t, filepath.Join("Videos", "unregistered.mp4"), []byte("SYNTHETIC-E2E-UNREGISTERED-MEDIA"))
	writeTestFile(
		t,
		filepath.Join("static", "uploads", "users", strconv.FormatInt(ownerID, 10), "999", "original.mp4"),
		[]byte("SYNTHETIC-E2E-UNREGISTERED-UPLOAD"),
	)

	s.mux.HandleFunc("/__e2e/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fixtures); err != nil {
			t.Errorf("encode readiness response: %v", err)
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:18765")
	if err != nil {
		t.Fatalf("listen on local E2E address: %v", err)
	}
	server := &http.Server{Handler: s.mux, ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	})

	stop := make(chan struct{})
	serveErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			serveErrors <- err
			return
		}
		serveErrors <- nil
	}()
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stop)
	}()
	select {
	case <-stop:
	case err := <-serveErrors:
		if err != nil {
			t.Fatalf("serve E2E fixture server: %v", err)
		}
		t.Fatal("E2E fixture server stopped before runner shutdown")
	}
}

func copyE2EStaticAssets(t *testing.T) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	projectDirectory := filepath.Join(filepath.Dir(currentFile), "..", "..")
	for _, relative := range []string{
		filepath.Join("static", "modern.css"),
		filepath.Join("static", "style.css"),
		filepath.Join("static", "preroll.js"),
		filepath.Join("static", "img", "placeholder.svg"),
	} {
		contents, err := os.ReadFile(filepath.Join(projectDirectory, relative))
		if err != nil {
			t.Fatalf("read public test asset %s: %v", relative, err)
		}
		destination := filepath.Join(".", relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatalf("create test asset directory %s: %v", destination, err)
		}
		if err := os.WriteFile(destination, contents, 0o644); err != nil {
			t.Fatalf("write test asset %s: %v", destination, err)
		}
	}
	writeTestFile(
		t,
		filepath.Join("static", "img", "yourAddColorMini.jpg"),
		[]byte("SYNTHETIC-E2E-AD-PLACEHOLDER"),
	)
}

func e2eUploadURL(ownerID, videoID int64) string {
	return "/static/uploads/users/" + strconv.FormatInt(ownerID, 10) +
		"/" + strconv.FormatInt(videoID, 10) + "/original.jpg"
}
