import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarActions } from './topbar-actions'

const locales = [
  { value: 'en-US' as const, label: 'English' },
  { value: 'zh-CN' as const, label: 'Chinese' }
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
    expect(html).toContain('data-slot="topbar-actions-divider"')
    expect(html).toContain('data-slot="topbar-actions-locale-trigger"')
    expect(html).toContain('English')
    expect(html).toContain('Toggle theme')
  })

  test('keeps the locale trigger and action rhythm on the reference compact shell', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./topbar-actions.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from './topbar-actions-divider'")
    expect(source).toContain("<TopbarActionsDivider className='hidden min-[920px]:block' />")
    expect(source).toContain("className='hidden min-[920px]:inline'")
    expect(source).toContain("className='min-w-40 border-[var(--line-strong)]'")
    expect(source).toContain("size='sm'")
    expect(source).toContain("size='icon-sm'")
    expect(source).toContain("type='button'")
    expect(source).toContain("variant='ghost'")
    expect(source).toContain("className='gap-[6px] px-[9px] text-[12px] font-semibold'")
    expect(source).toContain("<ChevronDownIcon className='text-[var(--ink-4)]' />")
    expect(source).toContain('{currentLocale?.label}')
    expect(source).not.toContain("width: 'auto'")
    expect(source).not.toContain("padding: '0 9px'")
  })
})
