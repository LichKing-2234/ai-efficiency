package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRuntimeBundleUsesRestrictedPermissions(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	b := &RuntimeBundle{
		SessionID:  "sess-1",
		RuntimeRef: "rt-1",
		Proxy: &ProxyRuntime{
			PID:        1234,
			ListenAddr: "127.0.0.1:18080",
			AuthToken:  "proxy-token",
			ConfigPath: "/tmp/ae-proxy-config-123/runtime.json",
		},
		EnvBundle: map[string]string{
			"AE_SESSION_ID": "sess-1",
		},
		KeyExpiresAt: time.Now().UTC().Truncate(time.Second).Add(1 * time.Hour),
	}

	if err := WriteRuntimeBundle(b); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	d := runtimeDir(b.SessionID)
	dirInfo, err := os.Stat(d)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("runtime dir perms = %o, want %o", dirInfo.Mode().Perm(), 0o700)
	}

	f := filepath.Join(d, "runtime.json")
	fileInfo, err := os.Stat(f)
	if err != nil {
		t.Fatalf("stat runtime file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("runtime file perms = %o, want %o", fileInfo.Mode().Perm(), 0o600)
	}

	collectorsInfo, err := os.Stat(filepath.Join(d, "collectors"))
	if err != nil {
		t.Fatalf("stat collectors dir: %v", err)
	}
	if collectorsInfo.Mode().Perm() != 0o700 {
		t.Fatalf("collectors dir perms = %o, want %o", collectorsInfo.Mode().Perm(), 0o700)
	}
}

func TestWriteAndReadRuntimeBundleIncludesProxyMetadata(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	in := &RuntimeBundle{
		SessionID: "sess-with-proxy",
		Proxy: &ProxyRuntime{
			PID:        4321,
			ListenAddr: "127.0.0.1:19999",
			AuthToken:  "tok-proxy",
			ConfigPath: "/tmp/ae-proxy-config-xyz/runtime.json",
		},
	}
	if err := WriteRuntimeBundle(in); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	out, err := ReadRuntimeBundle("sess-with-proxy")
	if err != nil {
		t.Fatalf("ReadRuntimeBundle: %v", err)
	}
	if out.Proxy == nil {
		t.Fatal("expected proxy metadata in runtime bundle")
	}
	if out.Proxy.PID != 4321 {
		t.Fatalf("proxy pid = %d, want %d", out.Proxy.PID, 4321)
	}
	if out.Proxy.ListenAddr != "127.0.0.1:19999" {
		t.Fatalf("proxy listen_addr = %q, want %q", out.Proxy.ListenAddr, "127.0.0.1:19999")
	}
	if out.Proxy.AuthToken != "tok-proxy" {
		t.Fatalf("proxy auth_token = %q, want %q", out.Proxy.AuthToken, "tok-proxy")
	}
	if out.Proxy.ConfigPath != "/tmp/ae-proxy-config-xyz/runtime.json" {
		t.Fatalf("proxy config_path = %q, want %q", out.Proxy.ConfigPath, "/tmp/ae-proxy-config-xyz/runtime.json")
	}
}
