import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ProviderButton } from './user-page'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-page.tsx'), 'utf8')

describe('ProviderButton', () => {
  test('renders active provider selection with status copy', () => {
    const html = renderToStaticMarkup(
      <ProviderButton
        active
        baseUrl='https://relay.example.com'
        labels={{ primary: 'primary', groupsReady: '2/3 ready' }}
        name='Relay Alpha'
        onClick={() => undefined}
        primary
        ready={2}
        total={3}
      />
    )

    expect(html).toContain('aria-pressed="true"')
    expect(html).toContain('Relay Alpha')
    expect(html).toContain('https://relay.example.com')
    expect(html).toContain('primary')
    expect(html).toContain('2/3 ready')
  })

  test('uses selectable card slots instead of page-local provider card layout', () => {
    expect(source).toContain('SelectableCardHeader')
    expect(source).toContain('SelectableCardMeta')
    expect(source).toContain('SelectableCardStatus')
    expect(source).not.toContain("className='flex items-center justify-between gap-2'")
    expect(source).not.toContain("className='mono mt-1 truncate text-muted-foreground text-[11px]'")
    expect(source).not.toContain("ready === total ? 'mt-2 font-medium text-[var(--pos)] text-xs' : 'mt-2 font-medium text-[var(--warn)] text-xs'")
  })
})
