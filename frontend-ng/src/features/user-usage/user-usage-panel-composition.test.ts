import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-usage-panel.tsx'), 'utf8')

describe('User usage panel composition', () => {
  test('uses shared row primitives for usage range and refresh controls', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain('<FilterRow')
    expect(source).toContain('<ActionGroup')
    expect(source).not.toContain("<div className='flex flex-wrap items-center justify-between gap-3'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
  })

  test('matches the reference usage analytics card structure', () => {
    const tokenTrendIndex = source.indexOf("title={t('usageDashboard.tokenTrend')}")
    const modelDistributionIndex = source.indexOf("title={t('usageDashboard.modelDistribution')}")
    const costByModelIndex = source.indexOf("title={t('usageDashboard.costByModel')}")
    const firstSplitAfterTrend = source.indexOf("<div className='split-2'>", tokenTrendIndex)

    expect(tokenTrendIndex).toBeGreaterThan(0)
    expect(modelDistributionIndex).toBeGreaterThan(tokenTrendIndex)
    expect(costByModelIndex).toBeGreaterThan(modelDistributionIndex)
    expect(firstSplitAfterTrend).toBeGreaterThan(tokenTrendIndex)
    expect(modelDistributionIndex).toBeGreaterThan(firstSplitAfterTrend)
    expect(costByModelIndex).toBeGreaterThan(firstSplitAfterTrend)
    expect(source).not.toContain("<div className='px-[18px] pb-4'>")
    expect(source).not.toContain("<div className='px-[18px] pb-[18px]'>")
  })
})
