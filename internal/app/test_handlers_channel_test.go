package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChannelPage(t *testing.T) {
	s := newTestServerWithTemplates(t, "channel.html")
	db := s.db

	ownerID := insertUser(t, db, "owner@chan.test", "pw", "Sofia", false)
	insertChannel(t, db, "1", "Unique Channel Name", "Sofia", ownerID, false)
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "1", title: "Unique Channel Video Title", description: "d",
		mediaURL: "/media/c1.mp4", thumbURL: "/media/c1.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})

	t.Run("renders channel and videos", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodGet, "/channel/1", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Unique Channel Name") {
			t.Error("body missing channel name")
		}
		if !strings.Contains(body, "Unique Channel Video Title") {
			t.Error("body missing video title")
		}
	})

	t.Run("empty channel", func(t *testing.T) {
		insertChannel(t, db, "2", "Empty Channel", "Sofia", ownerID, false)

		rec := doRequest(t, s, http.MethodGet, "/channel/2", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "This channel hasn't uploaded any videos yet.") {
			t.Error("body missing no-videos message")
		}
	})
}

func TestLockedChannelPagePrivacy(t *testing.T) {
	s := newTestServerWithTemplates(t, "channel.html", "edit_channel.html", "dashboard.html")
	db := s.db
	ownerID := insertUser(t, db, "locked-channel-owner@chan.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, db, "locked-channel-other@chan.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, db, "locked-channel-admin@chan.test", "synthetic-password", "Sofia", true)
	insertChannel(t, db, "101", "Secret Locked Channel", "Sofia", ownerID, true)
	if _, err := db.Exec(
		`update channels set description = ?, avatar_url = ? where id = '101'`,
		"SYNTHETIC-LOCKED-CHANNEL-DESCRIPTION",
		"/static/uploads/channels/101/synthetic-avatar.jpg",
	); err != nil {
		t.Fatalf("update locked channel fixture: %v", err)
	}
	insertVideo(t, db, testVideoRow{
		id: 1, channelID: "101", title: "Secret Locked Channel Video", description: "fixture",
		mediaURL: "/media/locked-channel-page.mp4", thumbURL: "/media/locked-channel-page.jpg",
		location: "Sofia", userID: ownerID, isPublic: 1,
	})
	insertChannel(t, db, "202", "Open Channel Page", "Sofia", ownerID, false)

	otherCookie := newSessionCookie(t, s, otherID)
	ownerCookie := newSessionCookie(t, s, ownerID)
	adminCookie := newSessionCookie(t, s, adminID)
	for _, tc := range []struct {
		name     string
		path     string
		cookie   *http.Cookie
		wantCode int
	}{
		{"locked anonymous", "/channel/101", nil, http.StatusNotFound},
		{"locked owner", "/channel/101", ownerCookie, http.StatusNotFound},
		{"locked other user", "/channel/101", otherCookie, http.StatusNotFound},
		{"locked admin", "/channel/101", adminCookie, http.StatusOK},
		{"open anonymous", "/channel/202", nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			rec := doRequest(t, s, http.MethodGet, tc.path, nil, cookies...)
			if rec.Code != tc.wantCode {
				t.Fatalf("GET %s status = %d, want %d", tc.path, rec.Code, tc.wantCode)
			}
			if tc.path == "/channel/101" && tc.wantCode != http.StatusOK {
				body := rec.Body.String()
				for _, secretFixture := range []string{
					"Secret Locked Channel",
					"SYNTHETIC-LOCKED-CHANNEL-DESCRIPTION",
					"/static/uploads/channels/101/synthetic-avatar.jpg",
					"Secret Locked Channel Video",
				} {
					if strings.Contains(body, secretFixture) {
						t.Errorf("denied channel response exposed %q", secretFixture)
					}
				}
			} else if tc.path == "/channel/101" && tc.wantCode == http.StatusOK {
				body := rec.Body.String()
				for _, expected := range []string{
					"Secret Locked Channel",
					"SYNTHETIC-LOCKED-CHANNEL-DESCRIPTION",
					"/static/uploads/channels/101/synthetic-avatar.jpg",
					"Secret Locked Channel Video",
				} {
					if !strings.Contains(body, expected) {
						t.Errorf("admin channel response missing %q", expected)
					}
				}
			}
		})
	}

	for _, tc := range []struct {
		name   string
		path   string
		cookie *http.Cookie
	}{
		{"owner edit access", "/edit-channel/101", ownerCookie},
		{"owner dashboard access", "/dashboard", ownerCookie},
	} {
		rec := doRequest(t, s, http.MethodGet, tc.path, nil, tc.cookie)
		if rec.Code != http.StatusOK {
			t.Errorf("%s GET %s status = %d, want 200", tc.name, tc.path, rec.Code)
		}
	}
}

