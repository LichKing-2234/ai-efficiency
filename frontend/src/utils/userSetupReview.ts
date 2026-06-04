export type InstallPlatform = 'shell' | 'windows'

const GITHUB_RELEASE_API_URL = 'https://api.github.com/repos/LichKing-2234/ai-efficiency/releases/latest'

type PlatformSource = {
  platform?: string
  userAgent?: string
  userAgentData?: {
    platform?: string
  }
}

export function detectInstallPlatform(source: PlatformSource = navigator): InstallPlatform {
  const platform = [
    source.userAgentData?.platform,
    source.platform,
    source.userAgent,
  ].filter(Boolean).join(' ')
  return /windows|win32|win64/i.test(platform) ? 'windows' : 'shell'
}

export function buildInstallCommand(origin: string) {
  return `curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=${origin} bash`
}

export function buildGithubConnectivityCommand() {
  return `curl -fsSI --connect-timeout 5 ${GITHUB_RELEASE_API_URL}`
}

export function buildWindowsInstallCommand(origin: string) {
  return `$env:AE_CLI_INSTALL_SERVER_URL = "${origin}"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex`
}

export function buildWindowsGithubConnectivityCommand() {
  return `iwr -UseB -Method Head ${GITHUB_RELEASE_API_URL}`
}

export function buildPreferredInstallCommand(origin: string, platform: InstallPlatform = detectInstallPlatform()) {
  return platform === 'windows' ? buildWindowsInstallCommand(origin) : buildInstallCommand(origin)
}

export function buildPreferredGithubConnectivityCommand(platform: InstallPlatform = detectInstallPlatform()) {
  return platform === 'windows' ? buildWindowsGithubConnectivityCommand() : buildGithubConnectivityCommand()
}

export function buildLoginCommand(origin: string) {
  return 'ae-cli login'
}

export function buildDeviceLoginCommand(origin: string) {
  return 'ae-cli login --device'
}

export function buildDiscoverCommand(origin: string, providerName: string) {
  return `ae-cli discover --provider ${providerName}`
}

export function buildHooksGlobalCommand() {
  return 'ae-cli hooks enable --global'
}

export function buildRepoInitCommand() {
  return 'ae-cli init'
}

export function buildDoctorCommand() {
  return 'ae-cli doctor'
}

export function buildSyncCommand() {
  return 'ae-cli sync'
}

export function buildHooksStatusUploadsCommand() {
  return 'ae-cli hooks status --uploads'
}
