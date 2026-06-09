import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'events-page.tsx'), 'utf8')

describe('Events page composition', () => {
  test('uses shared info tile grids for event detail metrics', () => {
    expect(source).toContain("import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'")
    expect(source).toContain('<InfoTileGrid columns={3}>')
    expect(source).not.toContain("<div className='grid grid-cols-3 gap-2'>")
  })
})
