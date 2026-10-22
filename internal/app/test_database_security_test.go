package app

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestNewEnablesForeignKeysOnEveryPooledConnection(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := New()
	if err != nil {
		t.Fatalf("initialize disposable application database: %v", err)
	}

	connections := make([]*sql.Conn, 0, 3)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
		_ = s.db.Close()
	}()

	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		connection, err := s.db.Conn(ctx)
		cancel()
		if err != nil {
			t.Fatalf("acquire pooled connection: %v", err)
		}
		connections = append(connections, connection)
	}
	for index, connection := range connections {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var enabled int
		err := connection.QueryRowContext(ctx, `pragma foreign_keys`).Scan(&enabled)
		cancel()
		if err != nil {
			t.Fatalf("read foreign_keys on connection %d: %v", index, err)
		}
		if enabled != 1 {
			t.Errorf("foreign_keys on connection %d = %d, want 1", index, enabled)
		}

		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		_, err = connection.ExecContext(
			ctx,
			`insert into sessions(token, user_id) values(?, ?)`,
			fmt.Sprintf("orphan-session-%d", index),
			int64(-index-1),
		)
		cancel()
		if err == nil {
			t.Errorf("connection %d accepted a session for a missing user", index)
		}
	}
}
