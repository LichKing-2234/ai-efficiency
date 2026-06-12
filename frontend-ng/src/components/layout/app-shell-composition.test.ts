import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'app-shell.tsx'), 'utf8')

describe('AppShell composition', () => {
  test('uses shared topbar title typography', () => {
    expect(source).toContain("from './topbar-title'")
    expect(source).toContain('<TopbarTitle')
    expect(source).not.toContain("<div className='font-semibold text-[10.5px] text-[var(--ink-4)] uppercase tracking-[0.04em]'>")
    expect(source).not.toContain("<div className='truncate font-semibold text-[15px] leading-tight'>")
  })

  test('uses shared command trigger composition in the topbar', () => {
    expect(source).toContain("from './topbar-actions'")
    expect(source).toContain('<TopbarActions')
    expect(source).not.toContain("<kbd className='rounded border border-border bg-[var(--surface)] px-1.5 py-0.5 font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'>")
    expect(source).not.toContain("className='hidden min-w-48 justify-start gap-2 text-[var(--ink-3)] lg:inline-flex'")
  })

  test('uses shared live status composition in the topbar', () => {
    expect(source).toContain("from './topbar-actions'")
    expect(source).toContain('<TopbarActions')
    expect(source).not.toContain("<div className='hidden items-center gap-2 rounded-full border border-[var(--pos-line)] bg-[var(--pos-soft)] px-3 py-1 md:flex'>")
  })

  test('uses shared sidebar user summary composition in the footer', () => {
    expect(source).toContain("from './sidebar-user-summary'")
    expect(source).toContain('<SidebarUserSummary')
    expect(source).not.toContain("className='grid size-8 place-items-center rounded-full bg-[var(--ae-ai-soft)] font-semibold text-[var(--ae-ai-2)] text-xs'")
    expect(source).not.toContain("className='truncate text-[var(--ae-text-4)] text-xs'")
  })

  test('uses shared topbar actions composition for the right cluster', () => {
    expect(source).toContain("from './topbar-actions'")
    expect(source).toContain('<TopbarActions')
    expect(source).not.toContain("<div className='ml-auto flex items-center gap-2'>")
    expect(source).not.toContain("<ChevronDownIcon className='size-3 text-[var(--ink-4)]' />")
  })

  test('uses the reference main content width instead of generic max-w-7xl shell spacing', () => {
    expect(source).toContain("<div className='mx-auto w-full max-w-[1180px]")
    expect(source).not.toContain("max-w-7xl")
  })

  test('uses the reference topbar backdrop and divider shell', () => {
    expect(source).toContain("gap-[14px]")
    expect(source).toContain("border-[var(--line)]")
    expect(source).toContain("bg-[color-mix(in_oklab,var(--bg)_82%,transparent)]")
    expect(source).toContain("backdrop-blur-[12px]")
    expect(source).toContain("h-[22px] w-px bg-[var(--line)]")
    expect(source).not.toContain("<TopbarActionsDivider")
    expect(source).not.toContain("bg-background/85 px-4 backdrop-blur")
    expect(source).not.toContain("h-6 w-px bg-border")
  })

  test('keeps topbar collapsed toggles on shared square icon buttons', () => {
    expect(source).toContain("variant='outline' size='icon-sm'")
    expect(source).toContain("variant='ghost' size='icon-sm'")
    expect(source).not.toContain("className='md:hidden h-8 px-2'")
  })

  test('uses the reference screen padding rhythm for logged-in content', () => {
    expect(source).toContain("px-[22px] pb-16 pt-[22px]")
    expect(source).toContain("min-[920px]:px-6")
    expect(source).not.toContain("p-4 pb-12")
    expect(source).not.toContain("md:p-6")
  })
})
