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
})
