# ae-cli Graceful Shutdown Hook 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 当 ae-cli 进程收到 SIGINT/SIGTERM 信号时，自动调用后端 API 将 session 标记为 completed，避免 session 永远停留在 active 状态。

**Architecture:** 在 `session.Manager` 中新增 best-effort 的 `Shutdown` 方法（忽略 API 错误，确保 state 文件清理）。在 `cmd/start.go` 中注册 SIGINT/SIGTERM 信号监听，收到信号时调用 Shutdown 后退出。在 `cmd/shell.go` 中只监听 SIGTERM（因为 SIGINT/Ctrl+C 在交互式 shell 中用于取消当前输入，不应终止 session），正常退出时也清理信号 goroutine。

**Tech Stack:** Go 1.26+, os/signal, syscall, context

**Status:** ✅ 已完成（2026-03-25）

**Replay Status:** 已完成且结构上仍接近可复用计划。若需再次执行，可先按当前代码快速复核后复用本文任务拆分。

> **Updated:** 2026-03-25 — 基于代码审查确认全部 3 个 Task 已实现，checkbox 已勾选。

**Design decisions:**
- `Shutdown` 是 best-effort 的：API 失败时仍清理本地 state 文件，不返回错误
- `start.go` 监听 SIGINT + SIGTERM（非交互式，信号意味着用户要退出）
- `shell.go` 只监听 SIGTERM（交互式 shell 中 Ctrl+C 不应杀掉 session）
- 信号 goroutine 通过 close(sigCh) 清理，避免泄漏
- `os.Exit(0)` 会跳过 defer，但当前代码路径中没有关键 defer，可接受

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `ae-cli/internal/session/manager.go` | 新增 `Shutdown(ctx)` 方法 |
| Modify | `ae-cli/internal/session/session_test.go` | 新增 Shutdown 测试（含 `"context"` import） |
| Modify | `ae-cli/cmd/start.go` | 注册 SIGINT/SIGTERM 信号处理，正常退出时清理 goroutine |
| Modify | `ae-cli/cmd/shell.go` | 注册 SIGTERM 信号处理，正常退出时清理 goroutine |

---

### Task 1: Shutdown 方法 — 测试 + 实现

**Files:**
- Modify: `ae-cli/internal/session/manager.go:80` (在 `Stop()` 方法后)
- Modify: `ae-cli/internal/session/session_test.go` (末尾添加测试，import 中添加 `"context"`)

- [x] **Step 1: 在 session_test.go import 中添加 `"context"`**

在 `ae-cli/internal/session/session_test.go` 的 import block 中添加 `"context"`：

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
)
```

- [x] **Step 2: 写 TestShutdownSuccess 失败测试**

在 `session_test.go` 末尾添加：

```go
func TestShutdownSuccess(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	var stopCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stopCalled = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{ID: "shutdown-test", Repo: "org/repo", Branch: "main"}
	writeState(state)

	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !stopCalled {
		t.Error("expected stop API to be called")
	}

	// State file should be removed
	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current != nil {
		t.Error("expected nil state after Shutdown")
	}
}
```

- [x] **Step 3: 写 TestShutdownNoSession 失败测试**

```go
func TestShutdownNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// Should not error when no session exists
	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown with no session should not error, got: %v", err)
	}
}
```

- [x] **Step 4: 写 TestShutdownAPIError 失败测试**

```go
func TestShutdownAPIError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{ID: "shutdown-api-err", Repo: "org/repo", Branch: "main"}
	writeState(state)

	// Should not error — best-effort, ignores API failure
	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown should not error on API failure, got: %v", err)
	}

	// State file should still be removed even if API fails
	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current != nil {
		t.Error("expected state file removed even when API fails")
	}
}
```

- [x] **Step 5: 运行测试确认编译失败**

Run: `cd ae-cli && go test ./internal/session/ -run 'TestShutdown' -v`
Expected: 编译错误 `m.Shutdown undefined`

- [x] **Step 6: 在 manager.go 中添加 `"context"` import 并实现 Shutdown 方法**

在 `manager.go` 的 import block 中添加 `"context"`，然后在 `Stop()` 方法后面添加：

```go
// Shutdown performs a best-effort session cleanup on process exit.
// Unlike Stop, it ignores API errors to ensure the state file is always removed.
func (m *Manager) Shutdown(ctx context.Context) error {
	state, err := m.Current()
	if err != nil || state == nil {
		return nil
	}

	// Best-effort: try to notify backend, ignore errors
	_ = m.client.StopSession(ctx, state.ID)

	// Always clean up local state
	_ = removeState()
	return nil
}
```

- [x] **Step 7: 运行测试确认全部通过**

Run: `cd ae-cli && go test ./internal/session/ -run 'TestShutdown' -v`
Expected: 3 个测试全部 PASS

- [x] **Step 8: 运行全部 session 测试确认无回归**

Run: `cd ae-cli && go test ./internal/session/ -v`
Expected: 全部 PASS

- [x] **Step 9: Commit**

```bash
git add ae-cli/internal/session/manager.go ae-cli/internal/session/session_test.go
git commit -m "feat(ae-cli): add best-effort Shutdown method to session manager"
```

---

### Task 2: start 命令注册信号处理

**Files:**
- Modify: `ae-cli/cmd/start.go`

- [x] **Step 1: 修改 start.go 的 import block**

将 `ae-cli/cmd/start.go` 的 import 替换为：

```go
import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)
```

- [x] **Step 2: 在 RunE 中添加信号处理**

在 `state, err := mgr.Start()` 成功后（`fmt.Printf("Session started!\n")` 之前），插入：

```go
		// Register signal handler for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig, ok := <-sigCh
			if !ok {
				return // channel closed, normal exit
			}
			_ = sig
			signal.Stop(sigCh)
			if state.TmuxSession != "" && tmux.SessionExists(state.TmuxSession) {
				_ = tmux.KillSession(state.TmuxSession)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mgr.Shutdown(ctx)
			os.Exit(0)
		}()
