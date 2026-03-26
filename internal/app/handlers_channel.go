package app

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) channelPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 || parts[2] == "" {
		http.NotFound(w, r)
		return
	}

	channelID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var ch Channel
	err = s.db.QueryRow(`
		select id, name, location, description, avatar_url, user_id, is_admin_locked
		from channels
		where id = ?
	`, channelID).Scan(
		&ch.ID,
		&ch.Name,
		&ch.Location,
		&ch.Description,
		&ch.AvatarURL,
		&ch.OwnerUserID,
		&ch.IsAdminLocked,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, err)
		return
	}

	user, _ := s.currentUser(r)
	var isSubscribed bool
	if user != nil {
		_ = s.db.QueryRow(
			`select exists(
				select 1 from subscriptions where user_id = ? and channel_id = ?
			)`,
			user.ID,
			channelID,
		).Scan(&isSubscribed)
	}

	var query string
	var args []any
	if user != nil && user.IsAdmin {
		query = `
			select
				v.id, v.channel_id, v.title, v.description,
				v.media_url, v.thumbnail_url, v.location, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				?,
				v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.channel_id = ?
			order by v.id desc
		`
		args = []any{user.ID, isSubscribed, channelID}
	} else if user != nil {
		query = `
			select
				v.id, v.channel_id, v.title, v.description,
				v.media_url, v.thumbnail_url, v.location, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				?,
				v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.channel_id = ? and (v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0)
			order by v.id desc
		`
		args = []any{user.ID, isSubscribed, channelID}
	} else {
		query = `
			select
				v.id, v.channel_id, v.title, v.description,
				v.media_url, v.thumbnail_url, v.location, c.name,
				0, 0, v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.channel_id = ? and v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
			order by v.id desc
		`
		args = []any{channelID}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer rows.Close()

	var videos []VideoView
	for rows.Next() {
		var item VideoView
		err := rows.Scan(
			&item.ID,
			&item.ChannelID,
			&item.Title,
			&item.Description,
			&item.MediaURL,
			&item.ThumbnailURL,
			&item.Location,
			&item.ChannelName,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
			&item.IsAdminLocked,
		)
		if err != nil {
			s.serverError(w, err)
			return
		}
		videos = append(videos, item)
	}

	data := ChannelPageData{
		Title:        ch.Name + " - VideoShareApp",
		Channel:      ch,
		IsSubscribed: isSubscribed,
		Videos:       videos,
		CurrentUser:  user,
	}

	if err := s.renderPage(w, "channel.html", "channel", data); err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) editChannelHandler(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "edit-channel" {
		http.NotFound(w, r)
		return
	}

	channelID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		var ch Channel
		err := s.db.QueryRow(`
			select id, name, location, description, avatar_url, user_id, is_admin_locked
			from channels
			where id = ?
		`, channelID).Scan(
			&ch.ID,
			&ch.Name,
			&ch.Location,
			&ch.Description,
			&ch.AvatarURL,
			&ch.OwnerUserID,
			&ch.IsAdminLocked,
		)

		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}

		isOwner := user.ID == ch.OwnerUserID
		if !isOwner && !user.IsAdmin {
			http.NotFound(w, r)
			return
		}

		form := ChannelEditForm{
			Name:          ch.Name,
			Location:      ch.Location,
			Description:   ch.Description,
			IsAdminLocked: ch.IsAdminLocked,
		}

		data := EditChannelPageData{
			Title:       "Edit Channel",
			Heading:     "Edit Channel Settings",
			Channel:     ch,
			Form:        form,
			CurrentUser: user,
			IsOwner:     isOwner,
		}

		if err := s.renderPage(w, "edit_channel.html", "edit_channel", data); err != nil {
			s.serverError(w, err)
		}
		return
	}

	if r.Method == http.MethodPost {
		var channelOwnerID int64
		if err := s.db.QueryRow(
			`select user_id from channels where id = ?`,
			channelID,
		).Scan(&channelOwnerID); err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			s.serverError(w, err)
			return
		}

		isOwner := user.ID == channelOwnerID
		if !isOwner && !user.IsAdmin {
			http.NotFound(w, r)
			return
		}

		if isOwner {
			name := strings.TrimSpace(r.FormValue("name"))
			location := strings.TrimSpace(r.FormValue("location"))
			description := r.FormValue("description")
			avatarBase64 := r.FormValue("avatar")

			if name == "" {
				name = "Unnamed Channel"
			}
			if location == "" {
				location = "Unknown"
			}

			var avatarURL string
			if avatarBase64 != "" {
				b64data := avatarBase64
				if idx := strings.Index(avatarBase64, ","); idx != -1 {
					b64data = avatarBase64[idx+1:]
				}

				imgBytes, err := base64.StdEncoding.DecodeString(b64data)
				if err == nil {
					avatarDir := filepath.Join("static", "uploads", "channels", fmt.Sprintf("%d", channelID))
					if mkErr := os.MkdirAll(avatarDir, 0755); mkErr == nil {
						avatarPath := filepath.Join(avatarDir, "avatar.jpg")
						if err = os.WriteFile(avatarPath, imgBytes, 0644); err == nil {
							avatarURL = "/" + filepath.ToSlash(avatarPath)
						}
					}
				}
			}

			if avatarURL != "" {
				_, err = s.db.Exec(
					`update channels set
						name = ?,
						location = ?,
						description = ?,
						avatar_url = ?
					where id = ?`,
					name,
					location,
					description,
					avatarURL,
					channelID,
				)
			} else {
				_, err = s.db.Exec(
					`update channels set
						name = ?,
						location = ?,
						description = ?
					where id = ?`,
					name,
					location,
					description,
					channelID,
				)
			}
			if err != nil {
				s.serverError(w, err)
				return
			}
		}

		if user.IsAdmin {
			isAdminLocked := r.FormValue("is_admin_locked") == "1"
			isAdminLockedInt := 0
			if isAdminLocked {
				isAdminLockedInt = 1
			}
			_, err = s.db.Exec(
				`update channels set is_admin_locked = ? where id = ?`,
				isAdminLockedInt,
				channelID,
			)
			if err != nil {
				s.serverError(w, err)
				return
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/channel/%d", channelID), http.StatusSeeOther)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) channelActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := s.requireUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "channels" || parts[2] != "subscribe" {
		http.NotFound(w, r)
		return
	}

	channelID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_, err = s.db.Exec(
		`insert or ignore into subscriptions(user_id, channel_id) values(?, ?)`,
		user.ID,
		channelID,
	)
	if err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
