package app

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) videoActionHandler(w http.ResponseWriter, r *http.Request) {
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
	if len(parts) != 3 || parts[0] != "videos" || parts[2] != "favorite" {
		http.NotFound(w, r)
		return
	}

	videoID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_, err = s.db.Exec(
		`insert or ignore into favorites(user_id, video_id) values(?, ?)`,
		user.ID,
		videoID,
	)
	if err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := s.requireUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	err = r.ParseMultipartForm(50 << 20)
	if err != nil {
		log.Println("parse form error:", err)
		http.Error(w, "Failed to parse upload", http.StatusBadRequest)
		return
	}

	var channelID int64
	channelOwnerID := user.ID
	newChannelName := strings.TrimSpace(r.FormValue("new_channel"))
	if newChannelName != "" {
		res, err := s.db.Exec(`insert into channels(name, location, user_id) values(?, ?, ?)`,
			newChannelName, user.Location, user.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		channelID, _ = res.LastInsertId()
	} else {
		channelID, err = strconv.ParseInt(r.FormValue("channel_id"), 10, 64)
		if err != nil || channelID < 1 {
			http.Error(w, "Please select a channel or create a new one.", http.StatusBadRequest)
			return
		}
		err = s.db.QueryRow(`select user_id from channels where id = ?`, channelID).Scan(&channelOwnerID)
		if err == sql.ErrNoRows {
			http.Error(w, "Selected channel was not found.", http.StatusBadRequest)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}
		if channelOwnerID != user.ID {
			http.Error(w, "You can only upload to channels you manage.", http.StatusForbidden)
			return
		}
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "Failed to get video file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	videoExt := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{".mp4": true, ".webm": true, ".mov": true}
	if !allowedExts[videoExt] {
		http.Error(w, "Unsupported video format. Use MP4, WebM, or MOV.", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Untitled Video"
	}
	description := r.FormValue("description")
	isPublic := r.FormValue("is_public") == "1"
	thumbnailBase64 := r.FormValue("thumbnail")
	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}

	res, err := s.db.Exec(`insert into videos(channel_id, title, description,
		media_url, thumbnail_url, location, user_id, is_public)
		values(?, ?, ?, ?, ?, ?, ?, ?)`,
		channelID, title, description, "", "", user.Location, channelOwnerID, isPublicInt)
	if err != nil {
		s.serverError(w, err)
		return
	}

	videoID, err := res.LastInsertId()
	if err != nil {
		s.serverError(w, err)
		return
	}

	dirOwnerID := channelOwnerID
	if dirOwnerID < 1 {
		dirOwnerID = user.ID
	}
	videoDir := filepath.Join(
		"static",
		"uploads",
		"users",
		fmt.Sprintf("%d", dirOwnerID),
		fmt.Sprintf("%d", videoID),
	)
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		s.serverError(w, err)
		return
	}

	videoPath := filepath.Join(videoDir, "original"+videoExt)
	out, err := os.Create(videoPath)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		s.serverError(w, err)
		return
	}

	mediaURL := "/" + filepath.ToSlash(videoPath)

	thumbnailURL := "/static/img/placeholder.svg"
	if thumbnailBase64 != "" {
		b64data := thumbnailBase64
		if idx := strings.Index(thumbnailBase64, ","); idx != -1 {
			b64data = thumbnailBase64[idx+1:]
		}
		if imgBytes, err := base64.StdEncoding.DecodeString(b64data); err == nil {
			thumbPath := filepath.Join(videoDir, "original.jpg")
			if err := os.WriteFile(thumbPath, imgBytes, 0644); err == nil {
				thumbnailURL = "/" + filepath.ToSlash(thumbPath)
			}
		}
	}

	_, err = s.db.Exec(`update videos set media_url = ?, thumbnail_url = ? where id = ?`,
		mediaURL, thumbnailURL, videoID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) editVideoHandler(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "edit-video" {
		http.NotFound(w, r)
		return
	}

	videoID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		var video VideoView
		var channelOwnerID int64
		row := s.db.QueryRow(`
			select
				v.id, v.title, v.description, v.media_url,
				v.thumbnail_url, v.is_public, v.is_admin_locked, c.user_id
			from videos v
			join channels c on c.id = v.channel_id
			where v.id = ?
		`, videoID)
		err := row.Scan(
			&video.ID,
			&video.Title,
			&video.Description,
			&video.MediaURL,
			&video.ThumbnailURL,
			&video.IsPublic,
			&video.IsAdminLocked,
			&channelOwnerID,
		)

		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}

		isOwner := user.ID == channelOwnerID
		if !isOwner && !user.IsAdmin {
			http.NotFound(w, r)
			return
		}

		form := VideoEditForm{
			Title:         video.Title,
			Description:   video.Description,
			Location:      video.Location,
			IsPublic:      video.IsPublic,
			IsAdminLocked: video.IsAdminLocked,
		}

		if video.Title == "Draft Video" {
			form.Title = ""
		}

		data := EditVideoPageData{
			Title:       "Edit Video",
			Heading:     "Edit Video Settings",
			Video:       video,
			Form:        form,
			CurrentUser: user,
			IsOwner:     isOwner,
		}

		if err := s.renderPage(w, "edit_video.html", "edit_video", data); err != nil {
			s.serverError(w, err)
		}
		return
	}

	if r.Method == http.MethodPost {
		var channelOwnerID int64
		var mediaURL string
		row := s.db.QueryRow(`
			select v.media_url, c.user_id
			from videos v
			join channels c on c.id = v.channel_id
			where v.id = ?
		`, videoID)
		if err := row.Scan(&mediaURL, &channelOwnerID); err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}

		isOwner := user.ID == channelOwnerID
		if !isOwner && !user.IsAdmin {
			http.NotFound(w, r)
			return
		}

		if isOwner {
			title := r.FormValue("title")
			description := r.FormValue("description")
			isPublic := r.FormValue("is_public") == "1"
			thumbnailBase64 := r.FormValue("thumbnail")

			isPublicInt := 0
			if isPublic {
				isPublicInt = 1
			}

			var thumbnailURL string
			if thumbnailBase64 != "" {
				b64data := thumbnailBase64
				if idx := strings.Index(thumbnailBase64, ","); idx != -1 {
					b64data = thumbnailBase64[idx+1:]
				}

				imgBytes, err := base64.StdEncoding.DecodeString(b64data)
				if err == nil {
					thumbPath := filepath.Join(filepath.Dir(strings.TrimPrefix(mediaURL, "/")), "original.jpg")

					if mkErr := os.MkdirAll(filepath.Dir(thumbPath), 0755); mkErr != nil {
						log.Println("thumbnail dir error:", mkErr)
					} else if err = os.WriteFile(thumbPath, imgBytes, 0644); err == nil {
						thumbnailURL = "/" + filepath.ToSlash(thumbPath)
					}
				}
			}

			if thumbnailURL != "" {
				_, err = s.db.Exec(
					`update videos set
						title = ?, description = ?, is_public = ?, thumbnail_url = ?
					where id = ?`,
					title,
					description,
					isPublicInt,
					thumbnailURL,
					videoID,
				)
			} else {
				_, err = s.db.Exec(
					`update videos set title = ?, description = ?, is_public = ? where id = ?`,
					title,
					description,
					isPublicInt,
					videoID,
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
				`update videos set is_admin_locked = ? where id = ?`,
				isAdminLockedInt,
				videoID,
			)
			if err != nil {
				s.serverError(w, err)
				return
			}
		}

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) watchHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "watch" {
		http.NotFound(w, r)
		return
	}

	videoID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user, _ := s.currentUser(r)

	var video VideoView
	var row *sql.Row
	if user != nil && user.IsAdmin {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name, 0, 0, v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.id = ?
		`, videoID)
	} else if user != nil {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.id = ?
				and (
					(v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0)
					or c.user_id = ?
				)
		`, user.ID, user.ID, videoID, user.ID)
	} else {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name, 0, 0, v.is_public, v.is_admin_locked
			from videos v join channels c on c.id = v.channel_id
			where v.id = ? and v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
		`, videoID)
	}

	err = row.Scan(
		&video.ID,
		&video.ChannelID,
		&video.Title,
		&video.Description,
		&video.MediaURL,
		&video.ThumbnailURL,
		&video.Location,
		&video.ChannelName,
		&video.IsFavorite,
		&video.IsSubscribed,
		&video.IsPublic,
		&video.IsAdminLocked,
	)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}

	similarVideos, err := s.loadSimilarVideos(video.ID, video.ChannelID, video.Location, user)
	if err != nil {
		s.serverError(w, err)
		return
	}

	similarChannels, err := s.loadSimilarChannels(video.ChannelID, video.Location, user)
	if err != nil {
		s.serverError(w, err)
		return
	}

	data := VideoPageData{
		Title:           video.Title,
		CurrentUser:     user,
		Video:           video,
		SimilarVideos:   similarVideos,
		SimilarChannels: similarChannels,
	}

	if err := s.renderPage(w, "watch.html", "watch", data); err != nil {
		s.serverError(w, err)
	}
}
