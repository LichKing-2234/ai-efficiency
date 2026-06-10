param(
  [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "LichKing-2234/ai-efficiency"
$DefaultServerUrl = "https://ai-efficiency.la3.agoralab.co"
$ServerUrlExplicit = Test-Path Env:AE_CLI_INSTALL_SERVER_URL
$ServerUrl = if ($env:AE_CLI_INSTALL_SERVER_URL) { $env:AE_CLI_INSTALL_SERVER_URL.Trim() } else { $DefaultServerUrl }
$CliReleaseTagPattern = "^ae-cli/v\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$"
$ReleaseApiUrl = if ($env:AE_CLI_INSTALL_RELEASE_API_URL) { $env:AE_CLI_INSTALL_RELEASE_API_URL } else { "https://api.github.com/repos/$Repo/releases?per_page=100" }
$ReleaseDownloadBase = if ($env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE) { $env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE.TrimEnd("/") } else { "https://github.com/$Repo/releases/download" }

if (-not $env:USERPROFILE) {
  throw "USERPROFILE must be set to determine the installation directory"
}

$InstallDir = Join-Path $env:USERPROFILE ".local\bin"
$TargetPath = Join-Path $InstallDir "ae-cli.exe"
$ConfigDir = Join-Path $env:USERPROFILE ".ae-cli"
$ConfigPath = Join-Path $ConfigDir "config.yaml"

function Write-GitHubReleaseProxyHelp {
  Write-Host "ae-cli downloads releases from GitHub Releases. This request failed before the installer could resolve or download the release."
  Write-Host "If your network cannot reach GitHub directly, configure a proxy and rerun, for example:"
  Write-Host '$env:HTTPS_PROXY = "http://127.0.0.1:7890"'
  Write-Host '$env:HTTP_PROXY = "http://127.0.0.1:7890"'
}

function Get-ReleaseVersion([string]$Tag) {
  $value = $Tag.Trim()
  if ($value.StartsWith("ae-cli/")) {
    $value = $value.Substring("ae-cli/".Length)
  }
  if ($value.StartsWith("v")) {
    $value = $value.Substring(1)
  }
  return $value
}

function Assert-CliReleaseTag([string]$Tag) {
  if ($Tag -notmatch $CliReleaseTagPattern) {
    throw "release tag must match ae-cli/vX.Y.Z or ae-cli/vX.Y.Z-prerelease: $Tag"
  }
}

function Get-NextReleasePage([string]$LinkHeader) {
  if (-not $LinkHeader) {
    return ""
  }
  foreach ($part in ($LinkHeader -split ",")) {
    if ($part -match '<([^>]+)>;\s*rel="next"') {
      return $Matches[1]
    }
  }
  return ""
}

function Get-LatestTag {
  $nextUrl = $ReleaseApiUrl
  $seenUrls = @{}

  while ($nextUrl) {
    if ($seenUrls.ContainsKey($nextUrl)) {
      throw "release pagination loop at $nextUrl"
    }
    $seenUrls[$nextUrl] = $true

    try {
      $response = Invoke-WebRequest -Uri $nextUrl -UseBasicParsing
      $releases = $response.Content | ConvertFrom-Json
    } catch {
      Write-GitHubReleaseProxyHelp
      throw
    }

    foreach ($release in @($releases)) {
      $tagName = [string]$release.tag_name
      if ($tagName -match $CliReleaseTagPattern) {
        return $tagName
      }
    }

    $linkHeader = [string]($response.Headers["Link"] -join ",")
    $nextUrl = Get-NextReleasePage $linkHeader
  }

  throw "failed to resolve ae-cli release tag"
}

function Test-ServerUrl([string]$Value) {
  return $Value -match "^https?://\S+$"
}

function Get-ExistingConfigPath {
  $yaml = Join-Path $ConfigDir "config.yaml"
  $yml = Join-Path $ConfigDir "config.yml"
  if (Test-Path -LiteralPath $yaml -PathType Leaf) { return $yaml }
  if (Test-Path -LiteralPath $yml -PathType Leaf) { return $yml }
  return ""
}

function Expand-CliArchive([string]$ArchivePath, [string]$Destination) {
  Expand-Archive -LiteralPath $ArchivePath -DestinationPath $Destination -Force
  $binary = Join-Path $Destination "ae-cli.exe"
  if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
    throw "release archive missing ae-cli.exe"
  }
  return $binary
}

function Set-CliServerUrl([string]$Path, [string]$Url) {
  $lines = if (Test-Path -LiteralPath $Path -PathType Leaf) { Get-Content -LiteralPath $Path } else { @() }
  $out = New-Object System.Collections.Generic.List[string]
  $inServer = $false
  $sawServer = $false
  $wroteUrl = $false

  foreach ($line in $lines) {
    if ($line -match "^server:\s*$") {
      if ($inServer -and -not $wroteUrl) {
        $out.Add("  url: `"$Url`"")
      }
      $inServer = $true
      $sawServer = $true
      $wroteUrl = $false
      $out.Add($line)
      continue
    }

    if ($inServer -and $line -match "^\S[^:]*:") {
      if (-not $wroteUrl) {
        $out.Add("  url: `"$Url`"")
      }
      $inServer = $false
    }

    if ($inServer -and $line -match "^(\s*)url:\s*") {
      $out.Add($Matches[1] + "url: `"$Url`"")
      $wroteUrl = $true
      continue
    }

    $out.Add($line)
  }

  if ($inServer -and -not $wroteUrl) {
    $out.Add("  url: `"$Url`"")
  }
  if (-not $sawServer) {
    if ($out.Count -gt 0) {
      $out.Add("")
    }
    $out.Add("server:")
    $out.Add("  url: `"$Url`"")
  }

  $out | Set-Content -LiteralPath $Path -Encoding UTF8
}

if ($ServerUrl -and -not (Test-ServerUrl $ServerUrl)) {
  throw "invalid AE_CLI_INSTALL_SERVER_URL: must start with http:// or https://"
}

$Tag = if ($Version) { $Version } else { Get-LatestTag }
Assert-CliReleaseTag $Tag
$ReleaseVersion = Get-ReleaseVersion $Tag
$Archive = "ae-cli_${ReleaseVersion}_windows_amd64.zip"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("ae-cli-install-" + [System.Guid]::NewGuid().ToString("N"))

Write-Host "Installing ae-cli $Tag..."

try {
  New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
  $ArchivePath = Join-Path $TempDir $Archive
  $ChecksumsPath = Join-Path $TempDir "checksums.txt"
  $ReleaseBase = "$ReleaseDownloadBase/$Tag"

  try {
    Invoke-WebRequest -Uri "$ReleaseBase/$Archive" -OutFile $ArchivePath -UseBasicParsing
    Invoke-WebRequest -Uri "$ReleaseBase/checksums.txt" -OutFile $ChecksumsPath -UseBasicParsing
  } catch {
    Write-GitHubReleaseProxyHelp
    throw
  }

  $Expected = (Get-Content -LiteralPath $ChecksumsPath | Where-Object { $_ -match "\s+$([regex]::Escape($Archive))$" } | Select-Object -First 1)
  if (-not $Expected) {
    throw "missing checksum for $Archive"
  }
  $ExpectedHash = ($Expected -split "\s+")[0].ToLowerInvariant()
  $ActualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArchivePath).Hash.ToLowerInvariant()
  if ($ExpectedHash -ne $ActualHash) {
    throw "checksum verification failed for $Archive"
  }

  $BinaryPath = Expand-CliArchive $ArchivePath $TempDir
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item -LiteralPath $BinaryPath -Destination $TargetPath -Force

  $ExistingConfig = Get-ExistingConfigPath
  if ($ExistingConfig) {
    if ($ServerUrlExplicit -and $ServerUrl) {
      Set-CliServerUrl $ExistingConfig $ServerUrl
      Write-Host "Updated CLI config at $ExistingConfig"
    } else {
      Write-Host "Using existing CLI config at $ExistingConfig"
    }
  } elseif ($ServerUrl) {
    New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
    @"
server:
  url: "$ServerUrl"
"@ | Set-Content -LiteralPath $ConfigPath -Encoding UTF8
    Write-Host "Wrote CLI config to $ConfigPath"
  }

  try {
    & $TargetPath hooks refresh-installations *> $null
  } catch {
    Write-Host "Warning: installed ae-cli but failed to refresh managed hook scripts."
  }

  Write-Host "Installed ae-cli $Tag to $TargetPath"

  $PathEntries = ($env:PATH -split ";") | ForEach-Object { $_.TrimEnd("\") }
  if ($PathEntries -notcontains $InstallDir.TrimEnd("\")) {
    Write-Host "Warning: $InstallDir is not in PATH."
    Write-Host "Add it to your user PATH before running ae-cli from a new terminal."
  }
} finally {
  if (Test-Path -LiteralPath $TempDir) {
    Remove-Item -LiteralPath $TempDir -Recurse -Force
  }
}
