package proxy

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProxyChildProcess(t *testing.T) {
	if os.Getenv(testChildEnv) != "1" {
		t.Skip("helper process")
	}
	args := os.Args
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(args) {
		fmt.Fprintln(os.Stderr, "missing runtime config path")
		os.Exit(2)
	}
	if err := ServeFromConfigFile(args[sep+1]); err != nil {
		fmt.Fprintf(os.Stderr, "proxy child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestSpawnStartsProxyAndReturnsReadyAddress(t *testing.T) {
	cfg := RuntimeConfig{
		SessionID:   "sess-test",
		ListenAddr:  "127.0.0.1:0",
		AuthToken:   "tok-test",
		ProviderURL: "http://example.local",
		ProviderKey: "provider-key",
	}

	pid, listenAddr, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() {
		_ = Stop(pid)
	}()

	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	if strings.TrimSpace(listenAddr) == "" {
		t.Fatal("listenAddr should not be empty")
	}

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Get("http://" + listenAddr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestStopTerminatesProxyProcess(t *testing.T) {
	cfg := RuntimeConfig{
		SessionID:  "sess-stop",
		ListenAddr: "127.0.0.1:0",
		AuthToken:  "tok-stop",
	}

	pid, listenAddr, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := Stop(pid); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := (&http.Client{Timeout: 150 * time.Millisecond}).Get("http://" + listenAddr + "/healthz")
		if err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("proxy still reachable after stop at %s", listenAddr)
}
