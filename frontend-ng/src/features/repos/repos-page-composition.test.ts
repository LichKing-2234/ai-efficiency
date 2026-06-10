import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repos-page.tsx'), 'utf8')

describe('Repos page composition', () => {
  test('uses shared filter and section primitives for the workbench shell', () => {
    expect(source).toContain("from '@/components/primitives/card-filter-bar'")
    expect(source).toContain("from '@/components/primitives/section-card-header'")
    expect(source).toContain('<CardFilterBar>')
    expect(source).toContain('<SectionCardHeader')
    expect(source).toContain('<ActionGroup push wrap>')
    expect(source).toContain('meta={`${number(total, locale)} ${t(')
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<ActionGroup wrap className='ml-auto'>")
    expect(source).not.toContain("<div className='border-border border-b px-3.5 py-3'>")
    expect(source).not.toContain("<div className='mb-3 flex items-center justify-between gap-2'>")
    expect(source).not.toContain("<div className='flex flex-col gap-2 border-b border-border px-5 py-4 md:flex-row md:items-center md:justify-between'>")
    expect(source).not.toContain("className='border-b border-border px-5 py-4'")
    expect(source).not.toContain("actions={<span className='text-muted-foreground text-sm'>{number(total, locale)} {t('repos.totalRepositories')}</span>}")
  })

  test('uses shared data grid record cells for clone URLs', () => {
    expect(source).toContain('DataGridRecordCell')
    expect(source).not.toContain("from '@/components/primitives/record-meta'")
    expect(source).not.toContain('<RecordMeta>')
    expect(source).not.toContain("<span className='min-w-0'>")
    expect(source).not.toContain("<span className='mono block truncate text-[11px] text-[var(--ink-4)]'>")
  })

  test('uses the shared workbench rail for provider scopes', () => {
    expect(source).toContain("from '@/components/primitives/workbench-rail'")
    expect(source).toContain('<WorkbenchRail')
    expect(source).toContain('<WorkbenchContent>')
    expect(source).toContain("scroll='workbench'")
    expect(source).not.toContain("<aside className='border-border bg-[var(--surface-2)] p-3 lg:border-r'>")
    expect(source).not.toContain("<section className='min-w-0'>")
    expect(source).not.toContain("className='max-h-[430px] overflow-y-auto'")
  })

  test('replaces provider and branch metadata columns with the reference AI PR ratio column', () => {
    expect(source).toContain('RatioMeter')
    expect(source).not.toContain("className='truncate text-[var(--ink-2)]'")
    expect(source).not.toContain("className='mono truncate text-xs'")
    expect(source).not.toContain("{t('repos.scmProvider')}</span>")
    expect(source).not.toContain("{t('repos.defaultBranch')}</span>")
  })

  test('uses real PR summary data for the reference AI PR ratio column', () => {
    expect(source).toContain("from '@/components/primitives/ratio-meter'")
    expect(source).toContain("{t('repos.aiPrs')}")
    expect(source).toContain('<RatioMeter')
    expect(source).toContain('repo.pr_summary?.ai_prs')
    expect(source).toContain('repo.pr_summary?.total_prs')
    expect(source).not.toContain("{t('repos.scmProvider')}</span>\\n        <span>{t('repos.defaultBranch')}</span>")
  })

  test('uses shared data grid header cells for aligned action columns', () => {
    expect(source).toContain('DataGridHeaderCell')
    expect(source).toContain("<DataGridHeaderCell align='right' />")
    expect(source).not.toContain("<span className='text-right' />")
  })

  test('uses shared data grid primary links for repository navigation', () => {
    expect(source).toContain('DataGridPrimaryLink')
    expect(source).not.toContain("className='block truncate font-semibold text-foreground text-sm hover:text-[var(--ai-deep)]'")
  })

  test('opens repository rows in a reference slide-over inspect panel', () => {
    expect(source).toContain("from '@/components/primitives/slide-over'")
    expect(source).toContain('DataGridRowAffordance')
    expect(source).toContain('<RepoInspectSlideOver')
    expect(source).toContain("onSelectRepo={(repo) => setSelectedRepo(repo)}")
    expect(source).toContain("<DataGridRow as='button'")
    expect(source).not.toContain('<DataGridPrimaryLink asChild>\\n              <Link')
    expect(source).not.toContain("data-slot='repo-row-actions'")
    expect(source).not.toContain("<ChevronRightIcon className='size-4' />")
  })

  test('uses shared status clusters inside the repository inspect panel', () => {
    expect(source).toContain("from '@/components/primitives/status-cluster'")
    expect(source).toContain('<StatusCluster>')
    expect(source).not.toContain("<div className='flex flex-wrap gap-2'>")
  })

  test('renders PR summary stats in the repository inspect panel', () => {
    expect(source).toContain("label={t('repos.totalPrs')}")
    expect(source).toContain("label={t('repos.aiPrs')}")
    expect(source).toContain("label={t('repos.aiPrShare')}")
    expect(source).toContain('percent(repo.pr_summary?.ai_share, locale)')
  })

  test('uses shared empty-state primitives for repository empty content', () => {
    expect(source).toContain("from '@/components/primitives/data-state'")
    expect(source).toContain('<EmptyState')
    expect(source).not.toContain("<CardContent className='p-8'>")
  })

  test('uses wrapped shadcn tabs without page-local tab list layout classes', () => {
    expect(source).toContain('<TabsList wrap>')
    expect(source).not.toContain("<TabsList className='h-auto flex-wrap justify-start'>")
  })

  test('uses the shared KPI grid primitive for repository health metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
  })

  test('shows the reference unbound warning banner from real inventory health', () => {
    expect(source).toContain("from '@/components/primitives/app-alert'")
    expect(source).toContain('health.unbound > 0 ? (')
    expect(source).toContain("<AppAlert")
    expect(source).toContain("tone='warning'")
    expect(source).toContain("title={t('repos.unboundWarningTitle', { count: number(health.unbound, locale) })}")
    expect(source).toContain("description={t('repos.unboundWarningDescription')}")
    expect(source).toContain("replaceSearch({ ...search, binding: 'unbound', provider: 'unbound', page: 1 })")
    expect(source).not.toContain("<div className='warn-soft")
  })
})
