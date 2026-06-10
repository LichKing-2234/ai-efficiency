import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repo-detail-page.tsx'), 'utf8')

describe('Repo detail page composition', () => {
  test('uses shared linked record items for pull request links', () => {
    expect(source).toContain("from '@/components/primitives/linked-record-list'")
    expect(source).toContain('<LinkedRecordItem')
    expect(source).toContain("variant='plain'")
    expect(source).not.toContain("<a className='flex min-w-0 items-center gap-2 font-semibold")
    expect(source).not.toContain("className='border-0 bg-transparent p-0 hover:bg-transparent'")
    expect(source).not.toContain("<span className='min-w-0'>")
  })

  test('uses the shared KPI grid utility for repository detail metrics', () => {
    expect(source).toContain("<div className='kpi-grid'>")
    expect(source).not.toContain("<div className='grid gap-4 sm:grid-cols-4'>")
  })

  test('uses shared filter rows for pull request range controls', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from '@/components/primitives/filter-row-title'")
    expect(source).toContain("<FilterRow className='text-sm'>")
    expect(source).toContain("<FilterRowTitle title={t('repoDetail.mergedIn')} variant='label' />")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2 text-sm'>")
    expect(source).not.toContain("<span className='text-muted-foreground'>{t('repoDetail.mergedIn')}</span>")
  })

  test('uses shared stacks for repair and expanded detail vertical rhythm', () => {
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).not.toContain("<div className='flex flex-col gap-3'>")
    expect(source).not.toContain("<div className='flex flex-col gap-4'>")
  })

  test('uses the shared inset panel flush variant for expanded pull request details', () => {
    expect(source).toContain('<InsetPanel flush>')
    expect(source).not.toContain("className='rounded-none border-x-0 border-t-0 p-4'")
  })

  test('uses the shared inset panel compact variant for sync status notes', () => {
    expect(source).toContain('<InsetPanel compact muted>')
    expect(source).not.toContain("className='px-3 py-2'")
  })

  test('uses shared status-with-reason rows for PR and snapshot usage states', () => {
    expect(source).toContain("from '@/components/primitives/status-with-reason'")
    expect(source.match(/<StatusWithReason/g)?.length).toBe(2)
    expect(source).not.toContain("<div className='flex flex-col gap-1'>")
    expect(source).not.toContain("<span className='flex min-w-0 flex-col gap-1'>")
  })

  test('uses shared data grid cells for PR and snapshot numeric metadata', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='tnum'>{compact((pr.usage_input_tokens")
    expect(source).not.toContain("className='tnum'>{number(pr.cycle_time_hours)}h")
    expect(source).not.toContain("className='tnum text-muted-foreground text-xs'")
    expect(source).not.toContain("className='mono min-w-0 truncate text-xs'")
    expect(source).not.toContain("className='tnum text-right'")
  })

  test('uses shared data grid header cells for snapshot numeric headers', () => {
    expect(source).toContain('DataGridHeaderCell')
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.input')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.output')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.cache')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.reasoning')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.credits')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.requests')}</span>")
  })

  test('uses shared data grid status rows for expanded PR loading and empty snapshot states', () => {
    expect(source).toContain('DataGridStatusRow')
    expect(source).not.toContain("className='py-4 text-center text-muted-foreground text-sm'")
    expect(source).not.toContain("className='justify-center py-6 text-center text-muted-foreground text-sm'")
  })
})
