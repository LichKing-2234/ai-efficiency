import type { RepoUpdateRequest } from '@/lib/api/types'

export function buildRepoBindingPayload(providerId: string): RepoUpdateRequest {
  return providerId ? { scm_provider_id: Number(providerId) } : { clear_scm_provider: true }
}
