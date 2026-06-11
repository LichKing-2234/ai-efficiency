import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarActions } from './topbar-actions'

const locales = [
  { value: 'en-US' as const, label: 'English', shortLabel: 'EN' },
  { value: 'zh-CN' as const, label: 'Chinese', shortLabel: 'ZH' }
]

describe('TopbarActions', () => {
  test('renders command, live status, locale, and theme controls as one action cluster', () => {
    const html = renderToStaticMarkup(
      <TopbarActions
        commandLabel='Search or jump to...'
        dark={false}
        ingestingLabel='Ingesting'
        locale='en-US'
        locales={locales}
        themeLabel='Toggle theme'
        onLocaleChange={() => undefined}
        onOpenCommand={() => undefined}
        onToggleTheme={() => undefined}
      />
    )

    expect(html).toContain('data-slot="topbar-actions"')
    expect(html).toContain('data-slot="topbar-command-trigger"')
    expect(html).toContain('data-slot="topbar-live-status"')
    expect(html).toContain('data-slot="topbar-actions-locale-trigger"')
    expect(html).toContain('EN')
    expect(html).toContain('Toggle theme')
  })

  test('keeps the locale trigger on the reference compact ghost-button shell', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-actions.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className='hidden sm:inline'")
    expect(source).toContain("className='min-w-40 border-[var(--line-strong)]'")
    expect(source).toContain("size='sm' type='button' variant='ghost'")
    expect(source).not.toContain("width: 'auto'")
    expect(source).not.toContain("padding: '0 9px'")
  })
})
