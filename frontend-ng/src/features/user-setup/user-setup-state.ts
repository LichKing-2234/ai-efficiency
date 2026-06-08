import type { UserGroupCredentialSummary, UserProviderModel, UserProviderSummary, UserProviderTestRequest } from '@/lib/api/types'

export interface UserProviderSelection {
  providerId: number | null
  groupId: string | null
}

export function chooseDefaultSelection(providers: UserProviderSummary[], current?: UserProviderSelection): UserProviderSelection {
  if (providers.length === 0) return { providerId: null, groupId: null }
  const currentProvider = current?.providerId ? providers.find((provider) => provider.id === current.providerId) : null
  const provider = currentProvider ?? providers.find((item) => item.is_primary) ?? providers[0]
  const currentGroup = current?.groupId ? provider.groups.find((group) => group.group_id === current.groupId) : null
  return {
    providerId: provider.id,
    groupId: (currentGroup ?? provider.groups[0])?.group_id ?? null
  }
}

export function secretStateKey(providerId: number, groupId: string) {
  return `${providerId}:${groupId}`
}

export function visibleCredentialSecret(providerId: number | null | undefined, group: UserGroupCredentialSummary | null | undefined, sessionSecrets: Record<string, string>) {
  if (!group) return ''
  const sessionSecret = providerId ? sessionSecrets[secretStateKey(providerId, group.group_id)] : ''
  return sessionSecret || group.credential.key || ''
}

export function maskApiKey(key: string) {
  if (!key) return ''
  if (key.length <= 12) return `${key.slice(0, 4)}***`
  return `${key.slice(0, 6)}...${key.slice(-4)}`
}

export function modelLabel(model: UserProviderModel) {
  const displayName = model.display_name?.trim()
  if (!displayName || displayName === model.id) return model.id
  return `${displayName} (${model.id})`
}

export function buildProviderTestRequest(group: UserGroupCredentialSummary, model: string, prompt: string): UserProviderTestRequest {
  return {
    platform: group.platform,
    group_id: group.group_id,
    model: model.trim(),
    prompt: prompt.trim() || 'Hi'
  }
}

export function buildInstallCommand(origin: string) {
  return `curl -fsSL https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.sh | AE_CLI_INSTALL_SERVER_URL=${origin} bash`
}

export function buildWindowsInstallCommand(origin: string) {
  return `$env:AE_CLI_INSTALL_SERVER_URL = "${origin}"; iwr -UseB https://raw.githubusercontent.com/LichKing-2234/ai-efficiency/main/ae-cli/install.ps1 | iex`
}

export function buildDiscoverCommand(providerName: string) {
  return `ae-cli discover --provider ${providerName}`
}
