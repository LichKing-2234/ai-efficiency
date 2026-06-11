import { renderToStaticMarkup } from 'react-dom/server'
import { FolderGit2Icon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { PageEmpty } from './page-empty'

describe('PageEmpty', () => {
  test('renders shared empty primitives for page-level empty states', () => {
    const html = renderToStaticMarkup(
      <PageEmpty icon={FolderGit2Icon} title='No repositories' description='Add a repository to get started.' action={<button type='button'>Add</button>} />
    )

    expect(html).toContain('data-slot="empty"')
    expect(html).toContain('data-slot="empty-title"')
    expect(html).toContain('data-slot="empty-description"')
    expect(html).toContain('data-slot="empty-content"')
    expect(html).toContain('data-slot="empty-icon"')
    expect(html).toContain('No repositories')
    expect(html).toContain('Add a repository to get started.')
    expect(html).toContain('Add')
  })
})
