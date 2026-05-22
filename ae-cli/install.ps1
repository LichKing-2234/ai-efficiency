param(
  [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "LichKing-2234/ai-efficiency"
$DefaultServerUrl = "https://ai-efficiency.la3.agoralab.co"
$ServerUrlExplicit = Test-Path Env:AE_CLI_INSTALL_SERVER_URL
$ServerUrl = if ($env:AE_CLI_INSTALL_SERVER_URL) { $env:AE_CLI_INSTALL_SERVER_URL.Trim() } else { $DefaultServerUrl }
$ReleaseApiUrl = if ($env:AE_CLI_INSTALL_RELEASE_API_URL) { $env:AE_CLI_INSTALL_RELEASE_API_URL } else { "https://api.github.com/repos/$Repo/releases/latest" }
$ReleaseDownloadBase = if ($env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE) { $env:AE_CLI_INSTALL_RELEASE_DOWNLOAD_BASE.TrimEnd("/") } else { "https://github.com/$Repo/releases/download" }

if (-not $env:USERPROFILE) {
  throw "USERPROFILE must be set to determine the installation directory"
}

$InstallDir = Join-Path $env:USERPROFILE ".local\bin"
$TargetPath = Join-Path $InstallDir "ae-cli.exe"
$ConfigDir = Join-Path $env:USERPROFILE ".ae-cli"
$ConfigPath = Join-Path $ConfigDir "config.yaml"

function Get-LatestTag {
  $release = Invoke-RestMethod -Uri $ReleaseApiUrl -UseBasicParsing
  if (-not $release.tag_name) {
    throw "failed to resolve release tag"
  }
  return [string]$release.tag_name
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
$ReleaseVersion = $Tag.TrimStart("v")
$Archive = "ae-cli_${ReleaseVersion}_windows_amd64.zip"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("ae-cli-install-" + [System.Guid]::NewGuid().ToString("N"))

Write-Host "Installing ae-cli $Tag..."

try {
  New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
  $ArchivePath = Join-Path $TempDir $Archive
  $ChecksumsPath = Join-Path $TempDir "checksums.txt"
  $ReleaseBase = "$ReleaseDownloadBase/$Tag"

  Invoke-WebRequest -Uri "$ReleaseBase/$Archive" -OutFile $ArchivePath -UseBasicParsing
  Invoke-WebRequest -Uri "$ReleaseBase/checksums.txt" -OutFile $ChecksumsPath -UseBasicParsing

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
