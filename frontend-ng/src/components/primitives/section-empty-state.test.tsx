import { renderToStaticMarkup } from 'react-dom/server'
import { FolderGit2Icon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { SectionEmptyState } from './section-empty-state'

describe('SectionEmptyState', () => {
  test('renders a shared compact section-level empty shell', async () => {
    const html = renderToStaticMarkup(
      <SectionEmptyState
        action={<button type='button'>Add repository</button>}
        description='Connect a provider to start importing repositories.'
        icon={FolderGit2Icon}
        title='No repositories yet'
      />
    )
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./section-empty-state.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/ui/empty'")
    expect(html).toContain('data-slot="empty"')
    expect(html).toContain('data-size="compact"')
    expect(html).toContain('No repositories yet')
    expect(html).toContain('Connect a provider to start importing repositories.')
    expect(html).toContain('Add repository')
  })
})
