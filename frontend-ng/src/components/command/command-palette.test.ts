import { describe, expect, test } from 'vitest'
import { getCommandPaletteItems } from './command-palette'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'command-palette.tsx'), 'utf8')

describe('getCommandPaletteItems', () => {
  test('hides admin commands for non-admin users', () => {
    expect(getCommandPaletteItems(false).map((command) => command.id)).toEqual([
      'overview',
      'usage',
      'events',
      'repos',
      'user',
      'add-repository',
      'create-api-key',
      'export-usage-report',
      'toggle-theme'
    ])
  })

  test('includes admin commands for admin users', () => {
    expect(getCommandPaletteItems(true).map((command) => command.id)).toEqual([
      'overview',
      'usage',
      'events',
      'repos',
      'user',
      'add-repository',
      'create-api-key',
      'export-usage-report',
      'toggle-theme',
      'admin-users',
      'settings'
    ])
  })

  test('adds safe action and repository result groups from real repo data', () => {
    const items = getCommandPaletteItems(true, [
      {
        id: 42,
        full_name: 'example/repo',
        name: 'repo',
        clone_url: 'https://example.invalid/example/repo.git',
        repo_key: 'example/repo',
        default_branch: 'main',
        status: 'active',
        binding_state: 'bound',
        group_id: null,
        created_at: '2026-01-01T00:00:00Z',
        pr_summary: {
          total_prs: 0,
          ai_prs: 0,
          ai_share: 0
        }
      }
    ])

    expect(items.find((command) => command.id === 'toggle-theme')).toMatchObject({
      kind: 'action',
      groupKey: 'command.actions',
      labelKey: 'nav.toggleTheme'
    })
    expect(items.find((command) => command.id === 'add-repository')).toMatchObject({
      kind: 'action',
      to: '/repos',
      groupKey: 'command.actions',
      labelKey: 'command.addRepository'
    })
    expect(items.find((command) => command.id === 'create-api-key')).toMatchObject({
      kind: 'action',
      to: '/user',
      groupKey: 'command.actions',
      labelKey: 'command.createApiKey'
    })
    expect(items.find((command) => command.id === 'export-usage-report')).toMatchObject({
      kind: 'action',
      to: '/usage',
      groupKey: 'command.actions',
      labelKey: 'command.exportUsageReport'
    })
    expect(items.find((command) => command.id === 'repo-42')).toMatchObject({
      kind: 'repo',
      to: '/repos/$id',
      params: { id: '42' },
      groupKey: 'command.repositories',
      label: 'example/repo'
    })
  })

  test('uses shared command footer composition', () => {
    expect(source).toContain("from '@/components/primitives/command-footer'")
    expect(source).toContain('<CommandFooter>')
    expect(source).not.toContain("<div className='border-t border-border px-4 py-2.5 text-[11px] text-[var(--ink-4)]'>")
  })

  test('loads repository suggestions from the same-origin API client', () => {
    expect(source).toContain("import { api } from '@/lib/api'")
    expect(source).toContain("queryKey: ['command-palette', 'repos']")
    expect(source).toContain('api.repos.list({ page: 1, pageSize: 4 })')
    expect(source).not.toContain('AE.repoRows')
  })
})
