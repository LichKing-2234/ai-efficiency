$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$RootDir = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$Installer = Join-Path $RootDir "ae-cli/install.ps1"
$InstallerSource = Get-Content -LiteralPath $Installer -Raw
if (-not $InstallerSource.Contains('update post-install')) {
  throw "Windows installer must invoke the newly installed binary post-install cleanup"
}
$TmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("ae-cli-install-ps1-test-" + [System.Guid]::NewGuid().ToString("N"))
$SiteRoot = Join-Path $TmpRoot "site"
$ServerProcess = $null

$LatestTag = "ae-cli/v0.2.0-preview.1"
$PlatformTag = "v0.1.0-preview.42"
$BridgeTag = "v0.2.0-cli.1"

function Get-ReleaseVersion([string]$Tag) {
  $value = $Tag
  if ($value.StartsWith("ae-cli/")) {
    $value = $value.Substring("ae-cli/".Length)
  }
  if ($value.StartsWith("v")) {
    $value = $value.Substring(1)
  }
  return $value
}

function Get-TestPort {
  $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse("127.0.0.1"), 0)
  $listener.Start()
  try {
    return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
  } finally {
    $listener.Stop()
  }
}

function New-CliArchive([string]$Tag) {
  $version = Get-ReleaseVersion $Tag
  $releaseDir = Join-Path (Join-Path $SiteRoot "releases") ($Tag -replace "/", [System.IO.Path]::DirectorySeparatorChar)
  $stageDir = Join-Path $TmpRoot ("stage-" + ($version -replace "[^0-9A-Za-z.-]", "-"))
  $archive = "ae-cli_${version}_windows_amd64.zip"
  $archivePath = Join-Path $releaseDir $archive

  New-Item -ItemType Directory -Force -Path $releaseDir, $stageDir | Out-Null
  Set-Content -LiteralPath (Join-Path $stageDir "ae-cli.exe") -Value "fake ae-cli $Tag" -Encoding ASCII

  Push-Location $stageDir
  try {
    Compress-Archive -Path "ae-cli.exe" -DestinationPath $archivePath -Force
  } finally {
    Pop-Location
  }

  $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
  Set-Content -LiteralPath (Join-Path $releaseDir "checksums.txt") -Value "$hash  $archive" -Encoding ASCII
}

function Write-FixtureServer {
  $serverScript = Join-Path $TmpRoot "fixture_server.py"
  @'
import functools
import http.server
import json
import socketserver
import sys

root, port, platform_tag, latest_tag = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=root, **kwargs)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/page1":
            body = json.dumps([{"tag_name": platform_tag}]).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Link", f"<http://127.0.0.1:{port}/page2>; rel=\"next\"")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if path == "/page2":
            body = json.dumps([{"tag_name": latest_tag}]).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        return super().do_GET()

with socketserver.TCPServer(("127.0.0.1", port), Handler) as httpd:
    httpd.serve_forever()
'@ | Set-Content -LiteralPath $serverScript -Encoding ASCII
  return $serverScript
}

function Start-FixtureServer {
  $port = Get-TestPort
  $serverScript = Write-FixtureServer
  $stdout = Join-Path $TmpRoot "server.out"
  $stderr = Join-Path $TmpRoot "server.err"
  $process = Start-Process -FilePath "python3" -ArgumentList @($serverScript, $SiteRoot, [string]$port, $PlatformTag, $LatestTag) -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
  $baseUrl = "http://127.0.0.1:$port"

  for ($i = 0; $i -lt 50; $i++) {
    try {
      Invoke-WebRequest -Uri "$baseUrl/page1" -UseBasicParsing | Out-Null
      return @{ Process = $process; BaseUrl = $baseUrl }
    } catch {
      Start-Sleep -Milliseconds 100
    }
  }

  throw "fixture server did not start; stderr: $(Get-Content -LiteralPath $stderr -Raw -ErrorAction SilentlyContinue)"
}

function Get-TargetPath([string]$HomeDir) {
  return (Join-Path (Join-Path $HomeDir ".local\bin") "ae-cli.exe")
}

function Invoke-TestInstall([string]$HomeDir, [string]$BaseUrl, [string]$Version = "") {
  New-Item -ItemType Directory -Force -Path $HomeDir | Out-Null

  $oldUserProfile = $env:USERPROFILE
  $oldReleaseApi = $env:AE_CLI_INSTALL_RELEASE_API_URL
  $oldDownloadBase = $env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE
  $oldServerUrl = $env:AE_CLI_INSTALL_SERVER_URL

  try {
    $env:USERPROFILE = $HomeDir
    $env:AE_CLI_INSTALL_RELEASE_API_URL = "$BaseUrl/page1"
    $env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE = "$BaseUrl/releases"
    $env:AE_CLI_INSTALL_SERVER_URL = "https://ae.example.com"

    if ($Version) {
      & $Installer $Version
    } else {
      & $Installer
    }
  } finally {
    $env:USERPROFILE = $oldUserProfile
    $env:AE_CLI_INSTALL_RELEASE_API_URL = $oldReleaseApi
    $env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE = $oldDownloadBase
    $env:AE_CLI_INSTALL_SERVER_URL = $oldServerUrl
  }
}

function Assert-InstalledTag([string]$HomeDir, [string]$Tag) {
  $target = Get-TargetPath $HomeDir
  if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
    throw "expected installed binary at $target"
  }
  $content = Get-Content -LiteralPath $target -Raw
  if (-not $content.Contains($Tag)) {
    throw "installed binary content did not include $Tag"
  }
}

try {
  New-Item -ItemType Directory -Force -Path $SiteRoot | Out-Null
  New-CliArchive $LatestTag
  New-CliArchive $BridgeTag

  $server = Start-FixtureServer
  $ServerProcess = $server.Process
  $baseUrl = $server.BaseUrl

  $latestHome = Join-Path $TmpRoot "home-latest"
  Invoke-TestInstall $latestHome $baseUrl
  Assert-InstalledTag $latestHome $LatestTag

  $configPath = Join-Path (Join-Path $latestHome ".ae-cli") "config.yaml"
  if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw "expected config at $configPath"
  }
  $config = Get-Content -LiteralPath $configPath -Raw
  if (-not $config.Contains('url: "https://ae.example.com"')) {
    throw "expected installer to write configured server URL"
  }

  $bridgeHome = Join-Path $TmpRoot "home-bridge"
  Invoke-TestInstall $bridgeHome $baseUrl $BridgeTag
  Assert-InstalledTag $bridgeHome $BridgeTag

  $badHome = Join-Path $TmpRoot "home-bad-tag"
  $badTagRejected = $false
  try {
    Invoke-TestInstall $badHome $baseUrl "v0.2.1-preview.1"
  } catch {
    $badTagRejected = $_.Exception.Message.Contains("release tag must match ae-cli/vX.Y.Z")
  }
  if (-not $badTagRejected) {
    throw "expected platform-style pinned tag to be rejected"
  }
  if (Test-Path -LiteralPath (Get-TargetPath $badHome)) {
    throw "invalid pinned tag should not install a binary"
  }
} finally {
  if ($ServerProcess -and -not $ServerProcess.HasExited) {
    Stop-Process -Id $ServerProcess.Id -Force
    $ServerProcess.WaitForExit()
  }
  if (Test-Path -LiteralPath $TmpRoot) {
    Remove-Item -LiteralPath $TmpRoot -Recurse -Force
  }
}
