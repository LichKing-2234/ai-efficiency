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

// NewDefault returns a client with the platform's default bounded transport and the supplied overall timeout.
func NewDefault(overallTimeout time.Duration) *http.Client {
	return New(Options{
		ConnectTimeout:        5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		OverallTimeout:        overallTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
	})
}

func New(options Options, wrappers ...TransportWrapper) *http.Client {
	dialer := &net.Dialer{
		Timeout:   options.ConnectTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
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

	var roundTripper http.RoundTripper = transport
	for index := len(wrappers) - 1; index >= 0; index-- {
		roundTripper = wrappers[index](roundTripper)
	}

	return &http.Client{
		Transport: roundTripper,
		Timeout:   options.OverallTimeout,
	}
}
