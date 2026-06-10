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
      'user'
    ])
  })

  test('includes admin commands for admin users', () => {
    expect(getCommandPaletteItems(true).map((command) => command.id)).toEqual([
      'overview',
      'usage',
      'events',
      'repos',
      'user',
      'admin-users',
      'settings'
    ])
  })

  test('uses shared command footer composition', () => {
    expect(source).toContain("from '@/components/primitives/command-footer'")
    expect(source).toContain('<CommandFooter>')
    expect(source).not.toContain("<div className='border-t border-border px-4 py-2.5 text-[11px] text-[var(--ink-4)]'>")
  })
})
