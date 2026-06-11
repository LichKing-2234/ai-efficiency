import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Tabs, TabsList, TabsTrigger } from './tabs'

describe('Tabs', () => {
  test('supports wrapped tab lists for toolbar provider filters', () => {
    const html = renderToStaticMarkup(
      <Tabs defaultValue='one'>
        <TabsList wrap>
          <TabsTrigger value='one'>One</TabsTrigger>
          <TabsTrigger value='two'>Two</TabsTrigger>
        </TabsList>
      </Tabs>
    )

    expect(html).toContain('data-wrap="true"')
    expect(html).toContain('h-auto')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('justify-start')
  })

  test('keeps the default tab list and trigger aligned with the reference segmented control density', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./tabs.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('bg-[var(--surface-inset)]')
    expect(source).toContain('border border-[var(--line)]')
    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('p-[3px]')
    expect(source).toContain('h-[calc(100%-1px)]')
    expect(source).toContain('text-[12.5px]')
    expect(source).toContain('font-semibold')
    expect(source).toContain("data-active:bg-[var(--surface)]")
    expect(source).toContain("data-active:text-[var(--ink)]")
    expect(source).toContain("group-data-[variant=default]/tabs-list:data-active:border-[var(--line)]")
    expect(source).not.toContain('bg-muted')
    expect(source).not.toContain('rounded-lg p-[3px] text-muted-foreground')
    expect(source).not.toContain('px-1.5 py-0.5 text-sm font-medium whitespace-nowrap text-foreground/60')
    expect(source).not.toContain('focus-visible:ring-[3px] focus-visible:ring-ring/50')
  })
})
