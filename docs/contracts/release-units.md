# Release Units Contract

This contract describes the independent platform and CLI release lines. Read it
before changing release tags, GitHub workflows, GoReleaser outputs, installer/
updater discovery, platform images, or Helm rollout inputs.

## Release Intent

Release intent is expressed by tag namespace, not inferred from changed-file
path filters. GitHub does not apply path filters to tag pushes, and this mixed
Go/Vue repository does not add a monorepo release framework to infer intent.

| Release unit | Normal tag namespace | Published outputs | Excluded outputs |
| --- | --- | --- | --- |
| Platform | `vX.Y.Z` or `vX.Y.Z-prerelease` | Embedded frontend/backend artifacts, backend bundle, GHCR image, platform GitHub Release, Helm-consumed image tag | CLI artifacts |
| CLI | `ae-cli/vX.Y.Z` or `ae-cli/vX.Y.Z-prerelease` | Cross-platform CLI archives, checksum, CLI GitHub Release | Backend bundle, GHCR image, Helm rollout |

The frontend remains embedded in the backend platform delivery. It is not a
third independent release unit. A CLI-only change does not create a platform
tag or trigger a Helm rollout.

## Platform Release

The platform workflow accepts the normal `v*` namespace and explicitly excludes
the legacy `v*-cli.*` shape. It validates backend, frontend embedding, and
deploy assets; publishes the multi-architecture platform image and backend
bundle; and owns the repository-level latest release under normal operation.

Platform manual dispatch may create the requested platform tag when it does not
exist, then builds from that exact tag. Platform artifacts do not contain the
CLI.

## CLI Release

The CLI workflow accepts only semantic `ae-cli/v*` tags. Manual dispatch reruns
an already existing tag; it does not create one.

Before publication, the workflow:

1. Checks out the exact CLI tag.
2. Runs CLI and installer tests.
3. Builds only CLI archives and checksums through the CLI GoReleaser config.
4. Unpacks a Linux artifact and verifies that `ae-cli version` matches the tag
   after removing the `ae-cli/` namespace.
5. Creates the GitHub Release on the original namespaced tag with
   repository-latest disabled.

The workflow uses GoReleaser snapshot building plus explicit GitHub Release
creation so the OSS tool does not have to interpret the slash-prefixed tag as a
normal release version.

## CLI Discovery and Installation

The repository-level `/releases/latest` endpoint belongs to the platform line.
CLI installers and the self-updater list GitHub Releases in publication order,
select the first valid `ae-cli/v*` tag, and follow `Link: rel="next"` pagination
until one is found. A platform tag is never CLI latest.

Normal pinned installation accepts the `ae-cli/v*` namespace only. The
installer downloads assets and installer source from that exact tag. The
self-updater compares normalized CLI versions and invokes the installer only
from the official install location; Windows self-install remains a direction
to rerun the PowerShell installer.

## One-Time Bridge

`v0.2.0-cli.1` is the only legacy bridge exception. Its dedicated workflow:

- Accepts only that exact tag.
- Reuses the CLI-only build/test path.
- Publishes no backend bundle, image, or Helm input.
- Intentionally made the bridge repository latest so older CLIs that read only
  `/releases/latest` could install a version that understands `ae-cli/v*`.

The bridge is not a reusable CLI namespace and is not considered by current CLI
latest discovery. Any later normal platform release may and does reclaim
repository latest.

## Coordinated Changes

Platform and CLI versions are independent and do not imply compatibility by
sharing a number.

- A CLI that requires a new platform API states its minimum platform version.
- A platform change that breaks old CLIs states the minimum CLI or migration.
- When one side remains backward compatible, release that side first.
- When neither side is independently compatible, deliver a compatibility
  expansion first, then the platform and CLI releases in an explicit sequence.
- Capability or endpoint detection is preferred to broad version branching.

## Verification and Safety

- Platform tags cannot trigger the CLI workflow; CLI tags cannot trigger the
  platform workflow.
- The bridge tag cannot trigger platform image or backend-bundle publication.
- CLI releases explicitly remain outside repository latest and contain no
  image or deployment output.
- Artifact smoke checks verify the embedded version before publication.
- Release-workflow contract tests guard trigger, latest, artifact, installer,
  and bridge boundaries.
- Tag creation, GitHub Release publication, image publication, and Helm rollout
  remain explicit delivery actions. Documentation or CI validation alone does
  not authorize them.
