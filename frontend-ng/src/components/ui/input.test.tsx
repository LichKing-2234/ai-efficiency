import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Input } from './input'

describe('Input', () => {
  test('renders the reference auth/input shell with stable sizing and surface treatment', () => {
    const html = renderToStaticMarkup(<Input placeholder='Username or email' />)

    expect(html).toContain('data-slot="input"')
    expect(html).toContain('h-10')
    expect(html).toContain('rounded-[var(--r-md)]')
    expect(html).toContain('border-input')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('px-3.5')
    expect(html).toContain('text-[13px]')
    expect(html).toContain('shadow-none')
  })

  test('keeps the reference focus treatment and avoids legacy ring classes', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./input.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
    expect(source).not.toContain('focus-visible:ring-[3px] focus-visible:ring-ring/30')
    expect(source).not.toContain('bg-card')
  })
})
