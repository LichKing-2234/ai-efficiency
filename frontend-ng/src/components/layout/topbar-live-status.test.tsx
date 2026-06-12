import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarLiveStatus } from './topbar-live-status'

describe('TopbarLiveStatus', () => {
  test('renders the reference live status pill with stable slots', () => {
    const html = renderToStaticMarkup(<TopbarLiveStatus label='Ingesting' />)

    expect(html).toContain('data-slot="topbar-live-status"')
    expect(html).toContain('data-slot="topbar-live-status-dot"')
    expect(html).toContain('data-slot="topbar-live-status-label"')
    expect(html).toContain('live-dot')
    expect(html).toContain('Ingesting')
    expect(html).toContain('rounded-full')
    expect(html).toContain('h-7')
    expect(html).toContain('px-[10px]')
  })

  test('keeps the denser reference live-status typography and spacing', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-live-status.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className='hidden h-7 items-center gap-[7px] rounded-full border border-[var(--pos-line)] bg-[var(--pos-soft)] px-[10px] min-[920px]:flex'")
    expect(source).toContain("className='font-semibold text-[11.5px] text-[var(--pos)]'")
    expect(source).not.toContain('text-xs')
    expect(source).not.toContain('px-2.5')
  })
})
