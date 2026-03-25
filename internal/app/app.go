package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func New() (*Server, error) {
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Println("os.MkdirAll error:", err)
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join("data", "app.db"))
	if err != nil {
		log.Println("sql.Open error:", err)
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		log.Println("PRAGMA foreign_keys error:", err)
		return nil, err
	}

	if err := initSchema(db); err != nil {
		log.Println("initSchema error:", err)
		return nil, err
	}

	if err := seedData(db); err != nil {
		log.Println("seedData error:", err)
		return nil, err
	}

	s := &Server{
		db:  db,
		mux: http.NewServeMux(),
	}

	s.routes()
	return s, nil
}

func (s *Server) Run() error {
	return http.ListenAndServe(":8080", s.mux)
}

func (s *Server) routes() {
	s.mux.Handle("/static/", s.staticHandler())
	s.mux.HandleFunc("/media/", s.mediaHandler)
	s.mux.HandleFunc("/", s.homeHandler)
	s.mux.HandleFunc("/register", s.registerHandler)
	s.mux.HandleFunc("/login", s.loginHandler)
	s.mux.HandleFunc("/logout", s.logoutHandler)
	s.mux.HandleFunc("/dashboard", s.dashboardHandler)
	s.mux.HandleFunc("/upload", s.uploadHandler)
	s.mux.HandleFunc("/edit-video/", s.editVideoHandler)
	s.mux.HandleFunc("/watch/", s.watchHandler)
	s.mux.HandleFunc("/videos/", s.videoActionHandler)
	s.mux.HandleFunc("/channels/", s.channelActionHandler)
	s.mux.HandleFunc("/channel/", s.channelPageHandler)
	s.mux.HandleFunc("/edit-channel/", s.editChannelHandler)
}

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

func (s *Server) homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	user, _ := s.currentUser(r)
	videos, err := s.loadFeed(user)
	if err != nil {
		s.serverError(w, err)
		return
	}

	feedChannels, err := s.loadChannelSidebar(user, false)
	if err != nil {
		s.serverError(w, err)
		return
	}

	subscribedChannels, err := s.loadChannelSidebar(user, true)
	if err != nil {
		s.serverError(w, err)
		return
	}

	data := HomePageData{
		Title:              "VideoShareApp",
		Heading:            "Your video feed",
		Subheading:         "Simple Go-powered streaming with favorites, channels, and" +
			" location-aware discovery.",
		CurrentUser:        user,
		Videos:             videos,
		FeedChannels:       feedChannels,
		SubscribedChannels: subscribedChannels,
	}

	if err := s.renderPage(w, "home.html", "home", data); err != nil {
		s.serverError(w, err)
	}
}

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