func TestChannelActionsRespectLockedVisibility(t *testing.T) {
	s := newTestServer(t)
	ownerID := insertUser(t, s.db, "locked-actions-owner@chan.test", "synthetic-password", "Sofia", false)
	otherID := insertUser(t, s.db, "locked-actions-other@chan.test", "synthetic-password", "Sofia", false)
	adminID := insertUser(t, s.db, "locked-actions-admin@chan.test", "synthetic-password", "Sofia", true)
	insertChannel(t, s.db, "101", "SYNTHETIC-LOCKED-ACTION-NAME", "Sofia", ownerID, true)
	insertChannel(t, s.db, "202", "SYNTHETIC-OPEN-ACTION-NAME", "Sofia", ownerID, false)
	if _, err := s.db.Exec(
		`update channels set description = 'SYNTHETIC-LOCKED-ACTION-DESCRIPTION' where id = '101'`,
	); err != nil {
		t.Fatalf("update locked channel fixture: %v", err)
	}
	for _, subscription := range []struct {
		userID    int64
		channelID int64
		notify    int
	}{
		{ownerID, 101, 1},
		{otherID, 101, 0},
	} {
		if _, err := s.db.Exec(
			`insert into subscriptions(user_id, channel_id, notify) values(?, ?, ?)`,
			subscription.userID,
			subscription.channelID,
			subscription.notify,
		); err != nil {
			t.Fatalf("insert subscription fixture: %v", err)
		}
	}
	cookies := map[string]*http.Cookie{
		"owner": newSessionCookie(t, s, ownerID),
		"other": newSessionCookie(t, s, otherID),
		"admin": newSessionCookie(t, s, adminID),
	}

	for _, tc := range []struct {
		name     string
		role     string
		userID   int64
		action   string
		channel  int64
		wantCode int
	}{
		{"anonymous subscribe", "", 0, "subscribe", 101, http.StatusSeeOther},
		{"anonymous notify", "", 0, "notify", 101, http.StatusSeeOther},
		{"owner subscribe", "owner", ownerID, "subscribe", 101, http.StatusNotFound},
		{"owner notify", "owner", ownerID, "notify", 101, http.StatusNotFound},
		{"other subscribe", "other", otherID, "subscribe", 101, http.StatusNotFound},
		{"other notify", "other", otherID, "notify", 101, http.StatusNotFound},
		{"missing channel subscribe", "admin", adminID, "subscribe", 999, http.StatusNotFound},
		{"missing channel notify", "admin", adminID, "notify", 999, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeExists, beforeNotify := 0, 0
			if tc.userID != 0 {
				beforeExists, beforeNotify = channelSubscriptionState(t, s.db, tc.userID, tc.channel)
			}
			path := fmt.Sprintf("/channels/%d/%s", tc.channel, tc.action)
			var requestCookies []*http.Cookie
			if tc.role != "" {
				requestCookies = append(requestCookies, cookies[tc.role])
			}
			rec := doRequest(t, s, http.MethodPost, path, nil, requestCookies...)
			if rec.Code != tc.wantCode {
				t.Fatalf("POST %s status = %d, want %d", path, rec.Code, tc.wantCode)
			}
			if tc.role == "" {
				if rec.Header().Get("Location") != "/login" {
					t.Errorf("anonymous redirect location = %q, want /login", rec.Header().Get("Location"))
				}
			} else if tc.channel == 101 || tc.channel == 999 {
				if rec.Header().Get("Location") != "" {
					t.Errorf("denied action location = %q, want empty", rec.Header().Get("Location"))
				}
				for _, secret := range []string{
					"SYNTHETIC-LOCKED-ACTION-NAME",
					"SYNTHETIC-LOCKED-ACTION-DESCRIPTION",
				} {
					if strings.Contains(rec.Body.String(), secret) {
						t.Errorf("denied response exposed %q", secret)
					}
				}
			}
			if tc.userID != 0 {
				afterExists, afterNotify := channelSubscriptionState(t, s.db, tc.userID, tc.channel)
				if afterExists != beforeExists || afterNotify != beforeNotify {
					t.Errorf("denied action changed subscription state from %d/%d to %d/%d",
						beforeExists, beforeNotify, afterExists, afterNotify)
				}
			}
		})
	}

	rec := doRequest(t, s, http.MethodPost, "/channels/202/notify", nil, cookies["other"])
	if rec.Code != http.StatusNotFound {
		t.Fatalf("notify without subscription status = %d, want 404", rec.Code)
	}
	if rec.Header().Get("Location") != "" || strings.Contains(rec.Body.String(), "SYNTHETIC-OPEN-ACTION-NAME") {
		t.Error("notify without subscription returned a toast redirect or channel name")
	}
	if exists, notify := channelSubscriptionState(t, s.db, otherID, 202); exists != 0 || notify != 0 {
		t.Fatalf("missing open-channel subscription became %d/%d", exists, notify)
	}

	for _, tc := range []struct {
		role      string
		channelID int64
		action    string
		wantToast string
	}{
		{"admin", 101, "subscribe", "subscribed=SYNTHETIC-LOCKED-ACTION-NAME"},
		{"admin", 101, "notify", "notify_on=SYNTHETIC-LOCKED-ACTION-NAME"},
		{"other", 202, "subscribe", "subscribed=SYNTHETIC-OPEN-ACTION-NAME"},
		{"other", 202, "notify", "notify_on=SYNTHETIC-OPEN-ACTION-NAME"},
	} {
		path := fmt.Sprintf("/channels/%d/%s", tc.channelID, tc.action)
		rec := doRequest(t, s, http.MethodPost, path, nil, cookies[tc.role])
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s POST %s status = %d, want 303", tc.role, path, rec.Code)
		}
		if location := rec.Header().Get("Location"); !strings.Contains(location, tc.wantToast) {
			t.Errorf("%s POST %s location = %q, want %q", tc.role, path, location, tc.wantToast)
		}
	}
	if exists, notify := channelSubscriptionState(t, s.db, adminID, 101); exists != 1 || notify != 1 {
		t.Errorf("admin locked-channel state = %d/%d, want 1/1", exists, notify)
	}
	if exists, notify := channelSubscriptionState(t, s.db, otherID, 202); exists != 1 || notify != 1 {
		t.Errorf("other open-channel state = %d/%d, want 1/1", exists, notify)
	}
}

