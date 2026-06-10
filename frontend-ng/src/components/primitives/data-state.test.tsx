import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { EmptyState, ErrorState, LoadingState } from './data-state'

describe('DataState', () => {
  test('renders loading state rhythm through stable slots', () => {
    const html = renderToStaticMarkup(<LoadingState label='Fetching repositories' />)

    expect(html).toContain('data-slot="data-state-loading"')
    expect(html).toContain('data-slot="data-state-loading-content"')
    expect(html).toContain('Fetching repositories')
  })

  test('renders empty state title and description through stable slots', () => {
    const html = renderToStaticMarkup(<EmptyState title='No records' description='Try another filter.' action={<button type='button'>Reset</button>} />)

    expect(html).toContain('data-slot="data-state-empty"')
    expect(html).toContain('data-slot="data-state-empty-title"')
    expect(html).toContain('data-slot="data-state-empty-description"')
    expect(html).toContain('No records')
    expect(html).toContain('Try another filter.')
    expect(html).toContain('Reset')
  })

  test('renders error state with caller-provided retry copy', () => {
    const html = renderToStaticMarkup(<ErrorState message='Request failed' retryLabel='Try again' onRetry={() => {}} />)

    expect(html).toContain('data-slot="data-state-error"')
    expect(html).toContain('data-slot="data-state-error-content"')
    expect(html).toContain('data-slot="data-state-error-message"')
    expect(html).toContain('Request failed')
    expect(html).toContain('Try again')
    expect(html).not.toContain('Retry')
  })
})
