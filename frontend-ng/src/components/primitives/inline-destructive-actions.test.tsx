import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InlineDestructiveActions } from './inline-destructive-actions'

describe('InlineDestructiveActions', () => {
  test('renders a ghost trigger before confirmation is armed', () => {
    const html = renderToStaticMarkup(
      <InlineDestructiveActions
        armed={false}
        cancelLabel='Cancel'
        confirmLabel='Confirm'
        triggerLabel='Delete'
        onArm={() => undefined}
        onCancel={() => undefined}
        onConfirm={() => undefined}
      />
    )

    expect(html).toContain('Delete')
    expect(html).toContain('data-slot="action-group"')
    expect(html).not.toContain('data-slot="inline-confirm-actions"')
  })

  test('renders shared destructive inline confirmation when armed', () => {
    const html = renderToStaticMarkup(
      <InlineDestructiveActions
        armed
        cancelLabel='Cancel'
        confirmLabel='Confirm'
        triggerLabel='Delete'
        onArm={() => undefined}
        onCancel={() => undefined}
        onConfirm={() => undefined}
      />
    )

    expect(html).toContain('data-slot="inline-confirm-actions"')
    expect(html).toContain('Confirm')
    expect(html).toContain('Cancel')
    expect(html).toContain('ml-auto')
  })
})
