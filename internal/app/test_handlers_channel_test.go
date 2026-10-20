package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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
