package app

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

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
	if !s.allowLoginAttempt(w, r) {
		return
	}
	cleanup, err := parseRequestForm(w, r, defaultFormBodyLimit, multipartMemoryLimit)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		rejectFormParseError(w, err)
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
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 8 ||
		len(password) > 72 || strings.TrimSpace(password) == "" {
		http.Error(
			w,
			"Password must be at least 8 characters, no more than 72 bytes, and not all whitespace.",
			http.StatusBadRequest,
		)
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
	if !s.allowLoginAttempt(w, r) {
		return
	}
	cleanup, err := parseRequestForm(w, r, defaultFormBodyLimit, multipartMemoryLimit)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		rejectFormParseError(w, err)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if !utf8.ValidString(password) || len(password) > 72 {
		s.renderAuth(w, AuthPageData{
			Title:       "Login",
			Heading:     "Welcome back",
			Action:      "/login",
			SubmitLabel: "Login",
			Error:       "Invalid credentials.",
		})
		return
	}
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
