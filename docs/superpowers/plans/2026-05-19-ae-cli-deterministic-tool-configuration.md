# ae-cli Deterministic Tool Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** The original rollout and the 2026-05-29 Codex app-only follow-up are complete. The 2026-07-18 explicit tool-selection and ChatGPT app follow-up is approved and ready for execution; its unchecked steps are the active work ledger.

**Goal:** Keep deterministic provider configuration intact while allowing users to explicitly configure supported tools that are not locally detectable and recognizing the renamed macOS `ChatGPT.app` as Codex.

**Architecture:** Keep explicit selection in `ae-cli/cmd/discover.go`: the command validates and deduplicates `--tool` values, then passes the resulting list through the existing `toolconfig.ConfigureTools` boundary. Keep `toolconfig.DetectInstalledTools` fact-based and extend only its Codex app-bundle candidates. No backend API or provider credential contract changes are required.

**Tech Stack:** Go, Cobra, `os/exec`, existing Go unit tests, Bash mock E2E.

## Global Constraints

- `--tool` accepts only `codex`, `claude`, and `gemini`, supports repeated and comma-separated values, deduplicates in first-seen order, and rejects unknown or blank values.
- Supplying `--tool` bypasses installation detection only; provider selection and platform credential checks remain unchanged.
- Omitting `--tool` preserves automatic detection.
- Codex automatic app detection checks user and system `ChatGPT.app` before the corresponding legacy `Codex.app` locations.
- Claude and Gemini remain PATH-only.
- Tests and examples use placeholder users, domains, and credentials only.

---

## Steps

- [x] Add failing tests for provider fetch, tool detection, tool config writers, and the discover command flow.
- [x] Implement `ae-cli discover` plus the `ae-cli/internal/toolconfig` package.
- [x] Add `GET /api/v1/providers` client parsing in `ae-cli/internal/client`.
- [x] Fix `backend/internal/handler/provider.go` to read the authenticated user from `auth.GetUserContext`.
- [x] Update CLI docs and add a current-contract spec for deterministic tool configuration.
- [x] Run `cd ae-cli && go test ./...`.
- [x] Add CI coverage for the mock `ae-cli discover` end-to-end path and verify it locally with `bash ae-cli/test/discover-e2e.sh <built-binary>`.
- [x] Run `cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run TestListProvidersForUserWithValidToken -v`.
- [x] Run a mock end-to-end `ae-cli discover` execution against a local stub `/api/v1/providers` server and verify the generated files under a temporary `HOME`.

## Known Remaining Gaps

- The existing live process behind `http://localhost:18081` was still serving the pre-fix backend binary during this rollout, so a real-machine `ae-cli discover --dry-run` against that endpoint continued to return the old `/api/v1/providers` `500` until that backend is restarted with the new code.

## 2026-05-29 Follow-up: Codex App-Only Detection

- [x] Reproduce the app-only detection gap with a focused failing unit test.
- [x] Implement Codex app-bundle fallback detection while keeping Claude and Gemini PATH-only.
- [x] Update current-contract docs for the new detection behavior.
- [x] Run `cd ae-cli && go test ./...`.
- [x] Build a test binary and rerun `bash ae-cli/test/discover-e2e.sh <built-binary>` with app-only Codex coverage.

## 2026-07-18 Follow-up: Explicit Tool Selection and ChatGPT App Detection

### Task 1: Add explicit `--tool` selection at the command boundary

**Files:**
- Modify: `ae-cli/cmd/discover.go`
- Test: `ae-cli/cmd/discover_test.go`

**Interfaces:**
- Consumes: `defaultDiscoverToolNames`, `discoverInstalledTools`, and `toolconfig.InstalledTool`.
- Produces: `resolveDiscoverTools(explicit []string) ([]toolconfig.InstalledTool, error)` and a Cobra string-slice `--tool` flag.

- [ ] **Step 1: Add focused failing tests for explicit selection, validation, deduplication, and default detection**

Add `reflect` to the imports in `ae-cli/cmd/discover_test.go`, then add:

