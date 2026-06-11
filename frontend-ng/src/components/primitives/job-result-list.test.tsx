import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { JobResultList } from './job-result-list'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'job-result-list.tsx'), 'utf8')

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

  test('uses shared row primitives for job result rows and identity copy', () => {
    expect(source).toContain("from './action-group'")
    expect(source).toContain("from './stack'")
    expect(source).toContain("className={cn('max-h-56 overflow-auto rounded-[var(--r-md)] border border-border bg-[var(--surface)]', className)}")
    expect(source).toContain("className='border-border border-b px-[14px] py-[9px] text-[12.5px] last:border-b-0'")
    expect(source).toContain("className='truncate font-medium text-[12.5px]'")
    expect(source).toContain("className='truncate text-[11px] text-[var(--ink-4)]'")
    expect(source).not.toContain("className={cn('max-h-56 overflow-auto rounded-[var(--r-md)] border border-border bg-card', className)}")
    expect(source).not.toContain("className='flex items-center justify-between gap-3 border-border border-b px-3 py-2 text-sm last:border-b-0'")
    expect(source).not.toContain("<div className='min-w-0'>")
  })
})
