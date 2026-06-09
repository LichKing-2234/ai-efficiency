import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ActionGroup } from './action-group'

describe('ActionGroup', () => {
  test('renders right-aligned compact actions by default', () => {
    const html = renderToStaticMarkup(
      <ActionGroup>
        <button type='button'>Update</button>
        <button type='button'>Delete</button>
      </ActionGroup>
    )

    expect(html).toContain('data-slot="action-group"')
    expect(html).toContain('justify-end')
    expect(html).toContain('gap-2')
    expect(html).toContain('Update')
    expect(html).toContain('Delete')
  })

  test('supports wrapping action rows for crowded toolbars', () => {
    const html = renderToStaticMarkup(
      <ActionGroup wrap>
        <button type='button'>Check update</button>
      </ActionGroup>
    )

    expect(html).toContain('flex-wrap')
    expect(html).toContain('Check update')
  })
})
