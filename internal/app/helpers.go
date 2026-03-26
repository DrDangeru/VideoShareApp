package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
)

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
