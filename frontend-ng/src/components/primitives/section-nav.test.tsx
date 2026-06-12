import { CircleIcon } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionNav, SectionNavFrame } from './section-nav'

describe('SectionNav', () => {
  test('renders an accessible section rail with the active item marked current', () => {
    const html = renderToStaticMarkup(
      <SectionNav
        ariaLabel='Settings sections'
        items={[
          { value: 'relay', label: 'Relay providers', icon: CircleIcon, trailing: '3 repos' },
          { value: 'deployment', label: 'Deployment', icon: CircleIcon }
        ]}
        onChange={() => undefined}
        value='deployment'
      />
    )

    expect(html).toContain('aria-label="Settings sections"')
    expect(html).toContain('Relay providers')
    expect(html).toContain('3 repos')
    expect(html).toContain('Deployment')
    expect(html).toContain('aria-current="page"')
    expect(html).toContain('data-active="true"')
  })

  test('renders a framed section rail surface for settings-style side navigation', () => {
    const html = renderToStaticMarkup(
      <SectionNavFrame>
        <SectionNav
          ariaLabel='Settings sections'
          items={[{ value: 'relay', label: 'Relay providers', icon: CircleIcon }]}
          onChange={() => undefined}
          value='relay'
        />
      </SectionNavFrame>
    )

    expect(html).toContain('data-slot="section-nav-frame"')
    expect(html).toContain('bg-[var(--surface-2)]')
    expect(html).toContain('p-[8px]')
    expect(html).toContain('Relay providers')
  })

  test('uses the tighter reference rail button rhythm and inset active state', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./section-nav.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("'h-[38px] w-full justify-start gap-2.5 px-[11px] text-left font-medium text-[13px] shadow-none'")
    expect(source).toContain("'border-[var(--line)] bg-[var(--surface-inset)] text-foreground hover:bg-[var(--surface-inset)]'")
    expect(source).toContain("className='ml-auto shrink-0 text-[11px] text-[var(--ink-3)]'")
    expect(source).toContain("bg-[var(--surface-2)] p-[8px] shadow-none")
    expect(source).not.toContain("'h-10 w-full justify-start gap-3 px-3 text-left font-medium text-sm shadow-none'")
    expect(source).not.toContain("className='ml-auto shrink-0 text-[11px] text-muted-foreground'")
  })

  test('owns workbench scroll constraints for long section rails', () => {
    const html = renderToStaticMarkup(
      <SectionNav
        ariaLabel='Repository scopes'
        items={[
          { value: 'all', label: 'All repositories', icon: CircleIcon },
          { value: 'platform', label: 'Platform', icon: CircleIcon }
        ]}
        onChange={() => undefined}
        scroll='workbench'
        value='all'
      />
    )

    expect(html).toContain('max-h-[430px]')
    expect(html).toContain('overflow-y-auto')
    expect(html).toContain('Repository scopes')
  })
})
