import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ToolbarSelect } from './toolbar-select'

describe('ToolbarSelect', () => {
  test('renders a compact grouped select for toolbars', () => {
    const html = renderToStaticMarkup(
      <ToolbarSelect
        ariaLabel='Page size'
        options={[
          { label: '20 / page', value: '20' },
          { label: '50 / page', value: '50' }
        ]}
        value='20'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="select-trigger"')
    expect(html).toContain('aria-label="Page size: 20 / page"')
    expect(html).toContain('min-w-24')
  })

  test('supports small toolbar triggers', () => {
    const html = renderToStaticMarkup(
      <ToolbarSelect
        ariaLabel='Rows'
        size='sm'
        options={[{ label: '20', value: '20' }]}
        value='20'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('data-size="sm"')
  })

  test('supports semantic toolbar widths', () => {
    const html = renderToStaticMarkup(
      <ToolbarSelect
        ariaLabel='Page size'
        width='compact'
        options={[{ label: '20 / page', value: '20' }]}
        value='20'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('w-36')
  })

  test('supports disabled selects', () => {
    const html = renderToStaticMarkup(
      <ToolbarSelect
        ariaLabel='Provider'
        disabled
        options={[{ label: 'None', value: 'none' }]}
        value='none'
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('disabled=""')
  })
})
