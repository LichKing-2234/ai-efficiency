import type { VerifyReviewItem, VerifyReviewSummary } from '@/types'

export interface ReviewVerifyOutputInput {
  selectedProviderName: string
  versionOutput: string
  discoverOutput: string
  doctorOutput: string
}

function makeItem(status: VerifyReviewItem['status'], message: string): VerifyReviewItem {
  return { status, message }
}

function includesAny(haystack: string, needles: string[]) {
  const normalized = haystack.toLowerCase()
  return needles.some((needle) => normalized.includes(needle.toLowerCase()))
}

export function reviewVerifyOutput(input: ReviewVerifyOutputInput): VerifyReviewSummary {
  const versionOutput = input.versionOutput.trim()
  const discoverOutput = input.discoverOutput.trim()
  const doctorOutput = input.doctorOutput.trim()

  const version = versionOutput
    ? includesAny(versionOutput, ['ae-cli'])
      ? makeItem('looks_good', 'The output includes ae-cli and looks like a valid version response.')
      : makeItem('needs_attention', 'The version output does not look like an ae-cli version response.')
    : makeItem('cannot_determine', 'Paste the ae-cli version output to review this step.')

  const discoverTargets = ['~/.codex/config.toml', '~/.ae-cli/env.sh', '~/.claude/settings.json']
  const discoverHasProvider = discoverOutput.includes(input.selectedProviderName)
  const discoverHasTarget = discoverTargets.some((target) => discoverOutput.includes(target))
  const discover = discoverOutput
    ? discoverHasProvider && discoverHasTarget
      ? makeItem('looks_good', 'The dry-run output mentions the selected provider and at least one expected config target.')
      : makeItem('needs_attention', 'The dry-run output is missing the selected provider or expected config targets.')
    : makeItem('cannot_determine', 'Paste the discover --dry-run output to review this step.')

  const doctorFailureSignals = ['error', 'failed', 'unauthorized', 'forbidden']
  const doctorSuccessSignals = ['ok', 'ready', 'healthy', 'success']
  const doctor = doctorOutput
    ? includesAny(doctorOutput, doctorFailureSignals)
      ? makeItem('needs_attention', 'The doctor output contains failure keywords that need review.')
      : includesAny(doctorOutput, doctorSuccessSignals)
        ? makeItem('looks_good', 'The doctor output contains healthy status keywords.')
        : makeItem('cannot_determine', 'The doctor output did not contain clear success or failure signals.')
    : makeItem('cannot_determine', 'Paste the ae-cli doctor output to review this step.')

  return { version, discover, doctor }
}

export function buildInstallCommand(origin: string) {
  return `AE_CLI_INSTALL_SERVER_URL=${origin} curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | bash`
}

export function buildLoginCommand(origin: string) {
  return `ae-cli --server ${origin} login`
}

export function buildDeviceLoginCommand(origin: string) {
  return `ae-cli --server ${origin} login --device`
}

export function buildDiscoverCommand(origin: string, providerName: string) {
  return `ae-cli --server ${origin} discover --provider ${providerName}`
}