func channelSubscriptionState(t *testing.T, db *sql.DB, userID, channelID int64) (int, int) {
	t.Helper()
	var exists, notify int
	if err := db.QueryRow(
		`select count(*), coalesce(max(notify), 0) from subscriptions where user_id = ? and channel_id = ?`,
		userID,
		channelID,
	).Scan(&exists, &notify); err != nil {
		t.Fatalf("read subscription state: %v", err)
	}
	return exists, notify
}

var errInjectedIteration = errors.New("injected row iteration error")

type iterStubState struct {
	videoRowsDelivered int
	videoIterFailed    bool
}

type iterStubConnector struct {
	state *iterStubState
}

func (c iterStubConnector) Connect(context.Context) (driver.Conn, error) {
	return &iterStubConn{state: c.state}, nil
}

func (c iterStubConnector) Driver() driver.Driver { return iterStubDriver{} }

type iterStubDriver struct{}

func (iterStubDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("iterStubDriver: Open unsupported")
}

type iterStubConn struct {
	state *iterStubState
}

func (c *iterStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("iterStubConn: Prepare unsupported")
}

func (c *iterStubConn) Close() error { return nil }

func (c *iterStubConn) Begin() (driver.Tx, error) {
	return nil, errors.New("iterStubConn: Begin unsupported")
}

func (c *iterStubConn) QueryContext(
	_ context.Context, query string, _ []driver.NamedValue,
) (driver.Rows, error) {
	switch {
	case strings.Contains(query, "from videos"):
		return &iterStubRows{
			cols: []string{
				"id", "channel_id", "title", "description", "media_url", "thumbnail_url",
				"location", "name", "user_id", "is_favorite", "is_subscribed",
				"is_public", "is_admin_locked", "allow_comments", "made_for_kids",
			},
			rows: [][]driver.Value{{
				int64(1), int64(1), "Stub Video Unique", "d", "/media/s.mp4",
				"/media/s.jpg", "Sofia", "Stub Channel", int64(1), int64(0),
				int64(0), int64(1), int64(0), int64(1), int64(0),
			}},
			afterErr: errInjectedIteration,
			onRow:    func() { c.state.videoRowsDelivered++ },
			onErr:    func() { c.state.videoIterFailed = true },
		}, nil
	case strings.Contains(query, "from channels"):
		return &iterStubRows{
			cols: []string{
				"id", "name", "location", "description",
				"avatar_url", "user_id", "is_admin_locked",
			},
			rows: [][]driver.Value{{
				"1", "Stub Channel", "Sofia", "d", "", int64(1), int64(0),
			}},
		}, nil
	default:
		return &iterStubRows{cols: []string{"n"}}, nil
	}
}

type iterStubRows struct {
	cols     []string
	rows     [][]driver.Value
	pos      int
	afterErr error
	onRow    func()
	onErr    func()
}

func (r *iterStubRows) Columns() []string { return r.cols }

func (r *iterStubRows) Close() error { return nil }

func (r *iterStubRows) Next(dest []driver.Value) error {
	if r.pos < len(r.rows) {
		copy(dest, r.rows[r.pos])
		r.pos++
		if r.onRow != nil {
			r.onRow()
		}
		return nil
	}
	if r.afterErr != nil {
		if r.onErr != nil {
			r.onErr()
		}
		return r.afterErr
	}
	return io.EOF
}

func TestChannelPageIterationError(t *testing.T) {
	s := newTestServerWithTemplates(t, "channel.html")

	state := &iterStubState{}
	stub := sql.OpenDB(iterStubConnector{state: state})
	s.db = stub
	t.Cleanup(func() { stub.Close() })

	rec := doRequest(t, s, http.MethodGet, "/channel/1", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body != "Internal server error\n" {
		t.Errorf("body = %q, want generic internal error", body)
	}
	if state.videoRowsDelivered != 1 {
		t.Errorf("video rows delivered = %d, want 1", state.videoRowsDelivered)
	}
	if !state.videoIterFailed {
		t.Error("video iterator error was never injected")
	}
}
