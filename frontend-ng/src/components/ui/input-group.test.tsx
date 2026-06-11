import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InputGroup, InputGroupInput, InputGroupText } from './input-group'

describe('InputGroup', () => {
  test('keeps the shared inset control shell aligned with the reference input treatment', () => {
    const html = renderToStaticMarkup(
      <InputGroup>
        <InputGroupText>https://</InputGroupText>
        <InputGroupInput value='example.com' onChange={() => undefined} />
      </InputGroup>
    )

    expect(html).toContain('data-slot="input-group"')
    expect(html).toContain('h-10')
    expect(html).toContain('rounded-[var(--r-md)]')
    expect(html).toContain('bg-[var(--surface-inset)]')
    expect(html).toContain('hover:bg-[var(--surface-2)]')
    expect(html).toContain('text-[12px]')
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('px-3.5')
    expect(html).toContain('focus-visible:shadow-none')
  })

  test('avoids legacy ring-based focus styling', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./input-group.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('has-[[data-slot=input-group-control]:focus-visible]:border-ring')
    expect(source).not.toContain('has-[[data-slot=input-group-control]:focus-visible]:shadow-[var(--sh-focus)]')
    expect(source).not.toContain('ring-3')
    expect(source).not.toContain('text-sm text-muted-foreground')
  })
})
