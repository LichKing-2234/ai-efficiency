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
    expect(source).toContain("width='toolbar'")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<div className='flex flex-wrap gap-2'>")
    expect(source).not.toContain("className='min-w-[260px] flex-1'")
  })

  test('uses semantic field widths for secondary filters', () => {
    expect(source).toContain("width='datetime'")
    expect(source).toContain("width='wide'")
    expect(source).not.toContain("className='w-[220px]'")
    expect(source).not.toContain("className='w-72'")
  })

  test('uses the shared slide-over stack for event detail sections', () => {
    expect(source).toContain("from '@/components/primitives/slide-over-stack'")
    expect(source).toContain('<SlideOverStack>')
    expect(source).not.toContain("<div className='flex flex-col gap-[18px]'>")
  })

  test('keeps event row secondary metadata inside shared data grid cells', () => {
    expect(source).toContain('description={row.source_basename || row.tool_session_id}')
    expect(source).not.toContain("from '@/components/primitives/record-meta'")
    expect(source).not.toContain('<RecordMeta>')
    expect(source).not.toContain("<span className='mono block truncate text-[11px] text-[var(--ink-4)]'>")
  })

  test('uses shared data grid description cells for event repository metadata', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("<span className='min-w-0'>")
    expect(source).not.toContain("<span className='block truncate font-medium text-foreground text-sm'>{row.repo_name || t('events.unlinked')}</span>")
  })

  test('uses shared data grid cells for dense numeric and datetime columns', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='tnum text-right text-[var(--ink-2)]'")
    expect(source).not.toContain("className='text-right text-[var(--ink-3)] text-xs'")
    expect(source).not.toContain("className='tnum text-right font-semibold text-foreground'")
    expect(source).toContain("<DataGridCell align='right' emphasis numeric>")
  })

  test('uses shared data grid header cells for aligned table headers', () => {
    expect(source).toContain('DataGridHeaderCell')
    expect(source).not.toContain("<span className='text-right'>{t('events.requests')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('events.credit')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('events.ended')}</span>")
  })

  test('uses the shared data grid status row for empty results', () => {
    expect(source).toContain('DataGridStatusRow')
    expect(source).not.toContain("className='px-6 py-10 text-center text-muted-foreground text-sm'")
  })

  test('uses shared primitives for pagination metadata and empty detail sections', () => {
    expect(source).toContain("import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'")
    expect(source).toContain("<Empty size='compact'>")
    expect(source).toContain("meta={t('common.pageCount'")
    expect(source).not.toContain("<Empty className='p-4'>")
    expect(source).not.toContain("<span className='text-muted-foreground text-xs'>{t('common.pageCount'")
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{t('events.noMatchedPrs')}</div>")
  })
})
