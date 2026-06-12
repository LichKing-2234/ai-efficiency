import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { Badge } from '@/components/ui/badge'
import { RepositoriesWorkbenchShell } from './repositories-workbench-shell'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repositories-workbench-shell.tsx'), 'utf8')

describe('RepositoriesWorkbenchShell', () => {
  test('renders the shared repository workbench frame with stable slots', () => {
    const html = renderToStaticMarkup(
      <RepositoriesWorkbenchShell
        header={<button type='button'>All</button>}
        meta='10 total repositories'
        providerTabs={<button type='button'>GitHub</button>}
        rail={<button type='button'>platform-team</button>}
        railActions={<Badge variant='secondary'>2</Badge>}
        railDescription='GitHub'
        railTitle='Scopes'
        title='platform-team'
      >
        <div>Repository table</div>
      </RepositoriesWorkbenchShell>
    )

    expect(html).toContain('data-slot="repositories-workbench-shell"')
    expect(html).toContain('data-slot="repositories-workbench-grid"')
    expect(html).toContain('data-slot="card-filter-bar"')
    expect(html).toContain('data-slot="workbench-rail"')
    expect(html).toContain('data-slot="workbench-content"')
    expect(html).toContain('GitHub')
    expect(html).toContain('platform-team')
    expect(html).toContain('10 total repositories')
    expect(html).toContain('Repository table')
  })

  test('keeps the repository shell composed from shared frame primitives', () => {
    expect(source).toContain("from '@/components/primitives/framed-card'")
    expect(source).toContain("from '@/components/primitives/card-filter-bar'")
    expect(source).toContain("from '@/components/primitives/section-card-header'")
    expect(source).toContain("from '@/components/primitives/workbench-rail'")
    expect(source).toContain("<FramedCard data-slot='repositories-workbench-shell'>")
    expect(source).toContain("<div className='repo-workbench' data-slot='repositories-workbench-grid'>")
  })
})