```

在函数末尾 `return nil` 之前（tmux attach 正常返回后），添加清理代码：

```go
		signal.Stop(sigCh)
		close(sigCh)
```

- [x] **Step 3: 确认编译通过**

Run: `cd ae-cli && go build ./...`
Expected: 编译成功

- [x] **Step 4: Commit**

```bash
git add ae-cli/cmd/start.go
git commit -m "feat(ae-cli): register signal handler in start command for graceful shutdown"
```

---

### Task 3: shell 命令注册信号处理

**Files:**
- Modify: `ae-cli/cmd/shell.go`

注意：shell 是交互式的，只监听 SIGTERM，不监听 SIGINT（Ctrl+C 在交互式 shell 中用于取消当前输入）。

- [x] **Step 1: 修改 shell.go 的 import block**

将 `ae-cli/cmd/shell.go` 的 import 替换为：

```go
import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/shell"
	"github.com/spf13/cobra"
)
```

- [x] **Step 2: 修改 RunE 函数添加信号处理**

将 `shellCmd` 的 `RunE` 替换为：

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.NewManager(apiClient, cfg)
		state, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if state == nil {
			return fmt.Errorf("no active session")
		}

		// Register signal handler — only SIGTERM, not SIGINT
		// SIGINT (Ctrl+C) is used to cancel current input in interactive shells
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM)
		go func() {
			sig, ok := <-sigCh
			if !ok {
				return // channel closed, normal exit
			}
			_ = sig
			signal.Stop(sigCh)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mgr.Shutdown(ctx)
			os.Exit(0)
		}()

		s := shell.New(cfg, state)
		err = s.Run()

		// Clean up signal goroutine on normal exit
		signal.Stop(sigCh)
		close(sigCh)

		return err
	},
```

- [x] **Step 3: 确认编译通过**

Run: `cd ae-cli && go build ./...`
Expected: 编译成功

- [x] **Step 4: Commit**

```bash
git add ae-cli/cmd/shell.go
git commit -m "feat(ae-cli): register SIGTERM handler in shell command for graceful shutdown"
```

---

### Task 4: 全量测试验证

- [x] **Step 1: 运行 ae-cli 全部测试**

Run: `cd ae-cli && go test ./... -v`
Expected: 全部 PASS

- [x] **Step 2: 确认无 lint 问题**

Run: `cd ae-cli && go vet ./...`
Expected: 无输出（无问题）

- [x] **Step 3: 最终 commit（如有遗漏修复）**

如果前面的步骤中有任何修复，在这里统一 commit。
