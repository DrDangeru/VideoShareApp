package app

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) staticHandler() http.Handler {
	fileServer := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only protect user uploads; let regular static assets pass through
		if strings.HasPrefix(r.URL.Path, "/static/uploads/") {
			mediaURL := r.URL.Path

			var channelAdminLocked int
			var isPublic int
			var videoAdminLocked int
			var channelOwnerID int64
			err := s.db.QueryRow(`
				select c.is_admin_locked, v.is_public, v.is_admin_locked, c.user_id
				from videos v
				join channels c on c.id = v.channel_id
				where v.media_url = ? or v.thumbnail_url = ?
			`, mediaURL, mediaURL).Scan(&channelAdminLocked, &isPublic, &videoAdminLocked, &channelOwnerID)

			if err == nil {
				user, _ := s.currentUser(r)
				isAdmin := user != nil && user.IsAdmin
				isOwner := user != nil && user.ID == channelOwnerID

				if !isAdmin && !isOwner {
					if channelAdminLocked == 1 || videoAdminLocked == 1 || isPublic == 0 {
						http.Error(w, "Forbidden", http.StatusForbidden)
						return
					}
				}
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) mediaHandler(w http.ResponseWriter, r *http.Request) {
	// Reconstruct the media URL to match what is stored in the database
	mediaURL := r.URL.Path // e.g. /media/VID_...

	// Find the video and channel to check permissions
	var channelAdminLocked int
	var isPublic int
	var videoAdminLocked int
	var channelOwnerID int64
	err := s.db.QueryRow(`
		select c.is_admin_locked, v.is_public, v.is_admin_locked, c.user_id
		from videos v
		join channels c on c.id = v.channel_id
		where v.media_url = ? or v.thumbnail_url = ?
	`, mediaURL, mediaURL).Scan(&channelAdminLocked, &isPublic, &videoAdminLocked, &channelOwnerID)

	if err != nil {
		if err == sql.ErrNoRows {
			// If not found in DB, just serve it (might be a generic asset)
			http.ServeFile(w, r, filepath.Join("Videos", strings.TrimPrefix(mediaURL, "/media/")))
			return
		}
		s.serverError(w, err)
		return
	}

	user, _ := s.currentUser(r)
	isAdmin := user != nil && user.IsAdmin
	isOwner := user != nil && user.ID == channelOwnerID

	// Check if video is locked or private
	if !isAdmin && !isOwner {
		if channelAdminLocked == 1 || videoAdminLocked == 1 || isPublic == 0 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Serve the file
	http.ServeFile(w, r, filepath.Join("Videos", strings.TrimPrefix(mediaURL, "/media/")))
}
