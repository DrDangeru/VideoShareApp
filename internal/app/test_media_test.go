package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestMediaPermissions(t *testing.T) {
	s := newTestServerInTempDir(t)
	db := s.db

	ownerID := insertUser(t, db, "owner@media.test", "pw", "Sofia", false)
	otherID := insertUser(t, db, "other@media.test", "pw", "Sofia", false)
	adminID := insertUser(t, db, "admin@media.test", "pw", "Sofia", true)

	insertChannel(t, db, "1", "Open Channel", "Sofia", ownerID, false)
	insertChannel(t, db, "2", "Locked Channel", "Sofia", ownerID, true)

	publicBytes := []byte("PUBLIC-VIDEO-BYTES")
	privateBytes := []byte("PRIVATE-VIDEO-BYTES")
	vlockedBytes := []byte("VLOCKED-VIDEO-BYTES")
	clockedBytes := []byte("CLOCKED-VIDEO-BYTES")
	thumbBytes := []byte("PRIVATE-THUMB-BYTES")
	uploadBytes := []byte("UPLOAD-PRIVATE-BYTES")
	uploadThumbBytes := []byte("UPLOAD-THUMB-BYTES")
	uploadPublicBytes := []byte("UPLOAD-PUBLIC-BYTES")
	uploadVlockedBytes := []byte("UPLOAD-VLOCKED-BYTES")
	uploadClockedBytes := []byte("UPLOAD-CLOCKED-BYTES")

	writeTestFile(t, "Videos/public.mp4", publicBytes)
	writeTestFile(t, "Videos/private.mp4", privateBytes)
	writeTestFile(t, "Videos/private.jpg", thumbBytes)
	writeTestFile(t, "Videos/vlocked.mp4", vlockedBytes)
	writeTestFile(t, "Videos/clocked.mp4", clockedBytes)
	writeTestFile(t, "static/uploads/users/1/5/original.mp4", uploadBytes)
	writeTestFile(t, "static/uploads/users/1/5/original.jpg", uploadThumbBytes)
	writeTestFile(t, "static/uploads/users/1/6/original.mp4", uploadPublicBytes)
	writeTestFile(t, "static/uploads/users/1/7/original.mp4", uploadVlockedBytes)
	writeTestFile(t, "static/uploads/users/1/8/original.mp4", uploadClockedBytes)

	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "pub", description: "d",
		mediaURL: "/media/public.mp4", thumbURL: "/media/public.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 2, channelID: "1", title: "priv", description: "d",
		mediaURL: "/media/private.mp4", thumbURL: "/media/private.jpg",
		location: "Sofia", userID: ownerID, isPublic: 0,
	})
	insertVideo(t, db, testVideoRow{
		id: 3, channelID: "1", title: "vlock", description: "d",
		mediaURL: "/media/vlocked.mp4", thumbURL: "/media/vlocked.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 4, channelID: "2", title: "clock", description: "d",
		mediaURL: "/media/clocked.mp4", thumbURL: "/media/clocked.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 5, channelID: "1", title: "upload", description: "d",
		mediaURL: "/static/uploads/users/1/5/original.mp4",
		thumbURL: "/static/uploads/users/1/5/original.jpg",
		location: "Sofia", userID: ownerID, isPublic: 0,
	})
	insertVideo(t, db, testVideoRow{
		id: 6, channelID: "1", title: "upub", description: "d",
		mediaURL: "/static/uploads/users/1/6/original.mp4",
		thumbURL: "/static/uploads/users/1/6/original.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 7, channelID: "1", title: "uvlock", description: "d",
		mediaURL: "/static/uploads/users/1/7/original.mp4",
		thumbURL: "/static/uploads/users/1/7/original.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1, adminLocked: 1,
	})
	insertVideo(t, db, testVideoRow{
		id: 8, channelID: "2", title: "uclock", description: "d",
		mediaURL: "/static/uploads/users/1/8/original.mp4",
		thumbURL: "/static/uploads/users/1/8/original.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})

	ownerCookie := newSessionCookie(t, s, ownerID)
	otherCookie := newSessionCookie(t, s, otherID)
	adminCookie := newSessionCookie(t, s, adminID)

	cases := []struct {
		name    string
		path    string
		cookie  *http.Cookie
		status  int
		content []byte
	}{
		{"public anonymous", "/media/public.mp4", nil, http.StatusOK, publicBytes},
		{"public other", "/media/public.mp4", otherCookie, http.StatusOK, publicBytes},
		{"private anonymous", "/media/private.mp4", nil, http.StatusForbidden, privateBytes},
		{"private other", "/media/private.mp4", otherCookie, http.StatusForbidden, privateBytes},
		{"private owner", "/media/private.mp4", ownerCookie, http.StatusOK, privateBytes},
		{"private admin", "/media/private.mp4", adminCookie, http.StatusOK, privateBytes},
		{"private thumb anonymous", "/media/private.jpg", nil, http.StatusForbidden, thumbBytes},
		{"private thumb owner", "/media/private.jpg", ownerCookie, http.StatusOK, thumbBytes},
		{"video locked anonymous", "/media/vlocked.mp4", nil, http.StatusForbidden, vlockedBytes},
		{"video locked other", "/media/vlocked.mp4", otherCookie, http.StatusForbidden, vlockedBytes},
		{"video locked owner", "/media/vlocked.mp4", ownerCookie, http.StatusOK, vlockedBytes},
		{"video locked admin", "/media/vlocked.mp4", adminCookie, http.StatusOK, vlockedBytes},
		{"channel locked anonymous", "/media/clocked.mp4", nil, http.StatusForbidden, clockedBytes},
		{"channel locked other", "/media/clocked.mp4", otherCookie, http.StatusForbidden, clockedBytes},
		{"channel locked owner", "/media/clocked.mp4", ownerCookie, http.StatusOK, clockedBytes},
		{"channel locked admin", "/media/clocked.mp4", adminCookie, http.StatusOK, clockedBytes},
		{
			"upload private anonymous", "/static/uploads/users/1/5/original.mp4",
			nil, http.StatusForbidden, uploadBytes,
		},
		{
			"upload private other", "/static/uploads/users/1/5/original.mp4",
			otherCookie, http.StatusForbidden, uploadBytes,
		},
		{
			"upload private owner", "/static/uploads/users/1/5/original.mp4",
			ownerCookie, http.StatusOK, uploadBytes,
		},
		{
			"upload private admin", "/static/uploads/users/1/5/original.mp4",
			adminCookie, http.StatusOK, uploadBytes,
		},
		{
			"upload private thumb anonymous", "/static/uploads/users/1/5/original.jpg",
			nil, http.StatusForbidden, uploadThumbBytes,
		},
		{
			"upload private thumb owner", "/static/uploads/users/1/5/original.jpg",
			ownerCookie, http.StatusOK, uploadThumbBytes,
		},
		{
			"upload private thumb admin", "/static/uploads/users/1/5/original.jpg",
			adminCookie, http.StatusOK, uploadThumbBytes,
		},
		{
			"upload public anonymous", "/static/uploads/users/1/6/original.mp4",
			nil, http.StatusOK, uploadPublicBytes,
		},
		{
			"upload public other", "/static/uploads/users/1/6/original.mp4",
			otherCookie, http.StatusOK, uploadPublicBytes,
		},
		{
			"upload vlocked anonymous", "/static/uploads/users/1/7/original.mp4",
			nil, http.StatusForbidden, uploadVlockedBytes,
		},
		{
			"upload vlocked other", "/static/uploads/users/1/7/original.mp4",
			otherCookie, http.StatusForbidden, uploadVlockedBytes,
		},
		{
			"upload vlocked owner", "/static/uploads/users/1/7/original.mp4",
			ownerCookie, http.StatusOK, uploadVlockedBytes,
		},
		{
			"upload vlocked admin", "/static/uploads/users/1/7/original.mp4",
			adminCookie, http.StatusOK, uploadVlockedBytes,
		},
		{
			"upload clocked anonymous", "/static/uploads/users/1/8/original.mp4",
			nil, http.StatusForbidden, uploadClockedBytes,
		},
		{
			"upload clocked other", "/static/uploads/users/1/8/original.mp4",
			otherCookie, http.StatusForbidden, uploadClockedBytes,
		},
		{
			"upload clocked owner", "/static/uploads/users/1/8/original.mp4",
			ownerCookie, http.StatusOK, uploadClockedBytes,
		},
		{
			"upload clocked admin", "/static/uploads/users/1/8/original.mp4",
			adminCookie, http.StatusOK, uploadClockedBytes,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, tc.path, nil, cookies...)
			if rec.Code != tc.status {
				t.Fatalf("GET %s status = %d, want %d", tc.path, rec.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				if rec.Body.String() != string(tc.content) {
					t.Errorf("body = %q, want %q", rec.Body.String(), string(tc.content))
				}
			} else if strings.Contains(rec.Body.String(), string(tc.content)) {
				t.Error("denied response leaked content")
			}
		})
	}
}
