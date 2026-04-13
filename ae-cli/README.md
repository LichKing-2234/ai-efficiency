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
- prints a warning if `~/.local/bin` is not on `PATH`

## Verify

```bash
ae-cli version
```

## Windows

Windows users should download `ae-cli_<version>_<os>_<arch>.zip` from GitHub Releases and place `ae-cli.exe` on `PATH` manually.

## Relationship To Backend Deployment

- `ae-cli/install.sh` installs the developer CLI.
- `deploy/install.sh` installs the backend service for Linux systemd deployments.
