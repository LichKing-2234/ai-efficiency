import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarCommandTrigger } from './topbar-command-trigger'

describe('TopbarCommandTrigger', () => {
  test('renders desktop and compact command entry points with stable slots', () => {
    const html = renderToStaticMarkup(<TopbarCommandTrigger label='Search or jump to...' onOpen={() => undefined} />)

    expect(html).toContain('data-slot="topbar-command-trigger"')
    expect(html).toContain('data-slot="topbar-command-trigger-desktop"')
    expect(html).toContain('data-slot="topbar-command-trigger-mobile"')
    expect(html).toContain('data-slot="topbar-command-trigger-kbd"')
    expect(html).toContain('⌘K')
    expect(html).toContain('Search or jump to...')
  })

  test('uses the shared command-trigger shell with equal mobile icon sizing', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-command-trigger.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className='cmd-trigger hidden h-[34px] min-w-[188px] justify-start gap-[9px] border-[var(--line)] bg-[var(--surface-inset)] px-[10px] pl-[11px] text-[12.5px] text-[var(--ink-3)] min-[920px]:inline-flex'")
    expect(source).toContain("size='default'")
    expect(source).toContain("size='icon'")
    expect(source).toContain("border-[var(--line)] bg-[var(--surface)] px-[6px] py-[2px]")
    expect(source).toContain("className='min-[920px]:hidden'")
  })
})
