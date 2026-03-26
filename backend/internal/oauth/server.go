package oauth

import (
	"net/url"
)

// TokenGenerator bridges OAuth2 token generation to our auth.Service JWT generation.
type TokenGenerator interface {
	GenerateAccessToken(userID int, username, role string) (accessToken, refreshToken string, expiresIn int, err error)
}

// Server provides OAuth2 server configuration.
type Server struct {
	// clientIDs holds the set of registered public client IDs.
	clientIDs map[string]bool
}

// NewServer creates a new OAuth2 server with ae-cli as pre-registered public client.
func NewServer() *Server {
	return &Server{
		clientIDs: map[string]bool{
			"ae-cli": true,
		},
	}
}

// IsValidClient checks if the given client_id is registered.
func (s *Server) IsValidClient(clientID string) bool {
	return s.clientIDs[clientID]
}

// ValidateRedirectURI checks that the redirect_uri is a valid localhost callback.
// Host must be localhost or 127.0.0.1, port must be numeric, path must be /callback.
func ValidateRedirectURI(rawURI string) bool {
	u, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return false
	}
	port := u.Port()
	if port == "" {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	if u.Path != "/callback" {
		return false
	}
	return true
}