func (s *Server) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderAuth(w, AuthPageData{Title: "Register", Heading: "Create account", Action: "/register",
			SubmitLabel: "Register", ShowLocation: true})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	location := strings.TrimSpace(r.FormValue("location"))
	if email == "" || password == "" || location == "" {
		s.renderAuth(w, AuthPageData{Title: "Register", Heading: "Create account", Action: "/register",
			SubmitLabel: "Register", ShowLocation: true, Error: "All fields are required."})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.serverError(w, err)
		return
	}

	res, err := s.db.Exec(`insert into users(email, password_hash, location) values(?, ?, ?)`, email,
		string(hash), location)
	if err != nil {
		s.renderAuth(w, AuthPageData{Title: "Register", Heading: "Create account", Action: "/register",
			SubmitLabel: "Register", ShowLocation: true, Error: "Email already exists or is invalid."})
		return
	}

	userID, err := res.LastInsertId()
	if err != nil {
		s.serverError(w, err)
		return
	}

	if err := s.createSession(w, userID); err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderAuth(w, AuthPageData{Title: "Login", Heading: "Welcome back", Action: "/login",
			SubmitLabel: "Login"})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	user, hash, err := s.findUserByEmail(email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.renderAuth(w, AuthPageData{
			Title:       "Login",
			Heading:     "Welcome back",
			Action:      "/login",
			SubmitLabel: "Login",
			Error:       "Invalid credentials.",
		})
		return
	}

	if err := s.createSession(w, user.ID); err != nil {
		s.serverError(w, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		_, _ = s.db.Exec(`delete from sessions where token = ?`, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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

func (s *Server) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	videos, err := s.loadUserVideos(user)
	if err != nil {
		s.serverError(w, err)
		return
	}

	channels, err := s.loadManageableChannels(user)
	if err != nil {
		s.serverError(w, err)
		return
	}

	data := DashboardPageData{
		Title:       "Dashboard - VideoShareApp",
		Heading:     "Your Uploaded Videos",
		CurrentUser: user,
		Videos:      videos,
		Channels:    channels,
	}

	if err := s.renderPage(w, "dashboard.html", "dashboard", data); err != nil {
		s.serverError(w, err)
	}
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

	err = r.ParseMultipartForm(50 << 20) // 50MB max memory
	if err != nil {
		log.Println("parse form error:", err)
		http.Error(w, "Failed to parse upload", http.StatusBadRequest)
		return
	}

	// --- Resolve channel ---
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

	// --- Video file ---
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

	// --- Form fields ---
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

	// --- Insert video record ---
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

	// --- Save video file ---
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

	// --- Save thumbnail ---
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

	// --- Update record with file paths ---
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

		// Provide Form data with current video values as defaults if not already a draft
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

			// Save the base64 thumbnail if provided
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
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // Split the URL path by "/"
	if len(parts) != 2 || parts[0] != "watch" {                // Check if the path is valid
		http.NotFound(w, r)
		return
	}

	videoID, err := strconv.ParseInt(parts[1], 10, 64) // Parse the video ID from the URL
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

func (s *Server) loadSimilarVideos(
	videoID, channelID int64,
	location string,
	user *User,
) ([]SimilarVideo, error) {
	var query string
	var args []any

	if user != nil && user.IsAdmin {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ?
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{user.ID, user.ID, videoID, channelID, location, channelID}
	} else if user != nil {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ? and v.is_public = 1 and v.is_admin_locked = 0
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{user.ID, user.ID, videoID, channelID, location, channelID}
	} else {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name, 0, 0, v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ? and v.is_public = 1 and v.is_admin_locked = 0
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{videoID, channelID, location, channelID}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []SimilarVideo
	for rows.Next() {
		var item SimilarVideo
		err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Description,
			&item.MediaURL,
			&item.ThumbnailURL,
			&item.Location,
			&item.ChannelID,
			&item.ChannelName,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadSimilarChannels(
	excludeChannelID int64,
	location string,
	user *User,
) ([]SimilarChannel, error) {
	var query string
	var args []any

	if user != nil && user.IsAdmin {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url,
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{user.ID, excludeChannelID, location}
	} else if user != nil {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url,
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{user.ID, excludeChannelID, location}
	} else {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url, 0
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{excludeChannelID, location}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []SimilarChannel
	for rows.Next() {
		var item SimilarChannel
		err := rows.Scan(
			&item.ChannelID,
			&item.ChannelName,
			&item.Location,
			&item.AvatarURL,
			&item.LatestVideoID,
			&item.LatestVideoTitle,
			&item.LatestVideoURL,
			&item.IsSubscribed,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadChannelSidebar(user *User, subscribedOnly bool) ([]ChannelSidebarItem, error) {
	args := []any{}
	query := `
		select
			c.id,
			c.name,
			c.location,
			c.avatar_url,
			v.id,
			v.title,
			v.media_url,
			0
		from channels c
		join videos v on v.id = (
			select v2.id
			from videos v2
			where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
			order by v2.id desc
			limit 1
		)
	`

	if user != nil {
		if user.IsAdmin {
			query = `
				select
					c.id,
					c.name,
					c.location,
					c.avatar_url,
					v.id,
					v.title,
					v.media_url,
					exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
				from channels c
				join videos v on v.id = (
					select v2.id
					from videos v2
					where v2.channel_id = c.id
					order by v2.id desc
					limit 1
				)
			`
			args = append(args, user.ID)
		} else {
			query = `
				select
					c.id,
					c.name,
					c.location,
					c.avatar_url,
					v.id,
					v.title,
					v.media_url,
					exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
				from channels c
				join videos v on v.id = (
					select v2.id
					from videos v2
					where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
					order by v2.id desc
					limit 1
				)
			`
			args = append(args, user.ID)
		}
	}

	if subscribedOnly {
		if user == nil {
			return []ChannelSidebarItem{}, nil
		}
		query += `
			where exists(
				select 1 from subscriptions s
				where s.user_id = ? and s.channel_id = c.id
			)`
		args = append(args, user.ID)
	}

	if user != nil {
		query += `
			order by case when c.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.Location)
	} else {
		query += `
			order by v.id desc
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ChannelSidebarItem{}
	for rows.Next() {
		var item ChannelSidebarItem
		err := rows.Scan(
			&item.ChannelID,
			&item.ChannelName,
			&item.Location,
			&item.AvatarURL,
			&item.LatestVideoID,
			&item.LatestVideoTitle,
			&item.LatestVideoURL,
			&item.IsSubscribed,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Server) loadFeed(user *User) ([]VideoView, error) {
	args := []any{}
	query := `
		select
			v.id,
			v.channel_id,
			v.title,
			v.description,
			v.media_url,
			v.thumbnail_url,
			v.location,
			c.name,
			0,
			0,
			v.is_public,
			v.is_admin_locked
		from videos v
		join channels c on c.id = v.channel_id
		where v.is_public = 1 and v.is_admin_locked = 0
	`

	if user != nil && user.IsAdmin {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public,
				v.is_admin_locked
			from videos v
			join channels c on c.id = v.channel_id
			order by case when v.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.ID, user.ID, user.Location)
	} else if user != nil {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public,
				v.is_admin_locked
			from videos v
			join channels c on c.id = v.channel_id
			where v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
			order by case when v.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.ID, user.ID, user.Location)
	} else {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				0,
				0,
				v.is_public,
				v.is_admin_locked
			from videos v
			join channels c on c.id = v.channel_id
			where v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
			order by v.id desc
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := []VideoView{}
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
			return nil, err
		}
		videos = append(videos, item)
	}

	return videos, rows.Err()
}

func (s *Server) loadUserVideos(user *User) ([]VideoView, error) {
	query := `
		select
			v.id,
			v.channel_id,
			v.title,
			v.description,
			v.media_url,
			v.thumbnail_url,
			v.location,
			c.name,
			0,
			0,
			v.is_public,
			v.is_admin_locked
		from videos v
		join channels c on c.id = v.channel_id
		where c.user_id = ?
		order by v.id desc
	`
	args := []any{user.ID}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		videos = append(videos, item)
	}

	return videos, rows.Err()
}

func (s *Server) loadManageableChannels(user *User) ([]Channel, error) {
	query := `select id, name, location, user_id from channels where user_id = ? order by name`
	args := []any{user.ID}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Location, &ch.OwnerUserID); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	log.Println("server error:", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func (s *Server) renderAuth(w http.ResponseWriter, data AuthPageData) {
	if err := s.renderPage(w, "auth.html", "auth", data); err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) renderPage(
	w http.ResponseWriter,
	pageFile string,
	templateName string,
	data any,
) error {
	templates, err := template.ParseFiles(
		filepath.Join("templates", "layout.html"),
		filepath.Join("templates", pageFile),
	)
	if err != nil {
		return err
	}

	return templates.ExecuteTemplate(w, templateName, data)
}

func (s *Server) currentUser(r *http.Request) (*User, error) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRow(`
		select u.id, u.email, u.location, u.is_admin
		from sessions s
		join users u on u.id = s.user_id
		where s.token = ?
	`, cookie.Value)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.Location, &user.IsAdmin); err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *Server) requireUser(r *http.Request) (*User, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, errors.New("authentication required")
	}
	return user, nil
}

func (s *Server) createSession(w http.ResponseWriter, userID int64) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := hex.EncodeToString(b)
	if _, err := s.db.Exec(
		`insert into sessions(token, user_id) values(?, ?)`,
		token,
		userID,
	); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) findUserByEmail(email string) (*User, string, error) {
	row := s.db.QueryRow(
		`select id, email, location, is_admin, password_hash from users where email = ?`,
		email,
	)
	var user User
	var hash string
	if err := row.Scan(&user.ID, &user.Email, &user.Location, &user.IsAdmin, &hash); err != nil {
		return nil, "", err
	}
	return &user, hash, nil
}
