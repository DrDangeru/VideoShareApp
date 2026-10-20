package app

import (
	"net/http"
	"path/filepath"
	"strings"
)

func validFileRequestPath(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) || strings.ContainsAny(path, `\:`) {
		return false
	}

	relative := strings.TrimPrefix(path, prefix)
	if relative == "" {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." ||
			strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return false
		}
	}
	return true
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validFileRequestPath(r.URL.Path, "/static/") {
			http.NotFound(w, r)
			return
		}

		lowerPath := strings.ToLower(r.URL.Path)
		isUpload := lowerPath == "/static/uploads" ||
			strings.HasPrefix(lowerPath, "/static/uploads/")
		if isUpload {
			if lowerPath == "/static/uploads" || !s.authorizeMedia(w, r, r.URL.Path, true) {
				if lowerPath == "/static/uploads" {
					http.NotFound(w, r)
				}
				return
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeMedia(
	w http.ResponseWriter,
	r *http.Request,
	mediaURL string,
	allowAvatar bool,
) bool {
	user, _ := s.currentUser(r)
	isAdmin := user != nil && user.IsAdmin

	query := `
		select c.is_admin_locked, v.is_public, v.is_admin_locked, c.user_id
		from videos v
		join channels c on c.id = v.channel_id
		where v.media_url = ? or v.thumbnail_url = ?
	`
	args := []any{mediaURL, mediaURL}
	if allowAvatar {
		query += `
			union all
			select is_admin_locked, 1, 0, user_id
			from channels
			where avatar_url = ?
		`
		args = append(args, mediaURL)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		s.serverError(w, err)
		return false
	}
	defer rows.Close()

	var channelAdminLocked int
	var isPublic int
	var videoAdminLocked int
	var channelOwnerID int64
	matched := false
	denied := false
	for rows.Next() {
		if err := rows.Scan(
			&channelAdminLocked,
			&isPublic,
			&videoAdminLocked,
			&channelOwnerID,
		); err != nil {
			s.serverError(w, err)
			return false
		}
		matched = true
		isOwner := user != nil && user.ID == channelOwnerID
		if !isAdmin && !isOwner &&
			(channelAdminLocked == 1 || videoAdminLocked == 1 || isPublic == 0) {
			denied = true
		}
	}
	if err := rows.Err(); err != nil {
		s.serverError(w, err)
		return false
	}
	if !matched {
		http.NotFound(w, r)
		return false
	}
	if denied {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) mediaHandler(w http.ResponseWriter, r *http.Request) {
	mediaURL := r.URL.Path
	if !validFileRequestPath(mediaURL, "/media/") {
		http.NotFound(w, r)
		return
	}
	if !s.authorizeMedia(w, r, mediaURL, false) {
		return
	}

	http.ServeFile(w, r, filepath.Join("Videos", strings.TrimPrefix(mediaURL, "/media/")))
}
