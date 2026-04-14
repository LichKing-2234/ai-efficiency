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
- on first install, prompts for the AI Efficiency backend URL and writes `~/.ae-cli/config.yaml`
- prints a warning if `~/.local/bin` is not on `PATH`

For non-interactive installs, preseed the backend URL:

```bash
AE_CLI_INSTALL_SERVER_URL=https://ae.example.com \
curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash
```

If you already have a CLI config file, the installer leaves it unchanged.

When `tools` are not configured explicitly, `ae-cli` auto-detects common local tool binaries from `PATH` (`claude`, `codex`, `kiro`).

## Verify

```bash
ae-cli version
```

Then run:

```bash
ae-cli login
```

## Windows

Windows users should download `ae-cli_<version>_<os>_<arch>.zip` from GitHub Releases and place `ae-cli.exe` on `PATH` manually.

## Relationship To Backend Deployment

- `ae-cli/install.sh` installs the developer CLI.
- `deploy/install.sh` installs the backend service for Linux systemd deployments.
