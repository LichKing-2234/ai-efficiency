import type * as React from 'react'
import { FramedTableCard } from '@/components/primitives/framed-table-card'
import { SearchActionBar } from '@/components/primitives/search-action-bar'
import { SearchWorkbenchCard } from '@/components/primitives/search-workbench-card'
import { WorkbenchFrame } from '@/components/primitives/workbench-frame'

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
    <WorkbenchFrame
      body={<FramedTableCard>{children}</FramedTableCard>}
      footer={footer}
      topBar={(
        <SearchWorkbenchCard>
          <SearchActionBar actions={actions} search={search}>
            {searchChildren}
          </SearchActionBar>
        </SearchWorkbenchCard>
      )}
    />
  )
}
