# VideoShareApp Architecture

This document outlines the current project structure, core components, and data models of the VideoShareApp application.

## Directory Structure

```text
VideoShareApp/
├── data/
│   └── app.db               # SQLite database file (auto-generated)
├── internal/
│   └── app/
│       ├── app.go           # Core HTTP server, routing, and handler logic
│       ├── db.go            # Database connection, migrations, and seeding
│       └── types.go         # Consolidated data structs and models
├── static/
│   ├── style.css            # Global CSS stylesheet (with light/dark theme vars)
│   └── uploads/
│       └── users/           # User-uploaded content
│           └── {user_id}/   
│               └── {video_id}/
│                   ├── original.mp4
│                   └── thumbnail.jpg
├── templates/               # HTML templates rendered by Go
│   ├── auth.html            # Login/Register views
│   ├── dashboard.html       # User dashboard for uploading videos
│   ├── edit_video.html      # Two-step upload: metadata & frame capture
│   ├── home.html            # Main feed (thumbnail cards linking to /watch/)
│   ├── layout.html          # Global layout wrapper
│   └── watch.html           # Video watch page with similar content sidebar
├── go.mod                   # Go module definition
├── go.sum                   # Go dependencies
└── main.go                  # Application entry point
```

## Data Models & Structs (`internal/app/types.go`)

The core domain structs map directly to database queries and are composed into Page Models for rendering the HTML views.

### Core Application State

* **`Server`**
  * Holds the database connection (`*sql.DB`) and the HTTP router (`*http.ServeMux`).
  * All HTTP handlers and database methods are attached to this struct (e.g., `s.homeHandler`, `s.loadFeed`).

### Domain Models

* **`User`**
  * Represents a registered user in the system.
  * Connected to sessions via the `sessions` table.
  * Fields: `ID`, `Email`, `Location`.

* **`Video`**
  * Represents a video in the system. Must belong to a channel.
  * Fields: `ID`, `ChannelID`, `Title`, `Description`, `MediaURL`, `ThumbnailURL`, `Location`, `ChannelName`,
  * `IsFavorite`, `IsSubscribed`, `IsPublic`.

* **`VideoView`**
  * A rich representation of a video, often joining data from the `channels` table and checking `favorites`/`subscriptions` tables. 
  * Must include the text entered by uploader to represent the video content and message.
  * Fields: `ID`, `ChannelID`, `Title`, `Description`, `MediaURL`, `ThumbnailURL`, `Location`, `ChannelName`, `IsFavorite`, `IsSubscribed`, `IsPublic`.

* **`VideoEditForm`**
  * Represents the form state when editing video metadata. Must represent the contents of the video.
  * Fields: `Title`, `Description`, `Location`, `IsPublic`.

* **`Channel`**
  * Represents a video channel, which groups videos and can be subscribed to.
  * Fields: `ID`, `Name`, `Location`, `OwnerUserID` (the owner of the channel).

* **`SimilarVideo`**
  * A video from the same channel or location, shown in the watch page sidebar.
  * Fields: `ID`, `Title`, `Description`, `MediaURL`, `ThumbnailURL`, `Location`, `ChannelID`, `ChannelName`, `IsFavorite`, `IsSubscribed`, `IsPublic`.

* **`SimilarChannel`**
  * A channel from the same location as the currently watched video.
  * Fields: `ChannelID`, `ChannelName`, `Location`, `LatestVideoID`, `LatestVideoTitle`, `LatestVideoURL`, `IsSubscribed`.

* **`FeedVideo`**
  * A video from a channel the current user is subscribed to.
  * Fields: `ID`, `Title`, `Description`, `MediaURL`, `ThumbnailURL`, `Location`, `ChannelID`, `ChannelName`, `IsFavorite`, `IsSubscribed`, `IsPublic`.

* **`ChannelSidebarItem`**
  * Represents a channel shown in the left sidebar (either globally or subscribed).
  * Includes data about the channel's most recent video to display a mini-feed.
  * Fields: `ChannelID`, `ChannelName`, `Location`, `LatestVideoID`, `LatestVideoTitle`, `LatestVideoURL`, `IsSubscribed`.

### Page Data Models

These structs are specifically designed to be passed into the `html/template` engine. They wrap domain models with page-specific context.

* **`HomePageData`**
  * Used by `homeHandler` -> `home.html`
  * Contains the `CurrentUser`, a slice of `VideoView` for the main feed, and slices of `ChannelSidebarItem` for the sidebar.

* **`AuthPageData`**
  * Used by `loginHandler` / `registerHandler` -> `auth.html`
  * Handles form state, submit labels, and dynamic `Error` messages.

* **`DashboardPageData`**
  * Used by `dashboardHandler` -> `dashboard.html`
  * Contains the `CurrentUser` and a slice of `VideoView` showing *only* the logged-in user's uploads.

* **`EditVideoPageData`**
  * Used by `editVideoHandler` -> `edit_video.html`
  * Contains the specific `VideoView` being edited, allowing the form to pre-populate the current metadata and media URLs.

* **`VideoPageData`**
  * Used by `watchHandler` -> `watch.html`
  * Contains the main `VideoView`, `SimilarVideos` (same channel/location), and `SimilarChannels` (same location).
