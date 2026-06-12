import type * as React from 'react'
import { FramedTableCard } from '@/components/primitives/framed-table-card'
import { SearchActionBar } from '@/components/primitives/search-action-bar'
import { SearchWorkbenchCard } from '@/components/primitives/search-workbench-card'

export function SearchTableWorkbench({
  actions,
  children,
  footer,
  search,
  searchChildren
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  footer?: React.ReactNode
  search: React.ReactNode
  searchChildren?: React.ReactNode
}) {
  return (
    <>
      <SearchWorkbenchCard>
        <SearchActionBar actions={actions} search={search}>
          {searchChildren}
        </SearchActionBar>
      </SearchWorkbenchCard>
      <FramedTableCard footer={footer}>
        {children}
      </FramedTableCard>
    </>
  )
}
