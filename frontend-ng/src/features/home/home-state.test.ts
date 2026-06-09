import { describe, expect, test } from 'vitest'
import { buildHomeSetupItems, homeSetupProgress } from './home-state'

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
})
