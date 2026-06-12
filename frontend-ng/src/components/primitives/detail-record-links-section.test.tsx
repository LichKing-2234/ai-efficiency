import { GitPullRequestIcon } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { LinkedRecordItem } from './linked-record-list'
import { DetailRecordLinksSection } from './detail-record-links-section'

describe('DetailRecordLinksSection', () => {
  test('renders shared detail section, linked list, and empty-state behavior', () => {
    const html = renderToStaticMarkup(
      <DetailRecordLinksSection emptyTitle='No matched PRs' title='Matched pull requests'>
        <LinkedRecordItem
          description='merged'
          href='https://example.com/pr/42'
          icon={<GitPullRequestIcon />}
          label='#42 Improve usage'
          variant='plain'
        />
      </DetailRecordLinksSection>
    )

    expect(html).toContain('data-slot="detail-section"')
    expect(html).toContain('data-slot="linked-record-list"')
    expect(html).toContain('#42 Improve usage')
    expect(html).toContain('Matched pull requests')
  })

  test('falls back to the shared page empty state when no linked records are present', () => {
    const html = renderToStaticMarkup(
      <DetailRecordLinksSection emptyTitle='No matched PRs' title='Matched pull requests' />
    )

    expect(html).toContain('No matched PRs')
    expect(html).toContain('data-slot="empty"')
  })
})
