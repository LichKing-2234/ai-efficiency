import type { ToolUsageEventRow } from '@/lib/api/types'
import { tokenTotal } from '@/lib/format'

export type HomeSetupInput = {
  connectedTools: number
  totalRepos?: number | null
  recentEvents: number
}

export function buildHomeSetupItems(input: HomeSetupInput) {
  return [
    { id: 'account', ready: true },
    { id: 'ai-access', ready: input.connectedTools > 0 },
    { id: 'repos', ready: (input.totalRepos ?? 0) > 0 },
    { id: 'usage', ready: input.recentEvents > 0 }
  ]
}

export function homeSetupProgress(input: HomeSetupInput) {
  const items = buildHomeSetupItems(input)
  const ready = items.filter((item) => item.ready).length
  return {
    ready,
    total: items.length,
    ratio: items.length ? ready / items.length : 0
  }
}

export function buildHomeActivitySummary(event: ToolUsageEventRow) {
  return {
    id: event.id,
    bound: event.binding_status === 'bound',
    credit: event.credit_usage,
    endedAt: event.observed_end_at,
    requests: event.request_count,
    title: event.repo_name || event.source_basename || event.tool_session_id,
    tokens: tokenTotal(event),
    tool: event.tool
  }
}
