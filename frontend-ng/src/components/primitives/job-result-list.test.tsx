import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { JobResultList } from './job-result-list'

describe('JobResultList', () => {
  test('renders capped job result rows with fallback identity and status badges', () => {
    const html = renderToStaticMarkup(
      <JobResultList
        items={[
          { user_id: 1, username: 'alice', email: 'alice@example.com', status: 'success', message: 'Added to Group Alpha' },
          { user_id: 2, email: 'bob@example.org', status: 'skipped' },
          { user_id: 3, status: 'failed', message: 'Missing relay mapping' }
        ]}
        maxItems={2}
      />
    )

    expect(html).toContain('data-slot="job-result-list"')
    expect(html).toContain('data-slot="job-result-list-row"')
    expect(html).toContain('alice')
    expect(html).toContain('Added to Group Alpha')
    expect(html).toContain('bob@example.org')
    expect(html).toContain('success')
    expect(html).toContain('skipped')
    expect(html).not.toContain('#3')
    expect(html).not.toContain('Missing relay mapping')
  })
})
