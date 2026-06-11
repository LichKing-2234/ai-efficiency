import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CodeBlock } from './code-block'

describe('CodeBlock', () => {
  test('renders shared mono content on the inset surface', () => {
    const html = renderToStaticMarkup(
      <CodeBlock ariaLabel='Raw payload'>
        {'{"ok":true}'}
      </CodeBlock>
    )

    expect(html).toContain('data-slot="code-block"')
    expect(html).toContain('Raw payload')
    expect(html).toContain('p-[14px]')
    expect(html).toContain('text-[12px]')
    expect(html).toContain('text-[var(--ink-2)]')
  })

  test('keeps code-block density inside the shared primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./code-block.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("'max-h-56 overflow-auto rounded-[var(--r-md)] bg-[var(--surface-inset)] p-[14px] text-[12px] leading-5 text-[var(--ink-2)]'")
    expect(source).not.toContain("'max-h-56 overflow-auto rounded-[var(--r-md)] bg-[var(--surface-inset)] p-3 text-xs leading-5'")
  })
})
