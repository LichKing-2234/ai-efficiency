import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { HeaderOptionButtons } from './header-option-buttons'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'header-option-buttons.tsx'), 'utf8')

describe('HeaderOptionButtons', () => {
  test('renders compact wrapped header option buttons with selected state', () => {
    const html = renderToStaticMarkup(
      <HeaderOptionButtons
        ariaLabel='Provider groups'
        onChange={() => {}}
        options={[
          { value: 'g1', label: 'Default' },
          { value: 'g2', label: 'Staging' }
        ]}
        value='g2'
      />
    )

    expect(html).toContain('data-slot="header-option-buttons"')
    expect(html).toContain('aria-label="Provider groups"')
    expect(html).toContain('Default')
    expect(html).toContain('Staging')
    expect(html).toContain('rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-[3px]')
    expect(html).toContain('h-6.5')
    expect(html).toContain('bg-[var(--ink)] text-[var(--ink-on-accent)]')
    expect(html).toContain('bg-transparent text-[var(--ink-2)]')
  })

  test('sources layout from the shared action group and shared button shell', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/ui/button'")
    expect(source).toContain("className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-[3px]'")
    expect(source).toContain("variant='ghost'")
    expect(source).toContain("option.value === value")
    expect(source).not.toContain("variant={option.value === value ? 'default' : 'outline'}")
  })
})
