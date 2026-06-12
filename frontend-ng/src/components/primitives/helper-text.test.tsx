import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { HelperText } from './helper-text'

describe('HelperText', () => {
  test('renders shared helper copy styling', () => {
    const html = renderToStaticMarkup(
      <HelperText>
        Subscription changes apply to the selected users.
      </HelperText>
    )

    expect(html).toContain('Subscription changes apply to the selected users.')
    expect(html).toContain('data-slot="field-description"')
  })
})
