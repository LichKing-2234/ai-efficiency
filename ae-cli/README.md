# ae-cli

## Install

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- v0.2.0
```

The installer:

- downloads the matching GitHub Release archive
- verifies `checksums.txt`
- installs `ae-cli` to `~/.local/bin/ae-cli`
- on first install, writes `~/.ae-cli/config.yaml` with the default backend URL
- when `AE_CLI_INSTALL_SERVER_URL` is set and a CLI config already exists, updates only `server.url` and preserves the rest of the file
- prints a warning if `~/.local/bin` is not on `PATH`

For non-interactive installs, preseed the backend URL:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=https://ae.example.com bash
```

If you already have a CLI config file and do not pass `AE_CLI_INSTALL_SERVER_URL`, the installer leaves it unchanged.

When `tools` are not configured explicitly, `ae-cli` auto-detects common local tool binaries from `PATH` (`claude`, `codex`, `kiro`).

## Verify

```bash
ae-cli version
```

Then run:

```bash
ae-cli login
```

To configure supported local AI tools from your current relay provider:

```bash
ae-cli discover
```

Use `ae-cli discover --dry-run` to preview the file changes without writing them, or `ae-cli discover --provider <name>` to override the primary provider.

Primary workflow after login:

```bash
ae-cli init
ae-cli sync
ae-cli doctor
```

Legacy `ae-cli start/stop/run/...` session commands are no longer included in the current binary. Use the sessionless workflow only.

## Windows

Windows PowerShell users can install the latest release with:

```powershell
iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex
```

To preseed a non-default backend URL:

```powershell
$env:AE_CLI_INSTALL_SERVER_URL = "https://ae.example.com"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex
```

The PowerShell installer follows the same config rule as the Bash installer: an explicit `AE_CLI_INSTALL_SERVER_URL` updates only `server.url` in an existing config.

## Relationship To Backend Deployment

- `ae-cli/install.sh` installs the developer CLI.
- `deploy/install.sh` installs the backend service for Linux systemd deployments.
