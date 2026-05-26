export function buildInstallCommand(origin: string) {
  return `curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=${origin} bash`
}

export function buildWindowsInstallCommand(origin: string) {
  return `$env:AE_CLI_INSTALL_SERVER_URL = "${origin}"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex`
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
