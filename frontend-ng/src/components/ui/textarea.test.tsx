import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Textarea } from './textarea'

describe('Textarea', () => {
  test('renders the reference multi-line control shell with shared surface treatment', () => {
    const html = renderToStaticMarkup(<Textarea placeholder='Write prompt' />)

    expect(html).toContain('data-slot="textarea"')
    expect(html).toContain('min-h-24')
    expect(html).toContain('border-input')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('rounded-[var(--r-md)]')
    expect(html).toContain('px-3.5')
    expect(html).toContain('py-2.5')
    expect(html).toContain('text-[13px]')
    expect(html).toContain('shadow-none')
  })

  test('keeps the reference focus treatment and avoids legacy ring classes', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./textarea.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
    expect(source).not.toContain('focus-visible:ring-[3px] focus-visible:ring-ring/30')
    expect(source).not.toContain('bg-card')
  })
})
