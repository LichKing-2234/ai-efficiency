import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'home-page.tsx'), 'utf8')

describe('Home page composition', () => {
  test('uses the shared card accent variant for the overview hero surface', () => {
    expect(source).toContain("<Card variant='accent'")
    expect(source).not.toContain("grid-paper overflow-hidden border-[var(--ai-line)]")
    expect(source).not.toContain("bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]")
  })

  test('uses shadcn empty primitives for the recent usage empty state', () => {
    expect(source).toContain("import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'")
    expect(source).toContain('<Empty>')
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{t('common.empty')}</div>")
  })
})