```go
func TestResolveDiscoverToolsUsesExplicitSelection(t *testing.T) {
	oldLister := discoverInstalledTools
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		t.Fatal("explicit selection must bypass installation detection")
		return nil, nil
	}
	t.Cleanup(func() { discoverInstalledTools = oldLister })

	got, err := resolveDiscoverTools([]string{"claude", "codex", "claude"})
	if err != nil {
		t.Fatalf("resolveDiscoverTools: %v", err)
	}
	want := []toolconfig.InstalledTool{{Name: "claude"}, {Name: "codex"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}

func TestResolveDiscoverToolsRejectsUnsupportedOrBlankTools(t *testing.T) {
	for _, explicit := range [][]string{{"cursor"}, {" "}} {
		_, err := resolveDiscoverTools(explicit)
		if err == nil || !strings.Contains(err.Error(), "supported tools: codex, claude, gemini") {
			t.Fatalf("resolveDiscoverTools(%q) error = %v", explicit, err)
		}
	}
}

func TestResolveDiscoverToolsUsesDetectionByDefault(t *testing.T) {
	oldLister := discoverInstalledTools
	want := []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}}
	discoverInstalledTools = func(names []string) ([]toolconfig.InstalledTool, error) {
		if !reflect.DeepEqual(names, defaultDiscoverToolNames) {
			t.Fatalf("tool names = %v, want %v", names, defaultDiscoverToolNames)
		}
		return want, nil
	}
	t.Cleanup(func() { discoverInstalledTools = oldLister })

	got, err := resolveDiscoverTools(nil)
	if err != nil {
		t.Fatalf("resolveDiscoverTools: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd ae-cli && go test ./cmd -run 'TestResolveDiscoverTools' -count=1 -v
```

Expected: build failure because `resolveDiscoverTools` is undefined.

- [ ] **Step 3: Implement the minimal explicit-selection helper and Cobra flag**

In `ae-cli/cmd/discover.go`, import `strings`, add `discoverToolNames []string`, and register:

```go
discoverCmd.Flags().StringSliceVar(&discoverToolNames, "tool", nil, "tool to configure even when not detected (codex, claude, gemini); may be repeated")
```

Replace the direct `discoverInstalledTools(defaultDiscoverToolNames)` call with:

```go
tools, err := resolveDiscoverTools(discoverToolNames)
```

Add:

```go
func resolveDiscoverTools(explicit []string) ([]toolconfig.InstalledTool, error) {
	if len(explicit) == 0 {
		return discoverInstalledTools(defaultDiscoverToolNames)
	}

	supported := make(map[string]struct{}, len(defaultDiscoverToolNames))
	for _, name := range defaultDiscoverToolNames {
		supported[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(explicit))
	tools := make([]toolconfig.InstalledTool, 0, len(explicit))
	for _, raw := range explicit {
		name := strings.TrimSpace(raw)
		if _, ok := supported[name]; !ok {
			return nil, fmt.Errorf("unsupported tool %q; supported tools: %s", raw, strings.Join(defaultDiscoverToolNames, ", "))
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tools = append(tools, toolconfig.InstalledTool{Name: name})
	}
	return tools, nil
}
```

Reset `discoverToolNames` to `nil` in every existing `runDiscover` test setup and restore its previous value in cleanup so Cobra global state cannot leak between tests.

- [ ] **Step 4: Run focused command tests and verify GREEN**

Run:

