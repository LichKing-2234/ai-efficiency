import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ManagedFormFooter } from './managed-form-footer'

describe('ManagedFormFooter', () => {
  test('renders stacked error alerts above the shared submit-cancel actions', () => {
    const html = renderToStaticMarkup(
      <ManagedFormFooter
        cancelLabel='Cancel'
        errors={['First error', undefined, 'Second error']}
        submitDisabled
        submitLabel='Create'
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('First error')
    expect(html).toContain('Second error')
    expect(html).toContain('data-slot="form-actions"')
    expect(html).toContain('Create')
    expect(html).toContain('Cancel')
  })

  test('omits alerts entirely when there are no concrete error messages', () => {
    const html = renderToStaticMarkup(
      <ManagedFormFooter
        cancelLabel='Cancel'
        errors={[undefined]}
        submitLabel='Update'
        onCancel={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('Update')
    expect(html).not.toContain('data-slot="alert"')
  })
})
