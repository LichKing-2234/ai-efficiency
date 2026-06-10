import { GitPullRequestIcon } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { LinkedRecordItem, LinkedRecordList } from './linked-record-list'

describe('LinkedRecordList', () => {
  test('renders external record links with shared row semantics', () => {
    const html = renderToStaticMarkup(
      <LinkedRecordList>
        <LinkedRecordItem description='alice' href='https://example.com/pr/42' icon={<GitPullRequestIcon />} label='repo#42 · Fix usage rollup' trailing='Open' />
        <LinkedRecordItem href='https://example.com/pr/43' label='repo#43 · Add attribution checks' />
      </LinkedRecordList>
    )

    expect(html).toContain('data-slot="linked-record-list"')
    expect(html).toContain('data-slot="linked-record-item"')
    expect(html).toContain('href="https://example.com/pr/42"')
    expect(html).toContain('target="_blank"')
    expect(html).toContain('rel="noreferrer"')
    expect(html).toContain('repo#42 · Fix usage rollup')
    expect(html).toContain('alice')
    expect(html).toContain('Open')
    expect(html).toContain('repo#43 · Add attribution checks')
  })

  test('supports a plain variant for links embedded in data grids', () => {
    const html = renderToStaticMarkup(
      <LinkedRecordItem
        description='alice'
        href='https://example.com/pr/42'
        icon={<GitPullRequestIcon />}
        label='Fix usage rollup'
        trailing='Open'
        variant='plain'
      />
    )

    expect(html).toContain('data-slot="linked-record-item"')
    expect(html).toContain('bg-transparent')
    expect(html).toContain('border-0')
    expect(html).toContain('p-0')
    expect(html).not.toContain('bg-card')
    expect(html).not.toContain('border-border')
  })

  test('keeps description rhythm inside the primitive description slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./linked-record-list.tsx', import.meta.url), 'utf8')
    )

    expect(source).not.toContain("className='mt-1 block truncate text-muted-foreground text-xs'")
  })
})
