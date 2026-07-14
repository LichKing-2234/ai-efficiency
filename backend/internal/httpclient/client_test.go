package httpclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewBoundsResponseHeadersAndOverallRequest(t *testing.T) {
	t.Run("response headers", func(t *testing.T) {
		endpoint := newWithholdingEndpoint(t, false)
		client := New(Options{
			ConnectTimeout:        100 * time.Millisecond,
			TLSHandshakeTimeout:   100 * time.Millisecond,
			ResponseHeaderTimeout: 40 * time.Millisecond,
			OverallTimeout:        2 * time.Second,
		})
		t.Cleanup(client.CloseIdleConnections)

		startedAt := time.Now()
		_, err := client.Get(endpoint.URL())
		assertTimeout(t, err)
		if elapsed := time.Since(startedAt); elapsed >= time.Second {
			t.Fatalf("response-header timeout took %s, want less than the overall request budget", elapsed)
		}
	})

	t.Run("overall request body", func(t *testing.T) {
		endpoint := newWithholdingEndpoint(t, true)
		client := New(Options{
			ConnectTimeout:        100 * time.Millisecond,
			TLSHandshakeTimeout:   100 * time.Millisecond,
			ResponseHeaderTimeout: 500 * time.Millisecond,
			OverallTimeout:        40 * time.Millisecond,
		})
		t.Cleanup(client.CloseIdleConnections)

		response, err := client.Get(endpoint.URL())
		if err != nil {
			t.Fatalf("Get() before body read error = %v", err)
		}
		defer response.Body.Close()

		_, err = io.ReadAll(response.Body)
		assertTimeout(t, err)
	})
}

func TestNewConfiguresBoundedConnectionPool(t *testing.T) {
	options := Options{
		ConnectTimeout:        11 * time.Millisecond,
		TLSHandshakeTimeout:   12 * time.Millisecond,
		ResponseHeaderTimeout: 13 * time.Millisecond,
		OverallTimeout:        14 * time.Millisecond,
		IdleConnTimeout:       15 * time.Millisecond,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   17,
		MaxConnsPerHost:       18,
	}

	client := New(options)
	t.Cleanup(client.CloseIdleConnections)

	if client == http.DefaultClient {
		t.Fatal("New() returned http.DefaultClient")
	}
	if client.Timeout != options.OverallTimeout {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, options.OverallTimeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport == http.DefaultTransport {
		t.Fatal("New() reused http.DefaultTransport")
	}
	if transport.DialContext == nil {
		t.Fatal("transport.DialContext is nil, want bounded net.Dialer")
	}
	if transport.TLSHandshakeTimeout != options.TLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, options.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != options.ResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, options.ResponseHeaderTimeout)
	}
	if transport.IdleConnTimeout != options.IdleConnTimeout {
		t.Errorf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, options.IdleConnTimeout)
	}
	if transport.MaxIdleConns != options.MaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, options.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != options.MaxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, options.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != options.MaxConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want %d", transport.MaxConnsPerHost, options.MaxConnsPerHost)
	}
}

func TestNewDefaultUsesBoundedRuntimeConfiguration(t *testing.T) {
	client := NewDefault(10 * time.Second)
	t.Cleanup(client.CloseIdleConnections)

	if client.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %s, want 10s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.DialContext == nil {
		t.Fatal("transport.DialContext is nil")
	}
	if transport.TLSHandshakeTimeout != 5*time.Second || transport.ResponseHeaderTimeout != 15*time.Second {
		t.Fatalf("handshake/header timeouts = %s/%s, want 5s/15s", transport.TLSHandshakeTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.IdleConnTimeout != 90*time.Second || transport.MaxIdleConns != 100 || transport.MaxIdleConnsPerHost != 20 || transport.MaxConnsPerHost != 50 {
		t.Fatal("default connection pool does not use bounded runtime configuration")
	}
}

func TestNewAppliesTransportWrappersInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	var events []string
	var mu sync.Mutex
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	wrapper := func(name string) TransportWrapper {
		return func(next http.RoundTripper) http.RoundTripper {
			return roundTripFunc(func(request *http.Request) (*http.Response, error) {
				record(name + ":before")
				response, err := next.RoundTrip(request)
				record(name + ":after")
				return response, err
			})
		}
	}

	client := New(Options{OverallTimeout: time.Second}, wrapper("first"), wrapper("second"))
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	response.Body.Close()

	want := []string{"first:before", "second:before", "second:after", "first:after"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("wrapper events = %v, want %v", events, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type withholdingEndpoint struct {
	listener net.Listener
	release  chan struct{}
	done     chan error
}

func newWithholdingEndpoint(t *testing.T, writeHeaders bool) *withholdingEndpoint {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	endpoint := &withholdingEndpoint{
		listener: listener,
		release:  make(chan struct{}),
		done:     make(chan error, 1),
	}

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			endpoint.done <- err
			return
		}
		defer connection.Close()

		request, err := http.ReadRequest(bufio.NewReader(connection))
		if err != nil {
			endpoint.done <- fmt.Errorf("read request: %w", err)
			return
		}
		request.Body.Close()
		if writeHeaders {
			if _, err := io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Length: 1\r\n\r\n"); err != nil {
				endpoint.done <- fmt.Errorf("write headers: %w", err)
				return
			}
		}
		<-endpoint.release
		endpoint.done <- nil
	}()

	t.Cleanup(func() {
		close(endpoint.release)
		listener.Close()
		select {
		case err := <-endpoint.done:
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("synthetic endpoint: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("synthetic endpoint did not stop")
		}
	})
	return endpoint
}

func (e *withholdingEndpoint) URL() string {
	return "http://" + e.listener.Addr().String()
}

func assertTimeout(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("request error = nil, want timeout")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return
	}
	var timeout interface{ Timeout() bool }
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("request error = %T %v, want timeout", err, err)
	}
}
