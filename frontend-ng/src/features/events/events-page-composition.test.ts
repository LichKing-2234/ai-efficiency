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

  test('uses shared filter rows for filter controls and detail badges', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain('<FilterRow>')
    expect(source).toContain("<FilterRow align='start'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<div className='flex flex-wrap gap-2'>")
  })

  test('uses the shared slide-over stack for event detail sections', () => {
    expect(source).toContain("from '@/components/primitives/slide-over-stack'")
    expect(source).toContain('<SlideOverStack>')
    expect(source).not.toContain("<div className='flex flex-col gap-[18px]'>")
  })

  test('uses shared record metadata for dense secondary row labels', () => {
    expect(source).toContain("from '@/components/primitives/record-meta'")
    expect(source).toContain('<RecordMeta>')
    expect(source).not.toContain("<span className='mono block truncate text-[11px] text-[var(--ink-4)]'>")
  })

  test('uses shared data grid cells for dense numeric and datetime columns', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='tnum text-right text-[var(--ink-2)]'")
    expect(source).not.toContain("className='text-right text-[var(--ink-3)] text-xs'")
  })
})
