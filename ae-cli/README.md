# ae-cli

## Install

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash -s -- ae-cli/v0.2.0
```

The installer:

- downloads the matching GitHub Release archive
- requires full CLI release tags such as `ae-cli/v0.2.0` for pinned installs; platform `v*` tags are rejected
- verifies `checksums.txt`
- installs `ae-cli` to `~/.local/bin/ae-cli`
- on first install, writes `~/.ae-cli/config.yaml` with the default backend URL
- when `AE_CLI_INSTALL_SERVER_URL` is set and a CLI config already exists, updates only `server.url` and preserves the rest of the file
- refreshes AE-managed Git hook templates recorded under `~/.ae-cli`
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

## Update

Check whether a newer `ae-cli/v*` GitHub Release is available:

```bash
ae-cli update check
```

Install the latest published release on the official user-level install path:

```bash
ae-cli update install
```

To reinstall the latest published release even when the current version already matches:

```bash
ae-cli update install --force
```

`ae-cli update upgrade` is accepted as an alias for `ae-cli update install`.

Update behavior:

- `update check` only reads GitHub Release metadata and does not require config, login, or backend access
- `update install` only upgrades the official managed path `~/.local/bin/ae-cli`
- CLI updates ignore platform `v*` releases; the platform repository latest release is reserved for backend/frontend/deploy updates
- the install step reuses the tagged official installer, so checksum verification, config preservation, and managed hook refresh stay aligned with `ae-cli/install.sh`
- if `ae-cli` is running from another path, the command fails with guidance to rerun the official installer instead of guessing how to overwrite that install

Windows currently supports `ae-cli update check`. To upgrade on Windows, rerun the PowerShell installer:

```powershell
iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex
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
ae-cli init --hooks none
ae-cli sync
ae-cli doctor
```

`ae-cli init` explicitly registers the current repository with the backend. It does not install Git hooks by default.

## Git Hooks

Install managed hooks once for all repositories on this machine:

```bash
ae-cli hooks enable --global
```

Install managed hooks only for the current repository:

```bash
ae-cli hooks enable --repo
```

Inspect or remove managed hook ownership:

```bash
ae-cli hooks status
ae-cli hooks disable --repo
ae-cli hooks disable --global
```

`ae-cli init` can also enable hooks explicitly:

```bash
ae-cli init --hooks repo --force
```

Ownership rules:

- `--force` overwrites the relevant `core.hooksPath` layer.
- AE-managed hooks do not chain or preserve previous hooks.
- Global scripts live in `~/.ae-cli/git-hooks`.
- Repo-local scripts live in the repository's canonical Git common directory under `ae-hooks`.
- Managed hook scripts resolve the official binary at `~/.local/bin/ae-cli`; `AE_CLI_BIN` is an advanced override for controlled debugging.
- Hook execution only uploads for backend-known reporting-enabled repositories. Unknown repositories fail open and do not create backend repo records.

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
