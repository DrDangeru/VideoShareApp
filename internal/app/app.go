package app

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"

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