```bash
cd ae-cli && go test ./cmd -run 'TestResolveDiscoverTools|TestDiscoverCommand' -count=1 -v
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit the command behavior**

```bash
git add ae-cli/cmd/discover.go ae-cli/cmd/discover_test.go docs/superpowers/plans/2026-05-19-ae-cli-deterministic-tool-configuration.md
git commit -m "feat(ae-cli): allow explicit discover tool selection"
```

### Task 2: Recognize `ChatGPT.app` while retaining legacy Codex detection

**Files:**
- Modify: `ae-cli/internal/toolconfig/toolconfig.go`
- Test: `ae-cli/internal/toolconfig/toolconfig_test.go`

**Interfaces:**
- Consumes: `codexAppBundleCandidates() []string` and `firstExistingDir(paths []string)`.
- Produces: ordered ChatGPT-first app candidates without changing `DetectInstalledTools` or `InstalledTool` signatures.

- [ ] **Step 1: Add failing ChatGPT-only and deterministic candidate-order tests**

Replace the existing `TestDetectInstalledToolsFindsCodexAppWithoutCLI` with a pure candidate-order regression. This avoids allowing a real `/Applications/ChatGPT.app` on the test machine to change the expected legacy result:

```go
func TestCodexAppBundleCandidatesPreferChatGPTAndRetainLegacyCodex(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got := codexAppBundleCandidates()
	want := []string{
		filepath.Join(tmpHome, "Applications", "ChatGPT.app"),
		"/Applications/ChatGPT.app",
		filepath.Join(tmpHome, "Applications", "Codex.app"),
		"/Applications/Codex.app",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}
```

Then add the real user-level ChatGPT app detection tests:

```go
func TestDetectInstalledToolsFindsChatGPTAppWithoutCLI(t *testing.T) {
	tmpBin := t.TempDir()
	t.Setenv("PATH", tmpBin)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	appPath := filepath.Join(tmpHome, "Applications", "ChatGPT.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(ChatGPT.app): %v", err)
	}

	got, err := DetectInstalledTools([]string{"codex", "claude"})
	if err != nil {
		t.Fatalf("DetectInstalledTools: %v", err)
	}
	if len(got) != 1 || got[0].Name != "codex" || got[0].Path != appPath {
		t.Fatalf("tools = %+v, want codex app at %s", got, appPath)
	}
}

func TestDetectInstalledToolsPrefersChatGPTAppOverLegacyCodexApp(t *testing.T) {
	tmpBin := t.TempDir()
	t.Setenv("PATH", tmpBin)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chatGPTPath := filepath.Join(tmpHome, "Applications", "ChatGPT.app")
	legacyPath := filepath.Join(tmpHome, "Applications", "Codex.app")
	for _, path := range []string{chatGPTPath, legacyPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", path, err)
		}
	}

	got, err := DetectInstalledTools([]string{"codex"})
	if err != nil {
		t.Fatalf("DetectInstalledTools: %v", err)
	}
	if len(got) != 1 || got[0].Path != chatGPTPath {
		t.Fatalf("tools = %+v, want ChatGPT app at %s", got, chatGPTPath)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd ae-cli && go test ./internal/toolconfig -run 'TestCodexAppBundleCandidatesPreferChatGPTAndRetainLegacyCodex|TestDetectInstalledToolsFindsChatGPTAppWithoutCLI|TestDetectInstalledToolsPrefersChatGPTAppOverLegacyCodexApp' -count=1 -v
```

Expected: the selected tests fail because `ChatGPT.app` is absent from the candidate list.

- [ ] **Step 3: Add ordered ChatGPT and legacy Codex candidates**

Replace `codexAppBundleCandidates` with:

```go
func codexAppBundleCandidates() []string {
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, "Applications", "ChatGPT.app"))
	}
	candidates = append(candidates, "/Applications/ChatGPT.app")
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, "Applications", "Codex.app"))
	}
	candidates = append(candidates, "/Applications/Codex.app")
	return candidates
}
```

- [ ] **Step 4: Run all toolconfig tests and verify GREEN**

Run:

```bash
cd ae-cli && go test ./internal/toolconfig -count=1 -v
```

Expected: all toolconfig tests pass; the candidate-order test proves both legacy paths remain present without depending on real system app installations.

- [ ] **Step 5: Commit app detection**

```bash
git add ae-cli/internal/toolconfig/toolconfig.go ae-cli/internal/toolconfig/toolconfig_test.go docs/superpowers/plans/2026-05-19-ae-cli-deterministic-tool-configuration.md
git commit -m "fix(ae-cli): detect Codex through ChatGPT app"
```

### Task 3: Verify the CLI contract end to end and align current documentation

**Files:**
- Modify: `ae-cli/test/discover-e2e.sh`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md`
- Modify: `docs/superpowers/plans/2026-05-19-ae-cli-deterministic-tool-configuration.md`

