import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'select.tsx'), 'utf8')

describe('Select', () => {
  test('keeps the trigger aligned with the reference input shell sizing and focus treatment', () => {
    expect(source).toContain('data-slot="select-trigger"')
    expect(source).toContain('data-[size=default]:h-10')
    expect(source).toContain('border-input')
    expect(source).toContain('bg-[var(--surface-inset)]')
    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('pl-3.5')
    expect(source).toContain('pr-3')
    expect(source).toContain('text-[13px]')
    expect(source).toContain('shadow-none')
    expect(source).toContain('focus-visible:border-ring')
    expect(source).not.toContain('focus-visible:shadow-[var(--sh-focus)]')
  })

  test('keeps menu content and items aligned with the tighter reference menu density', () => {
    expect(source).toContain('min-w-36')
    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('border border-[var(--line-strong)]')
    expect(source).toContain('bg-popover')
    expect(source).toContain('p-[5px]')
    expect(source).toContain('h-[34px]')
    expect(source).toContain('px-[10px]')
    expect(source).toContain('text-[13px]')
    expect(source).toContain('text-[var(--ink-2)]')
    expect(source).toContain('focus:bg-[var(--surface-inset)]')
    expect(source).toContain('focus:text-[var(--ink)]')
    expect(source).not.toContain('ring-1 ring-foreground/10')
    expect(source).not.toContain('text-sm')
  })

  test('avoids legacy transparent trigger chrome and ring utility focus styles', () => {
    expect(source).not.toContain('bg-transparent')
    expect(source).not.toContain('focus-visible:ring-3 focus-visible:ring-ring/50')
    expect(source).not.toContain('bg-card')
  })
})
