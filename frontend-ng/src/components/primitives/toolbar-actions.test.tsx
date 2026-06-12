import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ToolbarActions } from './toolbar-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'toolbar-actions.tsx'), 'utf8')

describe('ToolbarActions', () => {
  test('renders a shared wrapped toolbar action row shell', () => {
    const html = renderToStaticMarkup(
      <ToolbarActions>
        <button type='button'>Range</button>
        <button type='button'>Refresh</button>
      </ToolbarActions>
    )

    expect(html).toContain('data-slot="toolbar-actions"')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('Range')
    expect(html).toContain('Refresh')
  })

  test('sources layout from the shared action-group shell', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup dataSlot='toolbar-actions' wrap>{children}</ActionGroup>")
  })
})
