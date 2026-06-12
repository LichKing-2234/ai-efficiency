import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AppBrand } from './app-brand'

describe('AppBrand', () => {
  test('renders the shared brand mark, title, and mono subtitle', () => {
    const html = renderToStaticMarkup(
      <AppBrand mark='AE' subtitle='console · ng' title='AI Efficiency' />
    )

    expect(html).toContain('data-slot="app-brand"')
    expect(html).toContain('data-slot="app-brand-mark"')
    expect(html).toContain('data-slot="app-brand-title"')
    expect(html).toContain('data-slot="app-brand-subtitle"')
    expect(html).toContain('AI Efficiency')
    expect(html).toContain('console · ng')
    expect(html).toContain('bg-[linear-gradient(135deg,var(--ai-bright),var(--ai-deep))]')
  })

  test('supports compact auth-shell sizing without duplicating sidebar brand markup', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./app-brand.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("data-slot='app-brand'")
    expect(source).toContain("compact && 'gap-[10px]'")
    expect(source).toContain("compact ? 'size-8 rounded-[8px] text-[15px]' : 'size-7 rounded-[var(--r-sm)] text-xs'")
    expect(source).toContain("compact ? 'text-[13.5px] leading-[1.05] font-[650]' : 'text-[13.5px]'")
  })
})
