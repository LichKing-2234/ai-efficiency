import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'home-page.tsx'), 'utf8')

describe('Home page composition', () => {
  test('uses the shared card accent variant for the overview hero surface', () => {
    expect(source).toContain("<Card variant='accent'")
    expect(source).toContain("from '@/components/primitives/hero-content'")
    expect(source).toContain('<HeroContent')
    expect(source).not.toContain("grid-paper overflow-hidden border-[var(--ai-line)]")
    expect(source).not.toContain("bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between'>")
    expect(source).not.toContain("<p className='mt-2 text-muted-foreground text-sm'>")
  })

  test('uses shadcn empty primitives for the recent usage empty state', () => {
    expect(source).toContain("import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'")
    expect(source).toContain('<Empty>')
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
})
