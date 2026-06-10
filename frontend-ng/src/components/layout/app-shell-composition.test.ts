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
    expect(source).toContain("from './topbar-command-trigger'")
    expect(source).toContain('<TopbarCommandTrigger')
    expect(source).not.toContain("<kbd className='rounded border border-border bg-[var(--surface)] px-1.5 py-0.5 font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'>")
    expect(source).not.toContain("className='hidden min-w-48 justify-start gap-2 text-[var(--ink-3)] lg:inline-flex'")
  })

  test('uses shared live status composition in the topbar', () => {
    expect(source).toContain("from './topbar-live-status'")
    expect(source).toContain('<TopbarLiveStatus')
    expect(source).not.toContain("<div className='hidden items-center gap-2 rounded-full border border-[var(--pos-line)] bg-[var(--pos-soft)] px-3 py-1 md:flex'>")
  })

  test('uses shared sidebar user summary composition in the footer', () => {
    expect(source).toContain("from './sidebar-user-summary'")
    expect(source).toContain('<SidebarUserSummary')
    expect(source).not.toContain("className='grid size-8 place-items-center rounded-full bg-[var(--ae-ai-soft)] font-semibold text-[var(--ae-ai-2)] text-xs'")
    expect(source).not.toContain("className='truncate text-[var(--ae-text-4)] text-xs'")
  })
})
