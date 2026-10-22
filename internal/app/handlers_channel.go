package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
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
	if ch.IsAdminLocked && (user == nil || !user.IsAdmin) {
		http.NotFound(w, r)
		return
	}
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
				v.media_url, v.thumbnail_url, v.location, c.name, c.user_id,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				?,
				v.is_public, v.is_admin_locked, v.allow_comments, v.made_for_kids
			from videos v join channels c on c.id = v.channel_id
			where v.channel_id = ?
			order by v.id desc
		`
		args = []any{user.ID, isSubscribed, channelID}
	} else if user != nil {
		query = `
			select
				v.id, v.channel_id, v.title, v.description,
				v.media_url, v.thumbnail_url, v.location, c.name, c.user_id,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				?,
				v.is_public, v.is_admin_locked, v.allow_comments, v.made_for_kids
			from videos v join channels c on c.id = v.channel_id
			where v.channel_id = ? and (v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0)
			order by v.id desc
		`
		args = []any{user.ID, isSubscribed, channelID}
	} else {
		query = `
			select
				v.id, v.channel_id, v.title, v.description,
				v.media_url, v.thumbnail_url, v.location, c.name, c.user_id,
				0, 0, v.is_public, v.is_admin_locked, 1, 0
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
			&item.OwnerUserID,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
			&item.IsAdminLocked,
			&item.AllowComments,
			&item.MadeForKids,
		)
		if err != nil {
			s.serverError(w, err)
			return
		}
		videos = append(videos, item)
	}
	if err := rows.Err(); err != nil {
		s.serverError(w, err)
		return
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
		cleanup, formErr := parseRequestForm(w, r, editFormBodyLimit, multipartMemoryLimit)
		if cleanup != nil {
			defer cleanup()
		}
		if formErr != nil {
			rejectFormParseError(w, formErr)
			return
		}

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
			avatarData := r.FormValue("avatar")
			removeAvatar := r.FormValue("remove_avatar") == "1"

			if name == "" {
				name = "Unnamed Channel"
			}
			if location == "" {
				location = "Unknown"
			}

			if removeAvatar && avatarData != "" {
				http.Error(w, "Choose an avatar or remove the current one, not both.", http.StatusBadRequest)
				return
			}
			var avatarBytes []byte
			if !removeAvatar {
				avatarBytes, err = decodeThumbnail(avatarData)
				if err != nil {
					http.Error(w, "Invalid avatar image.", http.StatusBadRequest)
					return
				}
			}

			var avatarURL string
			if avatarBytes != nil {
				avatarDir := filepath.Join("static", "uploads", "channels", strconv.FormatInt(channelID, 10))
				if err := os.MkdirAll(avatarDir, 0o755); err != nil {
					s.serverError(w, err)
					return
				}
				avatarPath := filepath.Join(avatarDir, "avatar.jpg")
				if err := os.WriteFile(avatarPath, avatarBytes, 0o644); err != nil {
					s.serverError(w, err)
					return
				}
				avatarURL = "/" + filepath.ToSlash(avatarPath)
			}

			if removeAvatar {
				_, err = s.db.Exec(
					`update channels set name = ?, location = ?, description = ?, avatar_url = '' where id = ?`,
					name,
					location,
					description,
					channelID,
				)
			} else if avatarURL != "" {
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
	cleanup, formErr := parseRequestForm(w, r, defaultFormBodyLimit, multipartMemoryLimit)
	if cleanup != nil {
		defer cleanup()
	}
	if formErr != nil {
		rejectFormParseError(w, formErr)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "channels" {
		http.NotFound(w, r)
		return
	}

	action := parts[2]
	if action != "subscribe" && action != "notify" {
		http.NotFound(w, r)
		return
	}

	channelID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var channelName string
	var channelAdminLocked int
	if err := s.db.QueryRow(
		`select name, is_admin_locked from channels where id = ?`,
		channelID,
	).Scan(&channelName, &channelAdminLocked); err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, err)
		return
	}
	if channelAdminLocked != 0 && !user.IsAdmin {
		http.NotFound(w, r)
		return
	}

	var redirectURL string
	if action == "subscribe" {
		var exists int
		if err := s.db.QueryRow(
			`select count(*) from subscriptions where user_id = ? and channel_id = ?`,
			user.ID,
			channelID,
		).Scan(&exists); err != nil {
			s.serverError(w, err)
			return
		}

		if exists > 0 {
			_, err = s.db.Exec(`delete from subscriptions where user_id = ? and channel_id = ?`, user.ID, channelID)
			redirectURL = "/?unsubscribed=" + url.QueryEscape(channelName)
		} else {
			_, err = s.db.Exec(`insert into subscriptions(user_id, channel_id) values(?, ?)`, user.ID, channelID)
			redirectURL = "/?subscribed=" + url.QueryEscape(channelName)
		}
	} else {
		var currentNotify int
		err := s.db.QueryRow(
			`select notify from subscriptions where user_id = ? and channel_id = ?`,
			user.ID,
			channelID,
		).Scan(&currentNotify)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, err)
			return
		}

		result, err := s.db.Exec(
			`update subscriptions set notify = case when notify = 1 then 0 else 1 end where user_id = ? and channel_id = ?`,
			user.ID,
			channelID,
		)
		if err != nil {
			s.serverError(w, err)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil {
			s.serverError(w, err)
			return
		}
		if affected == 0 {
			http.NotFound(w, r)
			return
		}

		if currentNotify == 1 {
			redirectURL = "/?notify_off=" + url.QueryEscape(channelName)
		} else {
			redirectURL = "/?notify_on=" + url.QueryEscape(channelName)
		}
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
