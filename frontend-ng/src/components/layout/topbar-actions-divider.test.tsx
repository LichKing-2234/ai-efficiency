import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarActionsDivider } from './topbar-actions-divider'

describe('TopbarActionsDivider', () => {
  test('renders the reference topbar separator with a stable slot', () => {
    const html = renderToStaticMarkup(<TopbarActionsDivider />)

    expect(html).toContain('data-slot="topbar-actions-divider"')
    expect(html).toContain('h-[22px]')
    expect(html).toContain('w-px')
    expect(html).toContain('bg-[var(--line)]')
  })

  test('keeps the divider on the desktop-only shell from the reference topbar', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-actions-divider.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("data-slot='topbar-actions-divider'")
    expect(source).toContain("hidden h-[22px] w-px bg-[var(--line)] min-[920px]:block")
    expect(source).not.toContain('h-6')
    expect(source).not.toContain('bg-border')
  })
})
