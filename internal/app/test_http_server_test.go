package app

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerConfiguration(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	t.Setenv("VIDEO_SHARE_ADDR", "")
	server := s.httpServer()
	if server.Addr != "127.0.0.1:8080" {
		t.Errorf("default address = %q, want loopback", server.Addr)
	}
	if server.Handler != s.mux {
		t.Error("HTTP server handler does not use the application mux")
	}
	if server.ReadHeaderTimeout != 5*time.Second ||
		server.ReadTimeout != 10*time.Minute ||
		server.IdleTimeout != 60*time.Second ||
		server.MaxHeaderBytes != 1<<20 {
		t.Errorf("HTTP server limits = header %s, read %s, idle %s, max header %d",
			server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout, server.MaxHeaderBytes)
	}
	if server.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %s, want zero for media streaming", server.WriteTimeout)
	}

	t.Setenv("VIDEO_SHARE_ADDR", " :8080 ")
	if address := s.httpServer().Addr; address != ":8080" {
		t.Errorf("explicit remote-binding address = %q, want :8080", address)
	}
}
