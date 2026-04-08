package buildinfo

// These variables are set at build time via -ldflags.
// Example: go build -ldflags "-X ae-cli/internal/buildinfo.ServerURL=https://ae.example.com"
var (
	ServerURL = "http://localhost:8081"
	Version   = "v0.1.0"
)