**Interfaces:**
- Consumes: the built `ae-cli` binary, mock `/api/v1/user/providers`, and the Task 1/2 CLI contracts.
- Produces: executable proof for ChatGPT app-only detection and explicit Codex configuration with no detectable Codex binary or app.

- [ ] **Step 1: Extend the mock E2E fixture to exercise both paths**

In `ae-cli/test/discover-e2e.sh`:

- Replace `${TMP_HOME}/Applications/Codex.app` with `${TMP_HOME}/Applications/ChatGPT.app` so the existing default-discovery run proves the renamed app path.
- Add `TMP_EXPLICIT_HOME="$(mktemp -d)"` and include it in `cleanup`.
- After the current assertions, copy only `.ae-cli/token.json` into the explicit home and run:

```bash
EXPLICIT_OUTPUT_FILE="${TMP_EXPLICIT_HOME}/discover.out"
mkdir -p "${TMP_EXPLICIT_HOME}/.ae-cli"
cp "${TMP_HOME}/.ae-cli/token.json" "${TMP_EXPLICIT_HOME}/.ae-cli/token.json"
HOME="${TMP_EXPLICIT_HOME}" PATH="${TMP_BIN}" SHELL=/bin/zsh "${BIN_PATH}" discover --tool codex > "${EXPLICIT_OUTPUT_FILE}"

test -f "${TMP_EXPLICIT_HOME}/.codex/config.toml"
test -f "${TMP_EXPLICIT_HOME}/.codex/auth.json"
test ! -e "${TMP_EXPLICIT_HOME}/.claude/settings.json"
test ! -e "${TMP_EXPLICIT_HOME}/.ae-cli/env.sh"
grep -F "Configured provider relay.main for 1 tool(s):" "${EXPLICIT_OUTPUT_FILE}" >/dev/null
grep -F "  - codex" "${EXPLICIT_OUTPUT_FILE}" >/dev/null
```

- [ ] **Step 2: Build a temporary CLI and run the E2E script**

Run:

```bash
cd ae-cli && go build -o /tmp/ae-cli-discover-followup . && bash test/discover-e2e.sh /tmp/ae-cli-discover-followup
```

Expected: exit code 0 with both default ChatGPT app detection and explicit `--tool codex` assertions passing.

- [ ] **Step 3: Update current architecture and contract status**

In `docs/architecture.md`, update the `ae-cli discover` runtime bullets to state that:

```text
Automatic detection recognizes the Codex CLI, ChatGPT.app, and legacy Codex.app. Repeated --tool values explicitly select supported tools and bypass installation detection, while platform credential matching remains mandatory.
```

In `docs/superpowers/specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md`:

- Change the status back to `Implemented current contract`.
- Update the Overview and Tool detection sections to describe `ChatGPT.app` and `--tool` as current behavior.
- Change the 2026-07-18 follow-up introduction from approved/pending language to implemented language.

- [ ] **Step 4: Run full CLI verification and diff hygiene**

Run:

```bash
cd ae-cli && go test ./...
cd .. && git diff --check
```

Expected: all ae-cli packages pass and `git diff --check` prints no errors.

- [ ] **Step 5: Confirm the built help surface**

Run:

```bash
/tmp/ae-cli-discover-followup discover --help
```

Expected: output contains `--tool strings` and documents supported tools plus repeatability.

- [ ] **Step 6: Mark the follow-up complete and commit verification/docs**

After Steps 1-5 actually pass, change the plan Status to state that the 2026-07-18 follow-up is complete and check every completed step above. Then run:

```bash
git add ae-cli/test/discover-e2e.sh docs/architecture.md docs/superpowers/specs/2026-05-19-ae-cli-deterministic-tool-configuration-design.md docs/superpowers/plans/2026-05-19-ae-cli-deterministic-tool-configuration.md
git commit -m "test(ae-cli): verify explicit discover configuration"
```
