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
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	description := r.FormValue("description")
	isPublic := r.FormValue("is_public") == "on"
	allowComments := r.FormValue("allow_comments") == "on"
	madeForKids := r.FormValue("made_for_kids") == "on"

	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}

	allowCommentsInt := 0
	if allowComments {
		allowCommentsInt = 1
	}

	madeForKidsInt := 0
	if madeForKids {
		madeForKidsInt = 1
	}

	res, err := s.db.Exec(`insert into videos(channel_id, title, description,
		media_url, thumbnail_url, location, user_id, is_public, allow_comments, made_for_kids)
		values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channelID, title, description, "", "", user.Location, channelOwnerID, isPublicInt, allowCommentsInt, madeForKidsInt)
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
	thumbnailBase64 := r.FormValue("thumbnail_base64")
	if thumbnailBase64 != "" {
		parts := strings.Split(thumbnailBase64, ",")
		if len(parts) == 2 {
			imgBytes, err := base64.StdEncoding.DecodeString(parts[1])
			if err == nil {
				thumbPath := filepath.Join(videoDir, "original.jpg")
				if err := os.WriteFile(thumbPath, imgBytes, 0644); err == nil {
					thumbnailURL = "/" + filepath.ToSlash(thumbPath)
				}
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
				v.thumbnail_url, v.is_public, v.is_admin_locked,
				v.allow_comments, v.made_for_kids, c.user_id
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
			&video.AllowComments,
			&video.MadeForKids,
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
			AllowComments: video.AllowComments,
			MadeForKids:   video.MadeForKids,
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
			var dbLocation string
			var dbIsAdminLocked bool
			s.db.QueryRow(`select location, is_admin_locked from videos where id = ?`, videoID).Scan(&dbLocation, &dbIsAdminLocked)

			title := r.FormValue("title")
			description := r.FormValue("description")
			location := dbLocation
			if r.FormValue("location") != "" {
				location = r.FormValue("location")
			}
			isPublic := r.FormValue("is_public") == "1"
			allowComments := r.FormValue("allow_comments") == "1"
			madeForKids := r.FormValue("made_for_kids") == "1"
			isAdminLocked := dbIsAdminLocked
			if user.IsAdmin {
				isAdminLocked = r.FormValue("is_admin_locked") == "1"
			}

			isPublicInt := 0
			if isPublic {
				isPublicInt = 1
			}

			allowCommentsInt := 0
			if allowComments {
				allowCommentsInt = 1
			}

			madeForKidsInt := 0
			if madeForKids {
				madeForKidsInt = 1
			}

			var thumbnailURL string
			thumbnailBase64 := r.FormValue("thumbnail")
			if thumbnailBase64 != "" {
				parts := strings.Split(thumbnailBase64, ",")
				if len(parts) == 2 {
					imgBytes, err := base64.StdEncoding.DecodeString(parts[1])
					if err == nil {
						thumbPath := filepath.Join(filepath.Dir(strings.TrimPrefix(mediaURL, "/")), "original.jpg")

						if mkErr := os.MkdirAll(filepath.Dir(thumbPath), 0755); mkErr != nil {
							log.Println("thumbnail dir error:", mkErr)
						} else if err = os.WriteFile(thumbPath, imgBytes, 0644); err == nil {
							thumbnailURL = "/" + filepath.ToSlash(thumbPath)
						}
					}
				}
			}

			if thumbnailURL != "" {
				if user.IsAdmin {
					isAdminLockedInt := 0
					if isAdminLocked {
						isAdminLockedInt = 1
					}
					_, err = s.db.Exec(`update videos set title = ?, description = ?, location = ?, 
					is_public = ?, is_admin_locked = ?, allow_comments = ?, made_for_kids = ?, 
					thumbnail_url = ? where id = ?`,
						title, description, location, isPublicInt, isAdminLockedInt, allowCommentsInt,
						madeForKidsInt, thumbnailURL, videoID)
				} else {
					_, err = s.db.Exec(`update videos set title = ?, description = ?, location = ?, is_public = ?, allow_comments = ?, made_for_kids = ?, thumbnail_url = ? where id = ?`,
						title, description, location, isPublicInt, allowCommentsInt, madeForKidsInt, thumbnailURL, videoID)
				}
			} else {
				if user.IsAdmin {
					isAdminLockedInt := 0
					if isAdminLocked {
						isAdminLockedInt = 1
					}
					_, err = s.db.Exec(`update videos set title = ?, description = ?, location = ?, is_public = ?, is_admin_locked = ?, allow_comments = ?, made_for_kids = ? where id = ?`,
						title, description, location, isPublicInt, isAdminLockedInt, allowCommentsInt, madeForKidsInt, videoID)
				} else {
					_, err = s.db.Exec(`update videos set title = ?, description = ?, location = ?, is_public = ?, allow_comments = ?, made_for_kids = ? where id = ?`,
						title, description, location, isPublicInt, allowCommentsInt, madeForKidsInt, videoID)
				}
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

	var row *sql.Row
	if user != nil && user.IsAdmin {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name, 0, 0, v.is_public, v.is_admin_locked, v.allow_comments,
				v.made_for_kids
			from videos v join channels c on c.id = v.channel_id
			where v.id = ?
		`, videoID)
	} else if user != nil {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public, v.is_admin_locked, v.allow_comments, v.made_for_kids
			from videos v join channels c on c.id = v.channel_id
			where v.id = ? and (
				v.is_public = 1 or v.user_id = ?
			) and (v.is_admin_locked = 0 and c.is_admin_locked = 0)
		`, user.ID, user.ID, videoID, user.ID)
	} else {
		row = s.db.QueryRow(`
			select v.id, v.channel_id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.name, 0, 0, v.is_public, v.is_admin_locked, v.allow_comments,
				v.made_for_kids
			from videos v join channels c on c.id = v.channel_id
			where v.id = ? and v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
		`, videoID)
	}

	var video VideoView
	err = row.Scan(
		&video.ID, &video.ChannelID, &video.Title, &video.Description, &video.MediaURL, &video.ThumbnailURL,
		&video.Location, &video.ChannelName, &video.IsFavorite, &video.IsSubscribed, &video.IsPublic, &video.IsAdminLocked, &video.AllowComments, &video.MadeForKids,
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

	var comments []Comment
	if video.AllowComments {
		comments, err = s.loadComments(video.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
	}

	data := VideoPageData{
		Title:           video.Title,
		CurrentUser:     user,
		Video:           video,
		SimilarVideos:   similarVideos,
		SimilarChannels: similarChannels,
		Comments:        comments,
	}

	if err := s.renderPage(w, "watch.html", "watch", data); err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) addCommentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := s.requireUser(r)
	if err != nil {
		s.renderAuth(w, AuthPageData{
			Title:       "Login to Comment",
			Heading:     "Login",
			Action:      "/login",
			SubmitLabel: "Login",
		})
		return
	}

	videoIDStr := r.FormValue("video_id")
	videoID, err := strconv.ParseInt(videoIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid video ID", http.StatusBadRequest)
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "Comment cannot be empty", http.StatusBadRequest)
		return
	}

	var allowComments int
	err = s.db.QueryRow(`select allow_comments from videos where id = ?`, videoID).Scan(&allowComments)
	if err == sql.ErrNoRows {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}

	if allowComments == 0 {
		http.Error(w, "Comments are disabled for this video", http.StatusForbidden)
		return
	}

	_, err = s.db.Exec(`insert into comments(video_id, user_id, content) values(?, ?, ?)`, videoID, user.ID, content)
	if err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/watch/%d", videoID), http.StatusSeeOther)
}

func (s *Server) deleteCommentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := s.requireUser(r)
	if err != nil {
		s.renderAuth(w, AuthPageData{
			Title:       "Login to Comment",
			Heading:     "Login",
			Action:      "/login",
			SubmitLabel: "Login",
		})
		return
	}

	commentIDStr := strings.TrimPrefix(r.URL.Path, "/delete-comment/")
	commentID, err := strconv.ParseInt(commentIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid comment ID", http.StatusBadRequest)
		return
	}

	var commentOwnerID int64
	var videoID int64
	err = s.db.QueryRow(`select user_id, video_id from comments where id = ?`, commentID).Scan(&commentOwnerID, &videoID)
	if err == sql.ErrNoRows {
		http.Error(w, "Comment not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}

	var channelOwnerID int64
	err = s.db.QueryRow(`
		select c.owner_user_id 
		from channels c 
		join videos v on v.channel_id = c.id 
		where v.id = ?`, videoID).Scan(&channelOwnerID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	if user.ID != commentOwnerID && user.ID != channelOwnerID && !user.IsAdmin {
		http.Error(w, "You don't have permission to delete this comment", http.StatusForbidden)
		return
	}

	_, err = s.db.Exec(`delete from comments where id = ?`, commentID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/watch/%d", videoID), http.StatusSeeOther)
}
