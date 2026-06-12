import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'home-page.tsx'), 'utf8')

describe('Home page composition', () => {
  test('uses the shared card accent variant for the overview hero surface', () => {
    expect(source).toContain("<Card variant='accent'")
    expect(source).toContain("from '@/components/primitives/hero-content'")
    expect(source).toContain("from '@/components/primitives/start-actions'")
    expect(source).toContain('<HeroContent')
    expect(source).not.toContain("grid-paper overflow-hidden border-[var(--ai-line)]")
    expect(source).not.toContain("bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between'>")
    expect(source).not.toContain("<p className='mt-2 text-muted-foreground text-sm'>")
  })

  test('uses the reference framed pulse strip shell under the hero', () => {
    expect(source).toContain("from '@/components/primitives/pulse-stat-grid'")
    expect(source).toContain('<PulseStatGrid>')
    expect(source).not.toContain("rounded-[var(--r-md)] border border-border bg-card md:grid-cols-3")
  })

  test('uses shared page empty states for overview empty sections', () => {
    expect(source).toContain("from '@/components/primitives/page-empty'")
    expect(source).toContain("<PageEmpty title={t('common.empty')} />")
    expect(source).not.toContain("from '@/components/ui/empty'")
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{t('common.empty')}</div>")
  })

  test('uses shared card content stack for the recent usage activity list', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("<CardContentStack gap='none'>")
    expect(source).not.toContain("<CardContent className='flex flex-col'>")
  })

  test('uses shared progress fraction typography for setup status counts', () => {
    expect(source).toContain("from '@/components/primitives/progress-fraction'")
    expect(source).toContain('<ProgressFraction ready={setupProgress.ready} total={setupProgress.total} />')
    expect(source).not.toContain("<span className='text-[11px] text-[var(--ink-3)]'>/{setupProgress.total}</span>")
  })

  test('uses the shared KPI grid primitive for overview metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
  })

  test('keeps the fourth KPI aligned to the reference avg response metric using real usage stats', () => {
    expect(source).toContain("label={t('home.avgResponse')}")
    expect(source).toContain("value={durationMs(usageStats?.average_duration_ms ?? 0, locale)}")
    expect(source).toContain("helper={t('usageDashboard.avgResponseHelper', {")
    expect(source).not.toContain("label={t('home.connectedTools')}")
  })

  test('uses reference overview sections instead of embedding the full usage analytics panel', () => {
    expect(source).toContain("from '@/components/primitives/charts'")
    expect(source).toContain('<BarsH')
    expect(source).toContain("className='split-2'")
    expect(source).not.toContain('<UserUsagePanel embedded />')
  })

  test('keeps the reference live-activity and top-models ending row without an extra usage snapshot card', () => {
    expect(source).toContain("title={t('home.liveActivity')}")
    expect(source).toContain("title={t('home.topModels')}")
    expect(source).not.toContain("title={t('home.usageSnapshot')}")
  })

  test('keeps the setup status card copy compact like the reference overview card', () => {
    expect(source).toContain("title={t('home.setupStatus')}")
    expect(source).not.toContain("description={setupProgress.ready === setupProgress.total ? t('home.statusReady') : t('home.statusWaitingEvents')}")
  })

  test('uses checklist rows directly for setup status items instead of a page-local wrapper', () => {
    expect(source).toContain("from '@/components/primitives/checklist-row'")
    expect(source).toContain("from '@/components/primitives/link-action'")
    expect(source).toContain('<ChecklistRow')
    expect(source).toContain("<LinkAction asChild>")
    expect(source).not.toContain('function StatusLine(')
    expect(source).not.toContain("action={connectedTools.size > 0 ? null : <Button asChild variant='link' size='sm'>")
  })

  test('keeps setup status values grounded in real counts instead of generic ready copy', () => {
    expect(source).toContain("value={connectedTools.size ? t('home.statusAiAccessCount', { count: number(connectedTools.size, locale) }) : t('home.statusAiAccessMissing')}")
    expect(source).toContain("value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusRepoCount', { count: number(dashboard.data?.total_repos ?? 0, locale) }) : t('home.statusNoRepo')}")
    expect(source).toContain("value={recentEvents.length ? t('home.statusEventCount', { count: number(recentEvents.length, locale) }) : t('home.statusWaitingEvents')}")
    expect(source).not.toContain("value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusConfigured') : t('home.statusNoRepo')}")
    expect(source).not.toContain("value={recentEvents.length ? t('home.statusEvents') : t('home.statusWaitingEvents')}")
  })

  test('uses shared overview pulse and comparison primitives instead of page-local helpers', () => {
    expect(source).toContain("from '@/components/primitives/compare-bar'")
    expect(source).toContain("from '@/components/primitives/pulse-stat'")
    expect(source).toContain('<PulseStat')
    expect(source).toContain('<CompareBar')
    expect(source).not.toContain('function PulseStat(')
    expect(source).not.toContain('function CompareBar(')
    expect(source).not.toContain("<div className='text-muted-foreground text-sm line-through'>{currency(totalStandardCost, locale)}</div>")
  })

  test('keeps the reference hero dual-action row for setup and export', () => {
    expect(source).toContain("from '@/components/primitives/start-actions'")
    expect(source).toContain('<StartActions>')
    expect(source).toContain("t('command.exportUsageReport')")
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("<ButtonWithIcon asChild icon={ArrowRightIcon} iconPosition='end'>")
    expect(source).toContain("<ButtonWithIcon size='sm' variant='outline' icon={DownloadIcon} onClick={exportOverviewReport}>")
    expect(source).not.toContain("action={(\n            <Button asChild>")
  })

  test('uses shared link actions for overview secondary navigation affordances', () => {
    expect(source).toContain("from '@/components/primitives/link-action'")
    expect(source).toContain("<LinkAction asChild>")
    expect(source).toContain("iconEnd={ArrowRightIcon}")
    expect(source).not.toContain("<Button asChild variant='link' size='sm'>")
  })
})
