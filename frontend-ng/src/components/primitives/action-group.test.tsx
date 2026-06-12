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
    expect(html).toContain('gap-2.5')
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

  test('supports start-aligned inline action rows', () => {
    const html = renderToStaticMarkup(
      <ActionGroup align='start'>
        <button type='button'>Repair webhook</button>
      </ActionGroup>
    )

    expect(html).toContain('justify-start')
    expect(html).not.toContain('justify-end')
    expect(html).toContain('Repair webhook')
  })

  test('supports split action rows for paired equal-width decisions', () => {
    const html = renderToStaticMarkup(
      <ActionGroup layout='split'>
        <button type='button'>Approve</button>
        <button type='button'>Deny</button>
      </ActionGroup>
    )

    expect(html).toContain('w-full')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('Approve')
    expect(html).toContain('Deny')
  })

  test('supports fitted action rows inside dense data grids', () => {
    const html = renderToStaticMarkup(
      <ActionGroup fit wrap>
        <button type='button'>Copy encrypted</button>
      </ActionGroup>
    )

    expect(html).toContain('min-w-0')
    expect(html).toContain('Copy encrypted')
  })

  test('supports responsive end alignment for header actions', () => {
    const html = renderToStaticMarkup(
      <ActionGroup align='responsive-end' wrap>
        <button type='button'>Group Alpha</button>
      </ActionGroup>
    )

    expect(html).toContain('justify-start')
    expect(html).toContain('min-[920px]:justify-end')
    expect(html).not.toContain('gap-2 justify-end flex-wrap')
  })

  test('supports pushed toolbar actions', () => {
    const html = renderToStaticMarkup(
      <ActionGroup push wrap>
        <button type='button'>Add repository</button>
      </ActionGroup>
    )

    expect(html).toContain('ml-auto')
    expect(html).toContain('Add repository')
  })

  test('supports block-end aligned form actions', () => {
    const html = renderToStaticMarkup(
      <ActionGroup align='block-end'>
        <button type='button'>Start job</button>
      </ActionGroup>
    )

    expect(html).toContain('items-end')
    expect(html).toContain('justify-end')
    expect(html).toContain('Start job')
  })

  test('forwards native span attributes for semantic state hooks', () => {
    const html = renderToStaticMarkup(
      <ActionGroup data-empty='true' id='meter-shell'>
        <button type='button'>Visible</button>
      </ActionGroup>
    )

    expect(html).toContain('data-empty="true"')
    expect(html).toContain('id="meter-shell"')
  })
})
