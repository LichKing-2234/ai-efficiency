import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InlineConfirmActions } from './inline-confirm-actions'

describe('InlineConfirmActions', () => {
  test('renders shared inline confirm and cancel actions for dense workbench rows', () => {
    const html = renderToStaticMarkup(
      <InlineConfirmActions
        cancelLabel='Cancel'
        confirmLabel='Confirm'
        onCancel={() => undefined}
        onConfirm={() => undefined}
      />
    )

    expect(html).toContain('data-slot="inline-confirm-actions"')
    expect(html).toContain('Confirm')
    expect(html).toContain('Cancel')
    expect(html).toContain('type="button"')
  })

  test('supports destructive confirms and pushed layout for slide-over footers', () => {
    const html = renderToStaticMarkup(
      <InlineConfirmActions
        cancelLabel='Cancel'
        confirmLabel='Delete'
        confirmVariant='destructive'
        push
        wrap
        onCancel={() => undefined}
        onConfirm={() => undefined}
      />
    )

    expect(html).toContain('Delete')
    expect(html).toContain('ml-auto')
    expect(html).toContain('flex-wrap')
  })
})
