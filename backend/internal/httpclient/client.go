package httpclient

import (
	"net"
	"net/http"
	"time"
)

type Options struct {
	ConnectTimeout        time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	OverallTimeout        time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
}

type TransportWrapper func(http.RoundTripper) http.RoundTripper

var compatibilityTransport = newTransport(defaultOptions(0))

// NewDefault returns a client with the supplied overall timeout over one process-lifetime
// package compatibility transport. Production pool owners use New for private transports.
func NewDefault(overallTimeout time.Duration) *http.Client {
	return &http.Client{
		Transport: compatibilityTransport,
		Timeout:   overallTimeout,
	}
}

func defaultOptions(overallTimeout time.Duration) Options {
	return Options{
		ConnectTimeout:        5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		OverallTimeout:        overallTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
	}
}

func New(options Options, wrappers ...TransportWrapper) *http.Client {
	transport := newTransport(options)

	var roundTripper http.RoundTripper = transport
	for index := len(wrappers) - 1; index >= 0; index-- {
		roundTripper = wrappers[index](roundTripper)
	}

	return &http.Client{
		Transport: roundTripper,
		Timeout:   options.OverallTimeout,
	}
}

func newTransport(options Options) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   options.ConnectTimeout,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   options.TLSHandshakeTimeout,
		ResponseHeaderTimeout: options.ResponseHeaderTimeout,
		IdleConnTimeout:       options.IdleConnTimeout,
		MaxIdleConns:          options.MaxIdleConns,
		MaxIdleConnsPerHost:   options.MaxIdleConnsPerHost,
		MaxConnsPerHost:       options.MaxConnsPerHost,
	}
}
