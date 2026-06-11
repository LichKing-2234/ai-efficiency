import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'dropdown-menu.tsx'), 'utf8')

describe('DropdownMenu', () => {
  test('keeps content chrome aligned with the reference locale menu shell', () => {
    expect(source).toContain('data-slot="dropdown-menu-content"')
    expect(source).toContain('min-w-36')
    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('border border-[var(--line-strong)]')
    expect(source).toContain('bg-popover')
    expect(source).toContain('p-[5px]')
    expect(source).not.toContain('ring-1 ring-foreground/10')
  })

  test('keeps item density on the tighter reference menu rhythm', () => {
    expect(source).toContain('h-[34px]')
    expect(source).toContain('gap-[9px]')
    expect(source).toContain('px-[10px]')
    expect(source).toContain('text-[13px]')
    expect(source).toContain('font-medium')
    expect(source).toContain('text-[var(--ink-2)]')
    expect(source).toContain('focus:bg-[var(--surface-inset)]')
    expect(source).toContain('focus:text-[var(--ink)]')
    expect(source).not.toContain('text-sm')
  })
})
