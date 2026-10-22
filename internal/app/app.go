package app

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const applicationDatabaseDSN = "file:data/app.db?_pragma=foreign_keys(1)"

func New() (*Server, error) {
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Println("os.MkdirAll error:", err)
		return nil, err
	}

	db, err := sql.Open("sqlite", applicationDatabaseDSN)
	if err != nil {
		log.Println("sql.Open error:", err)
		return nil, err
	}

	if err := initSchema(db); err != nil {
		_ = db.Close()
		log.Println("initSchema error:", err)
		return nil, err
	}

	if err := seedData(db); err != nil {
		_ = db.Close()
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
	server := s.httpServer()
	log.Printf("server started on http://%s", server.Addr)
	return server.ListenAndServe()
}

func (s *Server) httpServer() *http.Server {
	address := strings.TrimSpace(os.Getenv("VIDEO_SHARE_ADDR"))
	if address == "" {
		address = "127.0.0.1:8080"
	}
	return &http.Server{
		Addr:              address,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
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
	s.mux.HandleFunc("/add-comment", s.addCommentHandler)
	s.mux.HandleFunc("/delete-comment/", s.deleteCommentHandler)
}
