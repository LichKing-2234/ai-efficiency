import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Card } from './card'

describe('Card', () => {
  test('exposes the reference accent card variant through the shared primitive', () => {
    const html = renderToStaticMarkup(<Card variant='accent'>Hero</Card>)

    expect(html).toContain('grid-paper')
    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]')
  })

  test('keeps shared cards shadow-free', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("'rounded-[var(--r-lg)] border border-border bg-card text-card-foreground'")
    expect(source).not.toContain('shadow-[var(--sh-sm)]')
  })

  test('keeps shared card footer chrome neutral instead of muted-tinted', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className={cn('flex items-center border-t border-border p-[18px]'")
    expect(source).not.toContain('bg-muted/50')
  })

  test('keeps base title and description typography aligned with the reference card hierarchy', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className={cn('font-[650] text-[14px] leading-none'")
    expect(source).toContain("className={cn('text-[12px] text-[var(--ink-3)]'")
    expect(source).not.toContain("className={cn('font-semibold text-sm leading-snug'")
    expect(source).not.toContain("className={cn('text-muted-foreground text-xs'")
  })
})
