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
})
