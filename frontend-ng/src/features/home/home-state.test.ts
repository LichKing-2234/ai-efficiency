import { describe, expect, test } from 'vitest'
import { buildHomeActivitySummary, buildHomeSetupItems, homeSetupProgress } from './home-state'

describe('home setup progress', () => {
  test('counts account, AI access, repositories, and recent usage readiness', () => {
    expect(homeSetupProgress({ connectedTools: 2, totalRepos: 4, recentEvents: 3 })).toEqual({
      ready: 4,
      total: 4,
      ratio: 1
    })
  })

  test('keeps missing integrations visible in the setup checklist', () => {
    const items = buildHomeSetupItems({ connectedTools: 0, totalRepos: 0, recentEvents: 0 })

    expect(homeSetupProgress({ connectedTools: 0, totalRepos: 0, recentEvents: 0 })).toEqual({
      ready: 1,
      total: 4,
      ratio: 0.25
    })
    expect(items.map((item) => [item.id, item.ready])).toEqual([
      ['account', true],
      ['ai-access', false],
      ['repos', false],
      ['usage', false]
    ])
  })

  test('summarizes recent activity rows from backend events', () => {
    expect(buildHomeActivitySummary({
      id: 1,
      tool: 'claude',
      repo_id: 10,
      repo_name: '',
      tool_session_id: 'session-1',
      dedupe_key: 'event-1',
      observed_end_at: '2026-06-09T09:30:00Z',
      request_count: 3,
      input_tokens: 100,
      output_tokens: 50,
      cached_input_tokens: 20,
      reasoning_tokens: 10,
      credit_usage: 0.25,
      source_basename: 'workspace-a',
      binding_status: 'bound'
    })).toEqual({
      id: 1,
      bound: true,
      credit: 0.25,
      endedAt: '2026-06-09T09:30:00Z',
      requests: 3,
      title: 'workspace-a',
      tokens: 180,
      tool: 'claude'
    })
  })
})
