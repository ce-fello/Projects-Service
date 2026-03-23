package main

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerAppliesTimeouts(t *testing.T) {
	server := newHTTPServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"8000",
		http.NewServeMux(),
	)

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", server.ReadHeaderTimeout, 5*time.Second)
	}
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("ReadTimeout = %v, want %v", server.ReadTimeout, 10*time.Second)
	}
	if server.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %v, want %v", server.WriteTimeout, 10*time.Second)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %v, want %v", server.IdleTimeout, 60*time.Second)
	}
}
