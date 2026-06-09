import { describe, expect, test } from 'vitest'
import { getCommandPaletteItems } from './command-palette'

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
})
