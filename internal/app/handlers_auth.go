package app

import (
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

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
