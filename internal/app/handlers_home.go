package app

import "net/http"

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
		Title:   "VideoShareApp",
		Heading: "Your video feed",
		Subheading: "Simple Go-powered streaming with favorites, channels, and" +
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
