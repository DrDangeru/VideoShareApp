package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSecuritySharedMediaURLs(t *testing.T) {
	t.Run("shared public and private URL in either insert order", func(t *testing.T) {
		for _, privateFirst := range []bool{false, true} {
			t.Run(map[bool]string{false: "public first", true: "private first"}[privateFirst], func(t *testing.T) {
				s := newTestServerInTempDir(t)
				db := s.db
				ownerID := insertUser(t, db, "shared-owner@media.test", "synthetic-password", "Sofia", false)
				otherID := insertUser(t, db, "shared-other@media.test", "synthetic-password", "Sofia", false)
				adminID := insertUser(t, db, "shared-admin@media.test", "synthetic-password", "Sofia", true)
				insertChannel(t, db, "101", "Shared Public Channel", "Sofia", ownerID, false)
				insertChannel(t, db, "202", "Shared Private Channel", "Sofia", otherID, false)

				const mediaURL = "/media/shared-public-private.mp4"
				const marker = "SYNTHETIC-SHARED-PUBLIC-PRIVATE"
				writeTestFile(t, "Videos/shared-public-private.mp4", []byte(marker))
				publicID, privateID := int64(1), int64(2)
				if privateFirst {
					publicID, privateID = privateID, publicID
				}
				public := testVideoRow{
					id: publicID, channelID: "101", title: "Shared Public", description: "fixture",
					mediaURL: mediaURL, thumbURL: "/media/shared-public.jpg",
					location: "Sofia", userID: ownerID, isPublic: 1,
				}
				private := testVideoRow{
					id: privateID, channelID: "202", title: "Shared Private", description: "fixture",
					mediaURL: mediaURL, thumbURL: "/media/shared-private.jpg",
					location: "Sofia", userID: otherID, isPublic: 0,
				}
				if privateFirst {
					insertVideo(t, db, private)
					insertVideo(t, db, public)
				} else {
					insertVideo(t, db, public)
					insertVideo(t, db, private)
				}
				var firstIsPublic int
				if err := db.QueryRow(
					`select is_public from videos where media_url = ? order by id limit 1`,
					mediaURL,
				).Scan(&firstIsPublic); err != nil {
					t.Fatalf("query first shared video: %v", err)
				}
				wantFirstIsPublic := 1
				if privateFirst {
					wantFirstIsPublic = 0
				}
				if firstIsPublic != wantFirstIsPublic {
					t.Fatalf("first shared row is_public = %d, want %d", firstIsPublic, wantFirstIsPublic)
				}

				assertSharedMediaAccess(t, s, mediaURL, marker, []sharedMediaAccess{
					{"anonymous", nil, http.StatusForbidden},
					{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
					{"public owner not private owner", newSessionCookie(t, s, ownerID), http.StatusForbidden},
					{"private owner", newSessionCookie(t, s, otherID), http.StatusOK},
				})
			})
		}
	})

	t.Run("shared public URL across different owners remains public", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		firstOwnerID := insertUser(t, db, "public-first@media.test", "synthetic-password", "Sofia", false)
		secondOwnerID := insertUser(t, db, "public-second@media.test", "synthetic-password", "Sofia", false)
		otherID := insertUser(t, db, "public-other@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "public-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "First Public Channel", "Sofia", firstOwnerID, false)
		insertChannel(t, db, "202", "Second Public Channel", "Sofia", secondOwnerID, false)

		const sharedURL = "/media/shared-public-owners.mp4"
		const marker = "SYNTHETIC-SHARED-PUBLIC-OWNERS"
		writeTestFile(t, "Videos/shared-public-owners.mp4", []byte(marker))
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Public Owner One", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/public-owner-one.jpg",
			location: "Sofia", userID: firstOwnerID, isPublic: 1,
		})
		insertVideo(t, db, testVideoRow{
			id: 2, channelID: "202", title: "Public Owner Two", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/public-owner-two.jpg",
			location: "Sofia", userID: secondOwnerID, isPublic: 1,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusOK},
			{"first owner", newSessionCookie(t, s, firstOwnerID), http.StatusOK},
			{"second owner", newSessionCookie(t, s, secondOwnerID), http.StatusOK},
			{"other user", newSessionCookie(t, s, otherID), http.StatusOK},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("private URL across different owners requires access to every owner", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		firstOwnerID := insertUser(t, db, "private-first@media.test", "synthetic-password", "Sofia", false)
		secondOwnerID := insertUser(t, db, "private-second@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "private-shared-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "First Private Channel", "Sofia", firstOwnerID, false)
		insertChannel(t, db, "202", "Second Private Channel", "Sofia", secondOwnerID, false)

		const sharedURL = "/media/shared-private-owners.mp4"
		const marker = "SYNTHETIC-SHARED-PRIVATE-OWNERS"
		writeTestFile(t, "Videos/shared-private-owners.mp4", []byte(marker))
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Private Owner One", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/private-owner-one.jpg",
			location: "Sofia", userID: firstOwnerID, isPublic: 0,
		})
		insertVideo(t, db, testVideoRow{
			id: 2, channelID: "202", title: "Private Owner Two", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/private-owner-two.jpg",
			location: "Sofia", userID: secondOwnerID, isPublic: 0,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"first private owner", newSessionCookie(t, s, firstOwnerID), http.StatusForbidden},
			{"second private owner", newSessionCookie(t, s, secondOwnerID), http.StatusForbidden},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("public media URL shared with private thumbnail", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		publicOwnerID := insertUser(t, db, "thumbnail-public@media.test", "synthetic-password", "Sofia", false)
		privateOwnerID := insertUser(t, db, "thumbnail-private@media.test", "synthetic-password", "Sofia", false)
		otherID := insertUser(t, db, "thumbnail-other@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "thumbnail-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "Public Thumbnail Channel", "Sofia", publicOwnerID, false)
		insertChannel(t, db, "202", "Private Media Channel", "Sofia", privateOwnerID, false)

		const sharedURL = "/media/public-thumbnail-private-media.jpg"
		const marker = "SYNTHETIC-PUBLIC-THUMB-PRIVATE-MEDIA"
		writeTestFile(t, "Videos/public-thumbnail-private-media.jpg", []byte(marker))
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Public Thumbnail", description: "fixture",
			mediaURL: "/media/public-thumbnail.mp4", thumbURL: sharedURL,
			location: "Sofia", userID: publicOwnerID, isPublic: 1,
		})
		insertVideo(t, db, testVideoRow{
			id: 2, channelID: "202", title: "Private Media", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/private-media.jpg",
			location: "Sofia", userID: privateOwnerID, isPublic: 0,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"unrelated user", newSessionCookie(t, s, otherID), http.StatusForbidden},
			{"public thumbnail owner", newSessionCookie(t, s, publicOwnerID), http.StatusForbidden},
			{"private media owner", newSessionCookie(t, s, privateOwnerID), http.StatusOK},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("shared public and video-locked URL", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		publicOwnerID := insertUser(t, db, "locked-public@media.test", "synthetic-password", "Sofia", false)
		lockedOwnerID := insertUser(t, db, "locked-owner@media.test", "synthetic-password", "Sofia", false)
		otherID := insertUser(t, db, "locked-other@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "locked-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "Public Video Channel", "Sofia", publicOwnerID, false)
		insertChannel(t, db, "202", "Video Lock Channel", "Sofia", lockedOwnerID, false)

		const sharedURL = "/media/shared-public-locked.mp4"
		const marker = "SYNTHETIC-SHARED-PUBLIC-LOCKED"
		writeTestFile(t, "Videos/shared-public-locked.mp4", []byte(marker))
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Public Video", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/public-shared.jpg",
			location: "Sofia", userID: publicOwnerID, isPublic: 1,
		})
		insertVideo(t, db, testVideoRow{
			id: 2, channelID: "202", title: "Locked Video", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/locked-shared.jpg",
			location: "Sofia", userID: lockedOwnerID, isPublic: 1, adminLocked: 1,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"unrelated user", newSessionCookie(t, s, otherID), http.StatusForbidden},
			{"public owner", newSessionCookie(t, s, publicOwnerID), http.StatusForbidden},
			{"locked video owner", newSessionCookie(t, s, lockedOwnerID), http.StatusOK},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("shared URL with channel-locked reference", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		publicOwnerID := insertUser(t, db, "channel-lock-public@media.test", "synthetic-password", "Sofia", false)
		lockedOwnerID := insertUser(t, db, "channel-lock-owner@media.test", "synthetic-password", "Sofia", false)
		otherID := insertUser(t, db, "channel-lock-other@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "channel-lock-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "Open Shared URL Channel", "Sofia", publicOwnerID, false)
		insertChannel(t, db, "202", "Locked Shared URL Channel", "Sofia", lockedOwnerID, true)

		const sharedURL = "/media/shared-channel-locked.mp4"
		const marker = "SYNTHETIC-SHARED-CHANNEL-LOCKED"
		writeTestFile(t, "Videos/shared-channel-locked.mp4", []byte(marker))
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Open Channel Video", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/open-channel.jpg",
			location: "Sofia", userID: publicOwnerID, isPublic: 1,
		})
		insertVideo(t, db, testVideoRow{
			id: 2, channelID: "202", title: "Locked Channel Video", description: "fixture",
			mediaURL: sharedURL, thumbURL: "/media/locked-channel.jpg",
			location: "Sofia", userID: lockedOwnerID, isPublic: 1,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"unrelated user", newSessionCookie(t, s, otherID), http.StatusForbidden},
			{"open channel owner", newSessionCookie(t, s, publicOwnerID), http.StatusForbidden},
			{"locked channel owner", newSessionCookie(t, s, lockedOwnerID), http.StatusOK},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("uploaded video shared with locked avatar", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		videoOwnerID := insertUser(t, db, "upload-shared-owner@media.test", "synthetic-password", "Sofia", false)
		avatarOwnerID := insertUser(t, db, "avatar-shared-owner@media.test", "synthetic-password", "Sofia", false)
		otherID := insertUser(t, db, "upload-shared-other@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "upload-shared-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "Upload Public Channel", "Sofia", videoOwnerID, false)
		insertChannel(t, db, "202", "Locked Avatar Channel", "Sofia", avatarOwnerID, true)

		sharedURL := fmt.Sprintf("/static/uploads/users/%d/shared-avatar.jpg", videoOwnerID)
		const marker = "SYNTHETIC-UPLOAD-AVATAR-COLLISION"
		writeTestFile(t, fmt.Sprintf("static/uploads/users/%d/shared-avatar.jpg", videoOwnerID), []byte(marker))
		if _, err := db.Exec(`update channels set avatar_url = ? where id = '202'`, sharedURL); err != nil {
			t.Fatalf("set locked channel avatar: %v", err)
		}
		insertVideo(t, db, testVideoRow{
			id: 1, channelID: "101", title: "Public Upload", description: "fixture",
			mediaURL: "/media/public-upload.mp4", thumbURL: sharedURL,
			location: "Sofia", userID: videoOwnerID, isPublic: 1,
		})
		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"video owner, not avatar owner", newSessionCookie(t, s, videoOwnerID), http.StatusForbidden},
			{"avatar owner", newSessionCookie(t, s, avatarOwnerID), http.StatusOK},
			{"unrelated user", newSessionCookie(t, s, otherID), http.StatusForbidden},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})

	t.Run("uploaded avatar shared by channels with different owners", func(t *testing.T) {
		s := newTestServerInTempDir(t)
		db := s.db
		firstOwnerID := insertUser(t, db, "avatar-first@media.test", "synthetic-password", "Sofia", false)
		secondOwnerID := insertUser(t, db, "avatar-second@media.test", "synthetic-password", "Sofia", false)
		adminID := insertUser(t, db, "avatar-shared-admin@media.test", "synthetic-password", "Sofia", true)
		insertChannel(t, db, "101", "First Avatar Channel", "Sofia", firstOwnerID, true)
		insertChannel(t, db, "202", "Second Avatar Channel", "Sofia", secondOwnerID, true)
		const sharedURL = "/static/uploads/channels/shared/avatar.jpg"
		const marker = "SYNTHETIC-SHARED-AVATAR"
		writeTestFile(t, "static/uploads/channels/shared/avatar.jpg", []byte(marker))
		if _, err := db.Exec(`update channels set avatar_url = ? where id in ('101', '202')`, sharedURL); err != nil {
			t.Fatalf("set shared avatar URLs: %v", err)
		}

		assertSharedMediaAccess(t, s, sharedURL, marker, []sharedMediaAccess{
			{"anonymous", nil, http.StatusForbidden},
			{"first channel owner", newSessionCookie(t, s, firstOwnerID), http.StatusForbidden},
			{"second channel owner", newSessionCookie(t, s, secondOwnerID), http.StatusForbidden},
			{"admin", newSessionCookie(t, s, adminID), http.StatusOK},
		})
	})
}

type sharedMediaAccess struct {
	name   string
	cookie *http.Cookie
	status int
}

func assertSharedMediaAccess(
	t *testing.T,
	s *Server,
	url, marker string,
	cases []sharedMediaAccess,
) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, url, nil, cookies...)
			if rec.Code != tc.status {
				t.Fatalf("GET %s status = %d, want %d", url, rec.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				if rec.Body.String() != marker {
					t.Errorf("GET %s body = %q, want synthetic marker", url, rec.Body.String())
				}
			} else if strings.Contains(rec.Body.String(), marker) {
				t.Errorf("GET %s leaked synthetic bytes to %s", url, tc.name)
			}
		})
	}
}
