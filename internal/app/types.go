package app

import (
	"database/sql"
	"net/http"
)

// Core Application State
type Server struct {
	db  *sql.DB
	mux *http.ServeMux
}

// Domain Models
type User struct {
	ID       int64
	Email    string
	Location string
	IsAdmin  bool
}

type VideoView struct {
	ID            int64
	ChannelID     int64
	Title         string
	Description   string
	MediaURL      string
	ThumbnailURL  string
	Location      string
	ChannelName   string
	OwnerUserID   int64
	IsFavorite    bool
	IsSubscribed  bool
	IsPublic      bool
	IsAdminLocked bool
	AllowComments bool
	MadeForKids   bool
}

type VideoEditForm struct {
	Title         string
	Description   string
	Location      string
	IsPublic      bool
	IsAdminLocked bool
	AllowComments bool
	MadeForKids   bool
}

type Channel struct {
	ID            string
	Name          string
	Location      string
	Description   string
	AvatarURL     string
	OwnerUserID   int64
	IsAdminLocked bool
}

type ChannelPageData struct {
	Title        string
	Channel      Channel
	IsSubscribed bool
	Videos       []VideoView
	CurrentUser  *User
}

type Video struct {
	ID           int64
	ChannelID    int64
	Title        string
	Description  string
	MediaURL     string
	ThumbnailURL string
	Location     string
	IsPublic     bool
}

type SimilarVideo struct {
	ID           int64
	Title        string
	Description  string
	MediaURL     string
	ThumbnailURL string
	Location     string
	ChannelID    int64
	ChannelName  string
	IsFavorite   bool
	IsSubscribed bool
	IsPublic     bool
	MadeForKids  bool
}

type SimilarChannel struct {
	ChannelID        int64
	ChannelName      string
	Location         string
	AvatarURL        string
	LatestVideoID    int64
	LatestVideoTitle string
	LatestVideoURL   string
	IsSubscribed     bool
}

type FeedVideo struct {
	ID           int64
	Title        string
	Description  string
	MediaURL     string
	ThumbnailURL string
	Location     string
	ChannelID    int64
	ChannelName  string
	IsFavorite   bool
	IsSubscribed bool
	IsPublic     bool
}

type ChannelSidebarItem struct {
	ChannelID        int64
	ChannelName      string
	Location         string
	AvatarURL        string
	LatestVideoID    int64
	LatestVideoTitle string
	LatestVideoURL   string
	IsSubscribed     bool
	Notify           bool
}

// Page Data Models
type HomePageData struct {
	Title              string
	Heading            string
	Subheading         string
	CurrentUser        *User
	Videos             []VideoView
	FeedChannels       []ChannelSidebarItem
	SubscribedChannels []ChannelSidebarItem
}

type AuthPageData struct {
	Title        string
	Heading      string
	Action       string
	SubmitLabel  string
	Error        string
	ShowLocation bool
	CurrentUser  *User
}

type DashboardPageData struct {
	Title       string
	Heading     string
	CurrentUser *User
	Videos      []VideoView
	Channels    []Channel
}

type EditVideoPageData struct {
	Title       string
	Heading     string
	Video       VideoView
	Form        VideoEditForm
	CurrentUser *User
	IsOwner     bool
}

type ChannelEditForm struct {
	Name          string
	Location      string
	Description   string
	IsAdminLocked bool
}

type EditChannelPageData struct {
	Title       string
	Heading     string
	Channel     Channel
	Form        ChannelEditForm
	CurrentUser *User
	IsOwner     bool
}

type Comment struct {
	ID        int64
	VideoID   int64
	UserID    int64
	Content   string
	CreatedAt string
	UserEmail string
}

type VideoPageData struct {
	Title           string
	CurrentUser     *User
	Video           VideoView
	Comments        []Comment
	SimilarVideos   []SimilarVideo
	SimilarChannels []SimilarChannel
}
