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
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from '@/components/primitives/primary-action-button'")
    expect(source).toContain("from '@/components/primitives/secondary-action-button'")
    expect(source).toContain("from '@/components/primitives/search-action-bar'")
    expect(source).toContain('<SearchActionBar')
    expect(source).toContain("<FilterRow className='min-w-0 flex-1'>")
    expect(source).toContain("<FilterRow align='start'>")
    expect(source).toContain("width='toolbar'")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<div className='flex flex-wrap gap-2'>")
    expect(source).not.toContain("className='min-w-[260px] flex-1'")
    expect(source).not.toContain("<CardFilterBar stacked>")
    expect(source).not.toContain('<Button onClick={applyCurrentFilters}>')
    expect(source).not.toContain("<Button variant='outline' onClick={clearTimeRange}>")
  })

  test('uses the shared leading-icon CTA button for event export', () => {
    expect(source).toContain("<ButtonWithIcon size='sm' disabled={rows.length === 0} variant='outline' icon={DownloadIcon} onClick={exportRows}>")
    expect(source).not.toContain("<Button disabled={rows.length === 0} variant='outline' onClick={exportRows}>")
  })

  test('uses the shared low-emphasis action button for admin clear-user controls', () => {
    expect(source).toContain("from '@/components/primitives/quiet-action-button'")
    expect(source).toContain("<QuietActionButton onClick={clearSelectedUser}>")
    expect(source).not.toContain("{isAdmin && appliedFilters.userId ? <Button variant='ghost' onClick={clearSelectedUser}>")
  })

  test('uses semantic field widths for secondary filters', () => {
    expect(source).toContain("width='datetime'")
    expect(source).toContain("width='wide'")
    expect(source).not.toContain("className='w-[220px]'")
    expect(source).not.toContain("className='w-72'")
  })

  test('uses the shared slide-over stack for event detail sections', () => {
    expect(source).toContain("from '@/components/primitives/slide-over-stack'")
    expect(source).toContain("from '@/components/primitives/detail-section'")
    expect(source).toContain('<SlideOverStack>')
    expect(source).toContain('<DetailSection')
    expect(source).not.toContain('<section>')
    expect(source).not.toContain("<div className='flex flex-col gap-[18px]'>")
  })

  test('keeps event row secondary metadata inside shared data grid cells', () => {
    expect(source).toContain("from '@/components/primitives/glyph-label-cell'")
    expect(source).toContain('<GlyphLabelCell')
    expect(source).toContain('description={row.source_basename || row.tool_session_id}')
    expect(source).not.toContain("from '@/components/primitives/record-meta'")
    expect(source).not.toContain('<RecordMeta>')
    expect(source).not.toContain("<span className='mono block truncate text-[11px] text-[var(--ink-4)]'>")
  })

  test('uses shared data grid description cells for event repository metadata', () => {
    expect(source).toContain('GlyphLabelCell')
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
    expect(source).toContain("from '@/components/primitives/framed-table-card'")
    expect(source).toContain('<FramedTableCard')
    expect(source).toContain("<PageEmpty title={t('events.noFilteredEvents')} />")
    expect(source).not.toContain('<FramedCard>')
    expect(source).not.toContain('<DataGridStatusRow')
    expect(source).not.toContain("className='px-6 py-10 text-center text-muted-foreground text-sm'")
  })

  test('uses shared primitives for pagination metadata and empty detail sections', () => {
    expect(source).toContain("from '@/components/primitives/page-empty'")
    expect(source).toContain("from '@/components/primitives/pager-nav-button'")
    expect(source).toContain("from '@/components/primitives/page-size-select'")
    expect(source).toContain("<PageEmpty title={t('events.noMatchedPrs')} />")
    expect(source).toContain("<PageSizeSelect")
    expect(source).toContain("ariaLabel={t('common.pageSizeControl')}")
    expect(source).toContain("labelMode='plain'")
    expect(source).toContain("<PagerNavButton direction='previous' onClick={previousPage} disabled={!pagination.canGoPrev}>")
    expect(source).toContain("<PagerNavButton direction='next' onClick={nextPage} disabled={!pagination.canGoNext}>")
    expect(source).toContain("meta={t('common.pageCount'")
    expect(source).toContain("t('command.exportUsageReport')")
    expect(source).not.toContain("from '@/components/ui/empty'")
    expect(source).not.toContain("<Empty className='p-4'>")
    expect(source).not.toContain("<span className='text-muted-foreground text-xs'>{t('common.pageCount'")
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{t('events.noMatchedPrs')}</div>")
    expect(source).not.toContain("<ToolbarSelect\n                ariaLabel={t('common.pageSizeControl')}")
    expect(source).not.toContain("<Button size='sm' variant='outline' onClick={previousPage} disabled={!pagination.canGoPrev}>")
    expect(source).not.toContain("<Button size='sm' variant='outline' onClick={nextPage} disabled={!pagination.canGoNext}>")
  })

  test('uses plain linked-record items for matched PR rows in the event detail drawer', () => {
    expect(source).toContain("variant='plain'")
    expect(source).not.toContain("<a key={i} className='pr-link'>")
  })

  test('uses the shared KPI grid primitive for event summary metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
  })
})
