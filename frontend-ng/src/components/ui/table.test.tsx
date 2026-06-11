import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'

describe('Table', () => {
  test('keeps shared table chrome aligned with the reference scan rhythm', () => {
    const html = renderToStaticMarkup(
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Repository</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow>
            <TableCell>ai-efficiency</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    )

    expect(html).toContain('data-slot="table"')
    expect(html).toContain('text-[13px]')
    expect(html).toContain('border-[var(--line-faint)]')
    expect(html).toContain('hover:bg-[var(--surface-2)]')
    expect(html).toContain('text-[10.5px]')
    expect(html).toContain('uppercase')
    expect(html).toContain('tracking-[0.06em]')
    expect(html).toContain('text-[var(--ink-2)]')
  })
})
