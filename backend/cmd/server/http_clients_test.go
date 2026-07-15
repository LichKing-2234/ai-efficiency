package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/httpclient"
)

func TestNewRuntimeHTTPClientsUsesBoundedIsolatedPoolsAndStrictTimeouts(t *testing.T) {
	cfg := config.HTTPClientConfig{
		ConnectTimeoutSeconds:        5,
		TLSHandshakeTimeoutSeconds:   5,
		ResponseHeaderTimeoutSeconds: 15,
		OverallTimeoutSeconds:        30,
		IdleConnTimeoutSeconds:       90,
		MaxIdleConns:                 100,
		MaxIdleConnsPerHost:          20,
		MaxConnsPerHost:              50,
	}
	clients := newRuntimeHTTPClients(cfg)

	relayClients := map[string]*http.Client{
		"runtime relay":          clients.runtimeRelay,
		"provider-created relay": clients.providerRelay,
		"settings":               clients.settings,
	}
	for name, client := range relayClients {
		if client != clients.runtimeRelay {
			t.Fatalf("%s client = %p, want shared Relay client %p", name, client, clients.runtimeRelay)
		}
		assertBoundedRuntimeClient(t, name, client, 30*time.Second)
	}

	generalClients := map[string]*http.Client{
		"directory": clients.directory,
		"scm":       clients.scm,
	}
	for name, client := range generalClients {
		if client != clients.directory {
			t.Fatalf("%s client = %p, want shared general downstream client %p", name, client, clients.directory)
		}
		assertBoundedRuntimeClient(t, name, client, 30*time.Second)
	}

	if clients.runtimeRelay == clients.directory {
		t.Fatal("Relay and general downstream clients must use separate connection pools")
	}
	if clients.runtimeRelay.Transport == clients.directory.Transport {
		t.Fatal("Relay transport must be isolated so it can be wrapped without affecting Directory or SCM")
	}
	if clients.version == clients.runtimeRelay || clients.version == clients.directory ||
		clients.webhook == clients.runtimeRelay || clients.webhook == clients.directory ||
		clients.version == clients.webhook {
		t.Fatal("version and webhook clients must each use separate connection pools")
	}
	assertBoundedRuntimeClient(t, "version", clients.version, 10*time.Second)
	assertBoundedRuntimeClient(t, "webhook", clients.webhook, 5*time.Second)

	t.Cleanup(clients.runtimeRelay.CloseIdleConnections)
	t.Cleanup(clients.directory.CloseIdleConnections)
	t.Cleanup(clients.version.CloseIdleConnections)
	t.Cleanup(clients.webhook.CloseIdleConnections)
}

func TestNewRuntimeHTTPClientsWrapsOnlyRelayPool(t *testing.T) {
	cfg := config.HTTPClientConfig{
		ConnectTimeoutSeconds:        5,
		TLSHandshakeTimeoutSeconds:   5,
		ResponseHeaderTimeoutSeconds: 15,
		OverallTimeoutSeconds:        30,
		IdleConnTimeoutSeconds:       90,
		MaxIdleConns:                 100,
		MaxIdleConnsPerHost:          20,
		MaxConnsPerHost:              50,
	}
	wrapper := func(next http.RoundTripper) http.RoundTripper {
		return &runtimeRelayTestTransport{next: next}
	}

	clients := newRuntimeHTTPClients(cfg, httpclient.TransportWrapper(wrapper))

	if _, ok := clients.runtimeRelay.Transport.(*runtimeRelayTestTransport); !ok {
		t.Fatalf("runtime Relay transport = %T, want telemetry wrapper", clients.runtimeRelay.Transport)
	}
	if clients.providerRelay != clients.runtimeRelay || clients.settings != clients.runtimeRelay {
		t.Fatal("all Relay consumers must share the wrapped Relay client")
	}
	for name, client := range map[string]*http.Client{
		"directory": clients.directory,
		"scm":       clients.scm,
		"version":   clients.version,
		"webhook":   clients.webhook,
	} {
		if _, ok := client.Transport.(*runtimeRelayTestTransport); ok {
			t.Fatalf("%s transport was incorrectly classified as Relay", name)
		}
	}

	t.Cleanup(clients.runtimeRelay.CloseIdleConnections)
	t.Cleanup(clients.directory.CloseIdleConnections)
	t.Cleanup(clients.version.CloseIdleConnections)
	t.Cleanup(clients.webhook.CloseIdleConnections)
}

type runtimeRelayTestTransport struct {
	next http.RoundTripper
}

func (t *runtimeRelayTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.next.RoundTrip(request)
}

func assertBoundedRuntimeClient(t *testing.T, name string, client *http.Client, overallTimeout time.Duration) {
	t.Helper()
	if client == nil {
		t.Fatalf("%s client is nil", name)
	}
	if client.Timeout != overallTimeout {
		t.Fatalf("%s Timeout = %s, want %s", name, client.Timeout, overallTimeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("%s transport = %T, want *http.Transport", name, client.Transport)
	}
	if transport.DialContext == nil {
		t.Fatalf("%s DialContext is nil", name)
	}
	if transport.TLSHandshakeTimeout != 5*time.Second || transport.ResponseHeaderTimeout != 15*time.Second {
		t.Fatalf("%s handshake/header timeouts = %s/%s", name, transport.TLSHandshakeTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.IdleConnTimeout != 90*time.Second || transport.MaxIdleConns != 100 || transport.MaxIdleConnsPerHost != 20 || transport.MaxConnsPerHost != 50 {
		t.Fatalf("%s connection pool is not bounded as configured", name)
	}
}
