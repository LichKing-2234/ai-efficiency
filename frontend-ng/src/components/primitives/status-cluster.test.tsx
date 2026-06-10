import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Badge } from '@/components/ui/badge'
import { StatusBadge } from './status-badge'
import { StatusCluster } from './status-cluster'

describe('StatusCluster', () => {
  test('renders compact wrapping status badges in a semantic slot', () => {
    const html = renderToStaticMarkup(
      <StatusCluster>
        <Badge variant='pos'>bound</Badge>
        <StatusBadge value='active' />
      </StatusCluster>
    )

    expect(html).toContain('data-slot="status-cluster"')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('bound')
    expect(html).toContain('active')
  })

  test('owns status cluster rhythm instead of route-local wrappers', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./status-cluster.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('data-slot')
    expect(source).not.toContain("className='flex flex-wrap gap-2'")
  })
})
