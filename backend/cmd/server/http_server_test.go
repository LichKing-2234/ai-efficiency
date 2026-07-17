package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
)

func TestNewHTTPServerConfiguresRuntimeBudgets(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer("127.0.0.1:8081", handler, config.ServerConfig{
		ReadHeaderTimeoutSeconds: 7,
		IdleTimeoutSeconds:       123,
	})

	if server.Addr != "127.0.0.1:8081" {
		t.Errorf("Addr = %q, want %q", server.Addr, "127.0.0.1:8081")
	}
	if server.Handler != handler {
		t.Errorf("Handler = %T, want supplied handler", server.Handler)
	}
	if server.ReadHeaderTimeout != 7*time.Second {
		t.Errorf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, 7*time.Second)
	}
	if server.IdleTimeout != 123*time.Second {
		t.Errorf("IdleTimeout = %s, want %s", server.IdleTimeout, 123*time.Second)
	}
	if server.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %s, want no synchronous request budget", server.ReadTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %s, want no synchronous response budget", server.WriteTimeout)
	}
}

func TestNewHTTPServerClosesSlowHeader(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := newHTTPServer(listener.Addr().String(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler called for incomplete request headers")
	}), config.ServerConfig{ReadHeaderTimeoutSeconds: 1})
	server.ReadHeaderTimeout = 50 * time.Millisecond

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
		select {
		case err := <-serveResult:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve() did not stop")
		}
	})

	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: example.test\r\nX-Incomplete:"); err != nil {
		t.Fatalf("write incomplete request: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	buffer := make([]byte, 256)
	for {
		_, err = connection.Read(buffer)
		if err == nil {
			continue
		}
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			t.Fatal("server did not close the slow-header connection before the one-second deadline")
		}
		break
	}
}
