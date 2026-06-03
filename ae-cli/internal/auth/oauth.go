package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/httpx"
)

// OAuthConfig holds the OAuth login configuration.
type OAuthConfig struct {
	ServerURL  string
	ClientID   string
	Timeout    time.Duration
	HTTPClient *http.Client
	Output     io.Writer
	Sleep      func(time.Duration)
}

// OAuthResult contains the result of a successful OAuth login.
type OAuthResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// Login performs the full OAuth2 Authorization Code Flow with PKCE.
func Login(ctx context.Context, cfg OAuthConfig) (*OAuthResult, error) {
	cfg = withOAuthDefaults(cfg)

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("start local server: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization denied: %s", errMsg)
			fmt.Fprintf(w, "<html><body><h1>Authorization denied</h1><p>You can close this window.</p></body></html>")
			return
		}

		receivedState := r.URL.Query().Get("state")
		if receivedState != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}

		codeCh <- code
		fmt.Fprintf(w, "<html><body><h1>Login successful!</h1><p>You can close this window and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	authURL := fmt.Sprintf("%s/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		cfg.ServerURL,
		url.QueryEscape(cfg.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(challenge),
		url.QueryEscape(state),
	)

	fmt.Fprintf(cfg.Output, "Opening browser for login...\n")
	fmt.Fprintf(cfg.Output, "If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	fmt.Fprintf(cfg.Output, "Waiting for authorization (timeout: %s)...\n", cfg.Timeout)
	select {
	case code := <-codeCh:
		return exchangeCodeWithClient(ctx, cfg.HTTPClient, cfg.ServerURL, code, redirectURI, verifier)
	case err := <-errCh:
		return nil, err
	case <-time.After(cfg.Timeout):
		return nil, fmt.Errorf("login timed out after %s", cfg.Timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(ctx context.Context, serverURL, code, redirectURI, verifier string) (*OAuthResult, error) {
	return exchangeCodeWithClient(ctx, http.DefaultClient, serverURL, code, redirectURI, verifier)
}

func exchangeCodeWithClient(ctx context.Context, client *http.Client, serverURL, code, redirectURI, verifier string) (*OAuthResult, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {"ae-cli"},
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := httpx.DoForm(ctx, client, http.MethodPost, serverURL+"/oauth/token", data, &tokenResp, httpx.Options{}); err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	return &OAuthResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Start()
}
