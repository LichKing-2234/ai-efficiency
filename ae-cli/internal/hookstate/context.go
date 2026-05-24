package hookstate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

type Context struct {
	ServerURL   string `json:"server_url,omitempty"`
	AuthSubject string `json:"auth_subject,omitempty"`
	RepoKey     string `json:"repo_key,omitempty"`
}

func NormalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func (c Context) Normalized() Context {
	return Context{
		ServerURL:   NormalizeServerURL(c.ServerURL),
		AuthSubject: strings.TrimSpace(c.AuthSubject),
		RepoKey:     strings.TrimSpace(c.RepoKey),
	}
}

func (c Context) Stable() bool {
	n := c.Normalized()
	return n.ServerURL != "" && n.AuthSubject != "" && n.RepoKey != ""
}

func (c Context) CacheKey() string {
	n := c.Normalized()
	sum := sha256.Sum256([]byte(n.ServerURL + "\x1f" + n.AuthSubject + "\x1f" + n.RepoKey))
	return hex.EncodeToString(sum[:])
}
